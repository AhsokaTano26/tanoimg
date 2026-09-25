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

func (a *App) urlUploader(w http.ResponseWriter, r *http.Request, visibility string) (string, string, apiKeyPrincipal, bool) {
	if a.userID(r) != "" {
		var name string
		if a.DB.QueryRow(`SELECT username FROM users WHERE id=?`, a.userID(r)).Scan(&name) == nil {
			return name, "url", apiKeyPrincipal{}, true
		}
		return "管理员", "url", apiKeyPrincipal{}, true
	}
	if principal, ok := a.uploadIdentity(w, r, "upload:url", visibility); ok {
		return principal.Name, "url-api", principal, true
	}
	return "", "", apiKeyPrincipal{}, false
}

func (a *App) importRemoteImage(r *http.Request, raw, uploadedBy, uploadedByType string, maxSize int64, metadata Image, principal apiKeyPrincipal, key string) (Image, error) {
	return a.importRemoteWithConfig(r, raw, uploadedBy, uploadedByType, maxSize, privateUploadConfig(a), false, metadata, principal, key)
}
func (a *App) importRemoteWithConfig(r *http.Request, raw, uploadedBy, uploadedByType string, maxSize int64, config uploadConfig, enforceFormats bool, metadata Image, principal apiKeyPrincipal, key string) (Image, error) {
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
	defer func(downloaded *os.File) { downloaded.Close(); os.Remove(downloaded.Name()) }(f)
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
	format, err = inspectImageFormat(f, format)
	if err != nil {
		return Image{}, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Image{}, err
	}
	if enforceFormats && !formatAllowed(config.AllowedFormats, format) {
		return Image{}, errors.New("该图片格式未开放上传")
	}
	if principal.ID != "" && !principal.AllowsFormat(format) {
		return Image{}, errors.New("API Key 不允许上传该图片格式")
	}
	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	processed, processedFormat, processedSize, err := a.processImageFile(r.Context(), f, format, size, config, 200<<10)
	if err != nil {
		return Image{}, err
	}
	if processed != f {
		defer func() { processed.Close(); os.Remove(processed.Name()) }()
		f = processed
		format, size = processedFormat, processedSize
	}
	stripped, strippedSize, err := a.maybeStripJPEGMetadata(f, format, a.lifecycleSettings().StripJPEGMetadata)
	if err != nil {
		return Image{}, err
	}
	if stripped != f {
		defer func() { stripped.Close(); os.Remove(stripped.Name()) }()
		f, size = stripped, strippedSize
	}
	digest, err := fileMD5(f)
	if err != nil {
		return Image{}, err
	}
	duplicate := ""
	admin, apiKeyID := a.duplicateOwner(r)
	if admin || apiKeyID != "" {
		duplicate, err = a.findExactDuplicate(f, digest, size, admin, apiKeyID)
		if err != nil {
			return Image{}, err
		}
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
	if err := a.bindUploadKey(r, strings.HasPrefix(r.URL.Path, "/api/upload/public"), key, id); err != nil {
		return Image{}, err
	}
	reservation, err := a.reserveUploadQuota(r.Context(), principal, a.clientIP(r), id, size)
	if err != nil {
		return Image{}, err
	}
	committed := false
	defer func() { _ = a.finishUploadQuota(nil, reservation, committed) }()
	if err := os.Rename(f.Name(), dest); err != nil {
		return Image{}, err
	}
	original := path.Base(u.Path)
	if original == "." || original == "/" || original == "" {
		original = "image." + format
	}
	visibility, _ := uploadVisibility(r)
	if uploadedByType == "public" {
		visibility = "public"
	}
	im := Image{ID: id, UUID: uuid, Filename: filename, OriginalName: original, Format: format, Size: size, Width: width, Height: height, UploadedBy: uploadedBy, UploadedByType: uploadedByType, Visibility: visibility, UploadedAt: now(), SourceURL: raw, IP: a.clientIP(r), APIKeyID: apiKeyID, Alt: metadata.Alt, Author: metadata.Author, License: metadata.License, Tags: metadata.Tags, MD5: digest, DuplicateOf: duplicate}
	if uploadedByType == "public" {
		if publicConfig(a).ContentSafety.Enabled {
			im.ModerationStatus = "pending"
		} else {
			im.ModerationStatus = "skipped"
			im.ModerationChecked = true
		}
	}
	im.UpdatedAt = im.UploadedAt
	im.URL = "/i/" + filename
	if err := a.saveImage(im); err != nil {
		os.Remove(dest)
		return Image{}, err
	}
	if im.ModerationStatus == "pending" {
		if err := a.enqueueModeration(im); err != nil {
			a.DB.Exec(`DELETE FROM images WHERE id=?`, im.ID)
			os.Remove(dest)
			return Image{}, err
		}
	}
	if r.URL.Path == "/api/upload/public/url" && !admin && apiKeyID == "" {
		im.Receipt, err = a.issueUploadReceipt(im.ID)
		if err != nil {
			a.DB.Exec(`DELETE FROM moderation_tasks WHERE image_id=?`, im.ID)
			a.DB.Exec(`DELETE FROM images WHERE id=?`, im.ID)
			os.Remove(dest)
			return Image{}, err
		}
	}
	a.enqueueNotification("upload", "图片上传", original+" 已从 URL 上传", map[string]any{"id": im.ID, "filename": im.Filename, "url": im.URL, "size": im.Size, "ip": im.IP, "type": im.UploadedByType})
	committed = true
	return im, nil
}

func (a *App) uploadURL(w http.ResponseWriter, r *http.Request) {
	visibility, validVisibility := uploadVisibility(r)
	if !validVisibility {
		fail(w, 400, "visibility 必须为 public、unlisted 或 private")
		return
	}
	name, kind, principal, valid := a.urlUploader(w, r, visibility)
	if !valid {
		return
	}
	if visibility == "public" {
		kind = "public"
	}
	var body struct {
		URL          json.RawMessage `json:"url"`
		ReturnBase64 bool            `json:"returnBase64"`
		Alt          string          `json:"alt"`
		Author       string          `json:"author"`
		License      string          `json:"license"`
		Tags         []string        `json:"tags"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) != nil {
		fail(w, 400, "无效请求")
		return
	}
	var metadata Image
	if err := applyUploadMetadata(&metadata, body.Alt, body.Author, body.License, body.Tags); err != nil {
		fail(w, 400, err.Error())
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
	if len(urls) > 1 && r.Header.Get("Idempotency-Key") != "" {
		fail(w, 400, "批量 URL 上传请为每张图片分别使用幂等键")
		return
	}
	maxSize := privateLimit(a)
	maxSize = principal.MaxFileSize(maxSize)
	if body.ReturnBase64 && maxSize > 4<<20 {
		maxSize = 4 << 20
	}
	key := ""
	if len(urls) == 1 {
		var proceed bool
		key, proceed = a.claimOrReplayUpload(w, r, false)
		if !proceed {
			return
		}
		defer func() { _ = a.finishUploadKey(r, false, key, "") }()
	}
	results := make([]map[string]any, 0, len(urls))
	errors := make([]map[string]any, 0)
	for _, raw := range urls {
		releaseSlot, allowed := a.acquireFairUploadSlot(w, r, false)
		if !allowed {
			return
		}
		im, err := a.importRemoteImage(r, raw, name, kind, maxSize, metadata, principal, key)
		releaseSlot()
		if err != nil {
			errors = append(errors, map[string]any{"success": false, "url": raw, "error": err.Error()})
			continue
		}
		_ = a.finishUploadKey(r, false, key, im.ID)
		key = ""
		data := map[string]any{"id": im.ID, "uuid": im.UUID, "filename": im.Filename, "format": im.Format, "size": im.Size, "width": im.Width, "height": im.Height, "url": im.URL, "uploadedAt": im.UploadedAt, "uploadedByType": im.UploadedByType, "visibility": im.Visibility, "alt": im.Alt, "author": im.Author, "license": im.License, "tags": im.Tags, "md5": im.MD5, "duplicateOf": im.DuplicateOf}
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
		if len(urls) == 1 && errors[0]["error"] == errDailyQuotaExceeded.Error() {
			w.Header().Set("Retry-After", "86400")
			fail(w, 429, "已达到今日上传数量或流量配额")
			return
		}
		fail(w, 400, errors[0]["error"].(string))
		return
	}
	ok(w, results[0]["data"])
}

func (a *App) uploadURLs(w http.ResponseWriter, r *http.Request) {
	visibility, validVisibility := uploadVisibility(r)
	if !validVisibility {
		fail(w, 400, "visibility 必须为 public、unlisted 或 private")
		return
	}
	name, kind, principal, valid := a.urlUploader(w, r, visibility)
	if !valid {
		return
	}
	if visibility == "public" {
		kind = "public"
	}
	var body struct {
		URLs    []string `json:"urls"`
		Alt     string   `json:"alt"`
		Author  string   `json:"author"`
		License string   `json:"license"`
		Tags    []string `json:"tags"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) != nil || len(body.URLs) == 0 || len(body.URLs) > 1000 {
		fail(w, 400, "请提供 1–1000 个图片 URL")
		return
	}
	var metadata Image
	if err := applyUploadMetadata(&metadata, body.Alt, body.Author, body.License, body.Tags); err != nil {
		fail(w, 400, err.Error())
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
		releaseSlot, allowed := a.acquireFairUploadSlot(w, r, false)
		if !allowed {
			return
		}
		im, err := a.importRemoteImage(r, raw, name, kind, principal.MaxFileSize(privateLimit(a)), metadata, principal, "")
		releaseSlot()
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

func (a *App) uploadPublicURL(w http.ResponseWriter, r *http.Request) {
	c := publicConfig(a)
	key, proceed := a.claimOrReplayUpload(w, r, true)
	if !proceed {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = a.finishUploadKey(r, true, key, "")
		}
	}()
	release, allowed := a.beginPublicUpload(w, r, c)
	if !allowed {
		return
	}
	defer release()
	var body struct {
		URL     string   `json:"url"`
		Alt     string   `json:"alt"`
		Author  string   `json:"author"`
		License string   `json:"license"`
		Tags    []string `json:"tags"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil || len(body.URL) > 2048 {
		fail(w, 400, "请提供一个有效的图片 URL")
		return
	}
	var metadata Image
	if err := applyUploadMetadata(&metadata, body.Alt, body.Author, body.License, body.Tags); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if _, err := parseRemoteURL(body.URL); err != nil {
		fail(w, 400, err.Error())
		return
	}
	releaseSlot, allowed := a.acquireFairUploadSlot(w, r, true)
	if !allowed {
		return
	}
	defer releaseSlot()
	if !a.verifyPublicTurnstile(w, r) {
		return
	}
	im, err := a.importRemoteWithConfig(r, body.URL, "访客", "public", c.MaxFileSize, c, true, metadata, apiKeyPrincipal{}, key)
	if err != nil {
		if errors.Is(err, errDailyQuotaExceeded) {
			w.Header().Set("Retry-After", "86400")
			fail(w, 429, "已达到今日上传数量或流量配额")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	committed = true
	_ = a.finishUploadKey(r, true, key, im.ID)
	ok(w, im)
}
