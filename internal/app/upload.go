package app

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type uploadConfig struct {
	Enabled        bool     `json:"enabled"`
	AllowedFormats []string `json:"allowedFormats"`
	MaxFileSize    int64    `json:"maxFileSize"`
	RateLimit      int      `json:"rateLimit"`
}

func publicConfig(a *App) uploadConfig {
	c := uploadConfig{Enabled: false, AllowedFormats: []string{"jpg", "jpeg", "png", "gif", "webp"}, MaxFileSize: 10 << 20, RateLimit: 10}
	json.Unmarshal(a.setting("publicApiConfig", c), &c)
	if c.MaxFileSize < 1 || c.MaxFileSize > 100<<20 {
		c.MaxFileSize = 10 << 20
	}
	return c
}

func privateLimit(a *App) int64 {
	var c struct {
		MaxFileSize int64 `json:"maxFileSize"`
	}
	json.Unmarshal(a.setting("privateApiConfig", map[string]any{"maxFileSize": 100 << 20}), &c)
	if c.MaxFileSize < 1 || c.MaxFileSize > 200<<20 {
		return 100 << 20
	}
	return c.MaxFileSize
}

func detectFormat(head []byte) string {
	if len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP" {
		return "webp"
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" && (string(head[8:12]) == "avif" || string(head[8:12]) == "avis") {
		return "avif"
	}
	switch http.DetectContentType(head) {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/bmp":
		return "bmp"
	case "image/x-icon", "image/vnd.microsoft.icon":
		return "ico"
	}
	return ""
}

func multipartFile(w http.ResponseWriter, r *http.Request, maxSize int64, tempDir string) (*os.File, string, int64, error) {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		return nil, "", 0, fmt.Errorf("需要 multipart/form-data")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSize+(1<<20))
	mr := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return nil, "", 0, fmt.Errorf("请选择图片")
		}
		if err != nil {
			return nil, "", 0, err
		}
		if part.FormName() != "file" && part.FormName() != "image" {
			part.Close()
			continue
		}
		name := filepath.Base(part.FileName())
		if name == "." || name == "" {
			part.Close()
			return nil, "", 0, fmt.Errorf("缺少文件名")
		}
		f, err := os.CreateTemp(tempDir, ".tanoimg-upload-*")
		if err != nil {
			part.Close()
			return nil, "", 0, err
		}
		n, err := io.Copy(f, io.LimitReader(part, maxSize+1))
		part.Close()
		if err != nil || n == 0 || n > maxSize {
			f.Close()
			os.Remove(f.Name())
			return nil, "", 0, fmt.Errorf("文件为空或超过大小限制")
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, "", 0, err
		}
		return f, name, n, nil
	}
}

func (a *App) upload(w http.ResponseWriter, r *http.Request, public bool) {
	maxSize := privateLimit(a)
	kind := "private"
	if public {
		kind = "public"
		c := publicConfig(a)
		if !c.Enabled {
			fail(w, 403, "公共上传已禁用")
			return
		}
		var blocked int
		if a.DB.QueryRow(`SELECT 1 FROM ip_blacklist WHERE ip=?`, a.clientIP(r)).Scan(&blocked) == nil {
			fail(w, 403, "该 IP 已被禁止上传")
			return
		}
		maxSize = c.MaxFileSize
		if !a.allowPublicRequest(a.clientIP(r), c.RateLimit) {
			fail(w, 429, "公开上传过于频繁，请稍后重试")
			return
		}
	} else if a.userID(r) == "" && !a.hasAPIKey(r) {
		fail(w, 401, "缺少有效的 API Key 或登录状态")
		return
	}
	if !a.acquireUploadSlot(w, r) {
		return
	}
	defer func() { <-a.limiter }()
	f, original, size, err := multipartFile(w, r, maxSize, filepath.Join(a.DataDir, "uploads"))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	head := make([]byte, 512)
	n, _ := f.Read(head)
	format := detectFormat(head[:n])
	if format == "" {
		fail(w, 400, "不支持的图片内容格式")
		return
	}
	if public {
		allowed := false
		for _, ext := range publicConfig(a).AllowedFormats {
			if strings.EqualFold(ext, format) || (format == "jpg" && strings.EqualFold(ext, "jpeg")) {
				allowed = true
				break
			}
		}
		if !allowed {
			fail(w, 400, "该图片格式未开放上传")
			return
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		fail(w, 500, "读取上传文件失败")
		return
	}
	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	uuid, err := newID()
	if err != nil {
		fail(w, 500, "创建图片失败")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, 500, "创建图片失败")
		return
	}
	filename := uuid + "." + format
	dest := filepath.Join(a.DataDir, "uploads", filename)
	if err := os.Rename(f.Name(), dest); err != nil {
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			fail(w, 500, "保存图片失败")
			return
		}
		out, createErr := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			fail(w, 500, "保存图片失败")
			return
		}
		_, err = io.Copy(out, f)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(dest)
			fail(w, 500, "保存图片失败")
			return
		}
	}
	im := Image{ID: id, UUID: uuid, Filename: filename, OriginalName: original, Format: format, Size: size, Width: width, Height: height, UploadedByType: kind, UploadedBy: "API用户", UploadedAt: now()}
	if public {
		im.UploadedBy = "访客"
	}
	im.UpdatedAt = im.UploadedAt
	im.URL = "/i/" + filename
	if err := a.saveImage(im); err != nil {
		os.Remove(dest)
		fail(w, 500, "保存图片记录失败")
		return
	}
	ok(w, im)
}

func (a *App) acquireUploadSlot(w http.ResponseWriter, r *http.Request) bool {
	select {
	case a.limiter <- struct{}{}:
		return true
	default:
	}
	select {
	case a.waiters <- struct{}{}:
		defer func() { <-a.waiters }()
	default:
		w.Header().Set("Retry-After", "1")
		fail(w, http.StatusTooManyRequests, "上传队列已满，请稍后重试")
		return false
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case a.limiter <- struct{}{}:
		return true
	case <-r.Context().Done():
		return false
	case <-timer.C:
		w.Header().Set("Retry-After", "1")
		fail(w, http.StatusTooManyRequests, "等待上传超时，请稍后重试")
		return false
	}
}

func (a *App) allowPublicRequest(ip string, limit int) bool {
	if limit < 1 {
		limit = 10
	}
	if limit > 1000 {
		limit = 1000
	}
	nowUnix := time.Now().Unix()
	cutoff := nowUnix - 60
	result, err := a.DB.Exec(`INSERT INTO upload_rates(ip,window_start,count) VALUES(?,?,1)
		ON CONFLICT(ip) DO UPDATE SET
		window_start=CASE WHEN upload_rates.window_start<=? THEN excluded.window_start ELSE upload_rates.window_start END,
		count=CASE WHEN upload_rates.window_start<=? THEN 1 ELSE upload_rates.count+1 END
		WHERE upload_rates.window_start<=? OR upload_rates.count<?`, ip, nowUnix, cutoff, cutoff, cutoff, limit)
	if err != nil {
		return false
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false
	}
	a.DB.Exec(`DELETE FROM upload_rates WHERE window_start<?`, nowUnix-120)
	return true
}

func remoteIP(remote string) string {
	ip, _, err := net.SplitHostPort(remote)
	if err != nil {
		ip = remote
	}
	if ip == "" {
		return "unknown"
	}
	return ip
}

func (a *App) clientIP(r *http.Request) string {
	peer := remoteIP(r.RemoteAddr)
	if !a.TrustProxy {
		return peer
	}
	forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
	if net.ParseIP(forwarded) != nil {
		return forwarded
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(realIP) != nil {
		return realIP
	}
	return peer
}
