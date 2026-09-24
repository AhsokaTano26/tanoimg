package app

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed web/*
var webFiles embed.FS

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func ok(w http.ResponseWriter, data any) {
	respond(w, 200, map[string]any{"success": true, "data": data})
}
func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]any{"success": false, "message": msg})
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.page)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { ok(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	mux.HandleFunc("GET /api/auth/verify", a.verify)
	mux.HandleFunc("PUT /api/admin/password", a.changePassword)
	mux.HandleFunc("PUT /api/admin/username", a.changeUsername)
	mux.HandleFunc("POST /api/upload/public", func(w http.ResponseWriter, r *http.Request) { a.upload(w, r, true) })
	mux.HandleFunc("POST /api/upload/private", func(w http.ResponseWriter, r *http.Request) { a.upload(w, r, false) })
	mux.HandleFunc("POST /api/upload/url", a.uploadURL)
	mux.HandleFunc("POST /api/upload/urls", a.uploadURLs)
	mux.HandleFunc("GET /api/images", a.images)
	mux.HandleFunc("GET /api/images/deleted", a.deletedImages)
	mux.HandleFunc("DELETE /api/images/batch", a.batchDeleteImages)
	mux.HandleFunc("GET /api/images/nsfw", a.nsfwImages)
	mux.HandleFunc("POST /api/images/nsfw-clear", a.clearNSFWImages)
	mux.HandleFunc("GET /api/images/preview/{filename}", a.previewImage)
	mux.HandleFunc("PUT /api/images/{id}/unmark-nsfw", a.unmarkNSFW)
	mux.HandleFunc("PUT /api/images/{id}/restore", a.restoreImage)
	mux.HandleFunc("DELETE /api/images/{id}", a.deleteImage)
	mux.HandleFunc("POST /api/settings/hard-delete", a.hardDeleteImages)
	mux.HandleFunc("GET /api/config/public", a.getPublicConfig)
	mux.HandleFunc("PUT /api/config/public", a.putPublicConfig)
	mux.HandleFunc("GET /api/config/private", a.getPrivateConfig)
	mux.HandleFunc("PUT /api/config/private", a.putPrivateConfig)
	mux.HandleFunc("GET /api/settings", a.getSettings)
	mux.HandleFunc("PUT /api/settings", a.putSettings)
	mux.HandleFunc("GET /api/settings/public", a.publicSettings)
	mux.HandleFunc("PUT /api/settings/appearance", a.putAppearanceSettings)
	mux.HandleFunc("GET /api/settings/stats", a.stats)
	mux.HandleFunc("GET /api/version/check", a.checkVersion)
	mux.HandleFunc("GET /api/notification", a.getNotification)
	mux.HandleFunc("PUT /api/notification", a.putNotification)
	mux.HandleFunc("POST /api/notification/test", a.testNotification)
	mux.HandleFunc("GET /api/apikeys", a.apiKeys)
	mux.HandleFunc("POST /api/apikeys", a.createAPIKey)
	mux.HandleFunc("PUT /api/apikeys/{id}", a.updateAPIKey)
	mux.HandleFunc("DELETE /api/apikeys/{id}", a.deleteAPIKey)
	mux.HandleFunc("GET /api/blacklist", a.listBlacklist)
	mux.HandleFunc("POST /api/blacklist", a.addBlacklist)
	mux.HandleFunc("DELETE /api/blacklist/{id}", a.deleteBlacklist)
	mux.HandleFunc("GET /i/{filename}", a.imageFile)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !strings.EqualFold(u.Host, r.Host) || (u.Scheme != "http" && u.Scheme != "https") {
					fail(w, http.StatusForbidden, "请求来源不被允许")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *App) page(w http.ResponseWriter, r *http.Request) {
	name := "index.html"
	switch r.URL.Path {
	case "/", "/login", "/gallery", "/upload":
	case "/admin", "/admin/gallery", "/admin/upload", "/admin/recycle", "/admin/settings", "/admin/api", "/admin/stats", "/recycle", "/settings", "/api", "/stats":
		if a.userID(r) == "" {
			target := r.URL.Path
			if !strings.HasPrefix(target, "/admin") {
				target = "/admin" + target
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/login?redirect="+url.QueryEscape(target), http.StatusSeeOther)
			return
		}
	case "/app.css":
		name = "app.css"
	case "/icons.svg":
		name = "icons.svg"
	default:
		name = strings.TrimPrefix(r.URL.Path, "/")
		if !strings.HasPrefix(name, "assets/") || !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
	}
	b, err := webFiles.ReadFile("web/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch path.Ext(name) {
	case ".html":
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js", ".mjs":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(b)
}

func (a *App) images(w http.ResponseWriter, r *http.Request) {
	admin := a.userID(r) != "" && r.URL.Query().Get("scope") != "public"
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	where := `is_deleted=0 AND is_nsfw=0`
	if !admin {
		var cfg struct {
			ShowOnHomepage bool `json:"showOnHomepage"`
		}
		json.Unmarshal(a.setting("privateApiConfig", map[string]any{}), &cfg)
		if !cfg.ShowOnHomepage {
			where += ` AND uploaded_by_type='public'`
		}
	}
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM images WHERE ` + where).Scan(&total); err != nil {
		fail(w, 500, "查询图片失败")
		return
	}
	rows, err := a.DB.Query(`SELECT `+imageColumns+` FROM images WHERE `+where+` ORDER BY uploaded_at DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		fail(w, 500, "查询图片失败")
		return
	}
	defer rows.Close()
	images := make([]any, 0, limit)
	for rows.Next() {
		im, err := scanImage(rows)
		if err != nil {
			fail(w, 500, "查询图片失败")
			return
		}
		if admin {
			images = append(images, im)
		} else {
			images = append(images, map[string]any{"id": im.ID, "uuid": im.UUID, "filename": im.Filename, "originalName": im.OriginalName, "format": im.Format, "size": im.Size, "width": im.Width, "height": im.Height, "url": im.URL, "uploadedBy": im.UploadedBy, "uploadedAt": im.UploadedAt})
		}
	}
	if rows.Err() != nil {
		fail(w, 500, "查询图片失败")
		return
	}
	ok(w, map[string]any{"images": images, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}

func (a *App) deleteImage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	result, err := a.DB.Exec(`UPDATE images SET is_deleted=1,updated_at=? WHERE id=?`, now(), r.PathValue("id"))
	if err != nil {
		fail(w, 500, "删除失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "图片不存在")
		return
	}
	ok(w, nil)
}

func (a *App) imageFile(w http.ResponseWriter, r *http.Request) {
	a.serveStoredImage(w, r, r.PathValue("filename"), false)
}

func (a *App) serveStoredImage(w http.ResponseWriter, r *http.Request, filename string, privileged bool) {
	if !safeFilename.MatchString(filename) {
		http.NotFound(w, r)
		return
	}
	uuid := strings.TrimSuffix(filename, path.Ext(filename))
	im, err := a.getImageByUUID(uuid)
	if err != nil || im.Filename != filename || (!privileged && im.IsDeleted) {
		http.NotFound(w, r)
		return
	}
	if im.IsNsfw && !privileged {
		http.Error(w, "图片不可访问", 403)
		return
	}
	f, err := os.Open(filepath.Join(a.DataDir, "uploads", filename))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mimeFor(im.Format))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if im.Format == "svg" {
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'")
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func mimeFor(format string) string {
	switch format {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "apng":
		return "image/apng"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "avif":
		return "image/avif"
	case "bmp":
		return "image/bmp"
	case "ico":
		return "image/x-icon"
	case "svg":
		return "image/svg+xml"
	case "tif", "tiff":
		return "image/tiff"
	}
	return "application/octet-stream"
}

func (a *App) getPublicConfig(w http.ResponseWriter, r *http.Request) {
	c := publicConfig(a)
	if a.userID(r) == "" {
		ok(w, map[string]any{"enabled": c.Enabled, "allowedFormats": c.AllowedFormats, "maxFileSize": c.MaxFileSize, "allowConcurrent": c.AllowConcurrent})
		return
	}
	ok(w, a.setting("publicApiConfig", c))
}

func (a *App) putPublicConfig(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var patch map[string]json.RawMessage
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&patch) != nil || patch == nil {
		fail(w, 400, "无效配置")
		return
	}
	var merged map[string]json.RawMessage
	if json.Unmarshal(a.setting("publicApiConfig", publicConfig(a)), &merged) != nil || merged == nil {
		merged = make(map[string]json.RawMessage)
	}
	for key, value := range patch {
		merged[key] = value
	}
	if raw, present := patch["contentSafety"]; present {
		var safety contentSafetyConfig
		if json.Unmarshal(raw, &safety) != nil {
			fail(w, 400, "审核配置无效")
			return
		}
		if safety.Provider != "" && safety.Provider != "nsfwdet" && safety.Provider != "elysiatools" && safety.Provider != "nsfw_detector" {
			fail(w, 400, "不支持的审核服务")
			return
		}
		for _, provider := range safety.Providers {
			if provider.Threshold < 0 || provider.Threshold > 1 {
				fail(w, 400, "审核阈值无效")
				return
			}
			for _, target := range []string{provider.APIURL, provider.UploadURL} {
				if target != "" {
					if _, err := parseRemoteURL(target); err != nil {
						fail(w, 400, "审核服务地址无效")
						return
					}
				}
			}
		}
	}
	b, _ := json.Marshal(merged)
	var c uploadConfig
	if json.Unmarshal(b, &c) != nil {
		fail(w, 400, "无效配置")
		return
	}
	conversions := 0
	for _, enabled := range []bool{c.ConvertToWebp, c.ConvertToPng, c.ConvertToJpg} {
		if enabled {
			conversions++
		}
	}
	if c.MaxFileSize < 1 || c.MaxFileSize > 100<<20 || len(c.AllowedFormats) == 0 || c.RateLimit < 0 || c.RateLimit > 1000 || (c.CompressionQuality != 0 && (c.CompressionQuality < 1 || c.CompressionQuality > 100)) || conversions > 1 {
		fail(w, 400, "无效配置")
		return
	}
	if err := a.setSetting("publicApiConfig", b); err != nil {
		fail(w, 500, "保存配置失败")
		return
	}
	ok(w, merged)
}

func (a *App) publicSettings(w http.ResponseWriter, r *http.Request) {
	var c map[string]any
	json.Unmarshal(a.setting("appSettings", map[string]any{"appName": "TanoImg"}), &c)
	delete(c, "siteUrl")
	ok(w, c)
}

func (a *App) putAppearanceSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var input struct {
		BackgroundURL  string `json:"backgroundUrl"`
		BackgroundBlur int    `json:"backgroundBlur"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.BackgroundBlur < 0 || input.BackgroundBlur > 40 || len(input.BackgroundURL) > 2048 || !validBackgroundURL(input.BackgroundURL) {
		fail(w, 400, "背景设置无效")
		return
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(a.setting("appSettings", map[string]any{"appName": "TanoImg"}), &settings) != nil || settings == nil {
		settings = make(map[string]json.RawMessage)
	}
	settings["backgroundUrl"], _ = json.Marshal(input.BackgroundURL)
	settings["backgroundBlur"], _ = json.Marshal(input.BackgroundBlur)
	b, _ := json.Marshal(settings)
	if err := a.setSetting("appSettings", b); err != nil {
		fail(w, 500, "保存背景设置失败")
		return
	}
	ok(w, input)
}

func validBackgroundURL(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	if err != nil || u.Fragment != "" || u.User != nil {
		return false
	}
	if u.Scheme == "https" {
		return u.Hostname() != ""
	}
	return u.Scheme == "" && u.Host == "" && u.RawQuery == "" && strings.HasPrefix(u.Path, "/i/") && path.Base(u.Path) == strings.TrimPrefix(u.Path, "/i/") && path.Base(u.Path) != "."
}

func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var total, public, private, deleted, nsfw, moderated int
	var activeSize, deletedSize int64
	err := a.DB.QueryRow(`SELECT coalesce(sum(CASE WHEN is_deleted=0 THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN is_deleted=0 AND uploaded_by_type='public' THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN is_deleted=0 AND uploaded_by_type!='public' THEN 1 ELSE 0 END),0),coalesce(sum(is_deleted),0),coalesce(sum(CASE WHEN is_deleted=0 THEN size ELSE 0 END),0),coalesce(sum(CASE WHEN is_deleted=1 THEN size ELSE 0 END),0),coalesce(sum(is_nsfw),0),coalesce(sum(moderation_checked),0) FROM images`).Scan(&total, &public, &private, &deleted, &activeSize, &deletedSize, &nsfw, &moderated)
	if err != nil {
		fail(w, 500, "读取统计失败")
		return
	}
	rate := 0.0
	if moderated > 0 {
		rate = float64(nsfw) * 100 / float64(moderated)
	}
	ok(w, map[string]any{"totalImages": total, "publicImages": public, "privateImages": private, "deletedImagesCount": deleted, "activeSize": activeSize, "deletedSize": deletedSize, "totalSize": activeSize + deletedSize, "nsfwImagesCount": nsfw, "moderatedImagesCount": moderated, "nsfwRate": rate})
}

func (a *App) apiKeys(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	rows, err := a.DB.Query(`SELECT id,key,name,enabled,is_default,created_at FROM apikeys ORDER BY created_at`)
	if err != nil {
		fail(w, 500, "查询密钥失败")
		return
	}
	defer rows.Close()
	list := make([]map[string]any, 0)
	for rows.Next() {
		var id, key, name, created string
		var enabled, def bool
		if rows.Scan(&id, &key, &name, &enabled, &def, &created) != nil {
			continue
		}
		list = append(list, map[string]any{"id": id, "key": key, "name": name, "enabled": enabled, "isDefault": def, "createdAt": created})
	}
	ok(w, list)
}

func (a *App) createAPIKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil || strings.TrimSpace(body.Name) == "" {
		fail(w, 400, "请输入密钥名称")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, 500, "创建密钥失败")
		return
	}
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		fail(w, 500, "创建密钥失败")
		return
	}
	key := "sk-" + hex.EncodeToString(b)
	_, err = a.DB.Exec(`INSERT INTO apikeys(id,key,name,enabled,is_default,created_at) VALUES(?,?,?,1,0,?)`, id, key, strings.TrimSpace(body.Name), now())
	if err != nil {
		fail(w, 500, "创建密钥失败")
		return
	}
	ok(w, map[string]any{"id": id, "key": key, "name": body.Name, "enabled": true})
}

func (a *App) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	result, err := a.DB.Exec(`DELETE FROM apikeys WHERE id=?`, r.PathValue("id"))
	if err != nil {
		fail(w, 500, "删除密钥失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "密钥不存在")
		return
	}
	ok(w, nil)
}

func (a *App) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		Name       *string `json:"name"`
		Enabled    *bool   `json:"enabled"`
		Regenerate bool    `json:"regenerate"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		fail(w, 400, "无效请求")
		return
	}
	var key, name, created string
	var enabled, isDefault bool
	id := r.PathValue("id")
	if err := a.DB.QueryRow(`SELECT key,name,enabled,is_default,created_at FROM apikeys WHERE id=?`, id).Scan(&key, &name, &enabled, &isDefault, &created); err != nil {
		fail(w, 404, "密钥不存在")
		return
	}
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
		if name == "" || len(name) > 100 {
			fail(w, 400, "密钥名称无效")
			return
		}
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.Regenerate {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			fail(w, 500, "重新生成密钥失败")
			return
		}
		key = "sk-" + hex.EncodeToString(b)
	}
	updated := now()
	if _, err := a.DB.Exec(`UPDATE apikeys SET key=?,name=?,enabled=? WHERE id=?`, key, name, enabled, id); err != nil {
		fail(w, 500, "更新密钥失败")
		return
	}
	ok(w, map[string]any{"id": id, "key": key, "name": name, "enabled": enabled, "isDefault": isDefault, "createdAt": created, "updatedAt": updated})
}
