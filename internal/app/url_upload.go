package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var blockedRemoteRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
}

func allowedRemoteIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range blockedRemoteRanges {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func parseRemoteURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("无效的图片 URL")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !allowedRemoteIP(ip) {
		return nil, errors.New("不允许下载内网或本机地址")
	}
	return u, nil
}

func newURLClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{MaxIdleConns: 8, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if allowedRemoteIP(ip) {
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
		}
		return nil, errors.New("目标地址不允许访问")
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("跳转次数过多")
		}
		_, err := parseRemoteURL(req.URL.String())
		return err
	}}
}

func (a *App) urlUploader(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if a.userID(r) != "" {
		var name string
		if a.DB.QueryRow(`SELECT username FROM users WHERE id=?`, a.userID(r)).Scan(&name) == nil {
			return name, "url", true
		}
		return "管理员", "url", true
	}
	if a.hasAPIKey(r) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			key = r.URL.Query().Get("apiKey")
		}
		var name string
		if a.DB.QueryRow(`SELECT name FROM apikeys WHERE key=? AND enabled=1`, key).Scan(&name) == nil {
			return name, "url-api", true
		}
	}
	fail(w, 401, "缺少有效的 API Key 或登录状态")
	return "", "", false
}

func (a *App) importRemoteImage(r *http.Request, raw, uploadedBy, uploadedByType string, maxSize int64) (Image, error) {
	u, err := parseRemoteURL(raw)
	if err != nil {
		return Image{}, err
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return Image{}, err
	}
	request.Header.Set("Accept", "image/*")
	response, err := a.urlClient.Do(request)
	if err != nil {
		return Image{}, fmt.Errorf("下载图片失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("下载图片失败: HTTP %d", response.StatusCode)
	}
	if !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "image/") {
		return Image{}, errors.New("URL 指向的不是图片")
	}
	if response.ContentLength > maxSize {
		return Image{}, errors.New("图片超过大小限制")
	}
	f, err := os.CreateTemp(filepath.Join(a.DataDir, "uploads"), ".tanoimg-url-*")
	if err != nil {
		return Image{}, err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	size, err := io.Copy(f, io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return Image{}, err
	}
	if size == 0 || size > maxSize {
		return Image{}, errors.New("图片为空或超过大小限制")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Image{}, err
	}
	head := make([]byte, 512)
	n, _ := f.Read(head)
	format := detectFormat(head[:n])
	if format == "" {
		return Image{}, errors.New("不支持的图片内容格式")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Image{}, err
	}
	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	uuid, err := newID()
	if err != nil {
		return Image{}, err
	}
	id, err := newID()
	if err != nil {
		return Image{}, err
	}
	filename := uuid + "." + format
	dest := filepath.Join(a.DataDir, "uploads", filename)
	if err := os.Rename(f.Name(), dest); err != nil {
		return Image{}, err
	}
	original := path.Base(u.Path)
	if original == "." || original == "/" || original == "" {
		original = "image." + format
	}
	im := Image{ID: id, UUID: uuid, Filename: filename, OriginalName: original, Format: format, Size: size, Width: width, Height: height, UploadedBy: uploadedBy, UploadedByType: uploadedByType, UploadedAt: now(), SourceURL: raw, IP: a.clientIP(r)}
	im.UpdatedAt = im.UploadedAt
	im.URL = "/i/" + filename
	if err := a.saveImage(im); err != nil {
		os.Remove(dest)
		return Image{}, err
	}
	a.enqueueNotification("upload", "图片上传", original+" 已从 URL 上传", map[string]any{"id": im.ID, "filename": im.Filename, "url": im.URL, "size": im.Size, "ip": im.IP, "type": im.UploadedByType})
	return im, nil
}

func (a *App) uploadURL(w http.ResponseWriter, r *http.Request) {
	name, kind, valid := a.urlUploader(w, r)
	if !valid {
		return
	}
	var body struct {
		URL          json.RawMessage `json:"url"`
		ReturnBase64 bool            `json:"returnBase64"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) != nil {
		fail(w, 400, "无效请求")
		return
	}
	var urls []string
	array := false
	if len(body.URL) > 0 && body.URL[0] == '[' {
		array = true
		if json.Unmarshal(body.URL, &urls) != nil {
			fail(w, 400, "无效图片 URL")
			return
		}
	} else {
		var single string
		if json.Unmarshal(body.URL, &single) != nil {
			fail(w, 400, "无效图片 URL")
			return
		}
		urls = []string{single}
	}
	if len(urls) == 0 || len(urls) > 1000 {
		fail(w, 400, "请提供 1–1000 个图片 URL")
		return
	}
	if body.ReturnBase64 && len(urls) > 10 {
		fail(w, 400, "Base64 批量返回最多支持 10 张图片")
		return
	}
	maxSize := privateLimit(a)
	if body.ReturnBase64 && maxSize > 4<<20 {
		maxSize = 4 << 20
	}
	if !a.acquireUploadSlot(w, r) {
		return
	}
	defer func() { <-a.limiter }()
	results := make([]map[string]any, 0, len(urls))
	errors := make([]map[string]any, 0)
	for _, raw := range urls {
		im, err := a.importRemoteImage(r, raw, name, kind, maxSize)
		if err != nil {
			errors = append(errors, map[string]any{"success": false, "url": raw, "error": err.Error()})
			continue
		}
		data := map[string]any{"id": im.ID, "uuid": im.UUID, "filename": im.Filename, "format": im.Format, "size": im.Size, "width": im.Width, "height": im.Height, "url": im.URL, "uploadedAt": im.UploadedAt, "uploadedByType": im.UploadedByType}
		if body.ReturnBase64 {
			b, err := os.ReadFile(filepath.Join(a.DataDir, "uploads", im.Filename))
			if err != nil {
				errors = append(errors, map[string]any{"success": false, "url": raw, "error": "读取 Base64 图片失败"})
				continue
			}
			data["base64"] = base64.StdEncoding.EncodeToString(b)
		}
		results = append(results, map[string]any{"success": true, "url": raw, "data": data})
	}
	if array {
		ok(w, map[string]any{"results": results, "errors": errors, "total": len(urls), "successCount": len(results), "errorCount": len(errors)})
		return
	}
	if len(results) == 0 {
		fail(w, 400, errors[0]["error"].(string))
		return
	}
	ok(w, results[0]["data"])
}

func (a *App) uploadURLs(w http.ResponseWriter, r *http.Request) {
	name, kind, valid := a.urlUploader(w, r)
	if !valid {
		return
	}
	var body struct {
		URLs []string `json:"urls"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) != nil || len(body.URLs) == 0 || len(body.URLs) > 1000 {
		fail(w, 400, "请提供 1–1000 个图片 URL")
		return
	}
	seen := make(map[string]bool, len(body.URLs))
	urls := make([]string, 0, len(body.URLs))
	for _, raw := range body.URLs {
		raw = strings.TrimSpace(raw)
		if raw != "" && !seen[raw] {
			seen[raw] = true
			urls = append(urls, raw)
		}
	}
	if len(urls) == 0 {
		fail(w, 400, "请提供有效的图片 URL")
		return
	}
	if !a.acquireUploadSlot(w, r) {
		return
	}
	defer func() { <-a.limiter }()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	send := func(event string, value any) {
		b, _ := json.Marshal(value)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	send("start", map[string]any{"total": len(urls)})
	success, failed := 0, 0
	for i, raw := range urls {
		if r.Context().Err() != nil {
			return
		}
		im, err := a.importRemoteImage(r, raw, name, kind, privateLimit(a))
		if err != nil {
			failed++
			send("progress", map[string]any{"index": i + 1, "total": len(urls), "url": raw, "status": "error", "error": err.Error()})
			continue
		}
		success++
		send("progress", map[string]any{"index": i + 1, "total": len(urls), "url": raw, "status": "success", "data": im})
	}
	send("complete", map[string]any{"total": len(urls), "successCount": success, "failCount": failed})
}
