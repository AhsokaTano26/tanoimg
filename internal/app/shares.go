package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ensureShareSchema(a *App) error {
	for _, query := range []string{
		`CREATE TABLE IF NOT EXISTS image_shares (id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, title TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, expires_at INTEGER NOT NULL DEFAULT 0, revoked_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS image_share_items (share_id TEXT NOT NULL, image_id TEXT NOT NULL, position INTEGER NOT NULL, PRIMARY KEY(share_id,image_id))`,
		`CREATE INDEX IF NOT EXISTS image_share_items_image ON image_share_items(image_id)`,
	} {
		if _, err := a.DB.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) registerShareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/shares", a.createShare)
	mux.HandleFunc("GET /api/admin/shares", a.listShares)
	mux.HandleFunc("DELETE /api/admin/shares/{id}", a.revokeShare)
	mux.HandleFunc("GET /api/shares/{token}", a.getShare)
	mux.HandleFunc("GET /api/shares/{token}/files/{filename}", a.shareFile)
}

func shareToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (a *App) createShare(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		ImageIDs      []string `json:"imageIds"`
		Title         string   `json:"title"`
		ExpiresInDays int      `json:"expiresInDays"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || len(body.ImageIDs) < 1 || len(body.ImageIDs) > 100 || len([]rune(body.Title)) > 200 || body.ExpiresInDays < 0 || body.ExpiresInDays > 3650 {
		fail(w, 400, "分享内容无效")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "分享内容无效")
		return
	}
	seen := make(map[string]bool, len(body.ImageIDs))
	ids := make([]string, 0, len(body.ImageIDs))
	for _, id := range body.ImageIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fail(w, 400, "请选择图片")
		return
	}
	// Validate all targets before storing any capability record.
	for _, id := range ids {
		var deleted, nsfw bool
		if err := a.DB.QueryRow(`SELECT is_deleted,is_nsfw FROM images WHERE id=?`, id).Scan(&deleted, &nsfw); err != nil || deleted || nsfw {
			fail(w, 404, "部分图片不存在或不可分享")
			return
		}
	}
	token, err := shareToken()
	if err != nil {
		fail(w, 500, "创建分享失败")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, 500, "创建分享失败")
		return
	}
	expires := int64(0)
	if body.ExpiresInDays > 0 {
		expires = time.Now().UTC().Add(time.Duration(body.ExpiresInDays) * 24 * time.Hour).Unix()
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "创建分享失败")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO image_shares(id,token_hash,title,created_at,expires_at) VALUES(?,?,?,?,?)`, id, tokenHash(token), strings.TrimSpace(body.Title), now(), expires); err != nil {
		fail(w, 500, "创建分享失败")
		return
	}
	for position, imageID := range ids {
		if _, err = tx.Exec(`INSERT INTO image_share_items(share_id,image_id,position) VALUES(?,?,?)`, id, imageID, position); err != nil {
			fail(w, 500, "创建分享失败")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "创建分享失败")
		return
	}
	ok(w, map[string]any{"id": id, "url": "/share/" + token, "imageCount": len(ids), "expiresAt": expires})
}

type resolvedShare struct {
	ID, Title, CreatedAt string
	ExpiresAt            int64
}

func (a *App) resolveShare(token string) (resolvedShare, error) {
	if len(token) != 43 {
		return resolvedShare{}, sql.ErrNoRows
	}
	var share resolvedShare
	err := a.DB.QueryRow(`SELECT id,title,created_at,expires_at FROM image_shares WHERE token_hash=? AND revoked_at='' AND (expires_at=0 OR expires_at>?)`, tokenHash(token), time.Now().Unix()).Scan(&share.ID, &share.Title, &share.CreatedAt, &share.ExpiresAt)
	return share, err
}

func (a *App) getShare(w http.ResponseWriter, r *http.Request) {
	share, err := a.resolveShare(r.PathValue("token"))
	if err != nil {
		fail(w, 404, "分享不存在或已过期")
		return
	}
	rows, err := a.DB.Query(`SELECT i.id,i.filename,i.original_name,i.format,i.size,i.width,i.height,i.alt,i.author,i.license,i.tags_json FROM image_share_items s JOIN images i ON i.id=s.image_id WHERE s.share_id=? AND i.is_deleted=0 AND i.is_nsfw=0 ORDER BY s.position`, share.ID)
	if err != nil {
		fail(w, 500, "读取分享失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, 8)
	for rows.Next() {
		var id, filename, name, format, alt, author, license, tagsJSON string
		var size int64
		var width, height int
		if err := rows.Scan(&id, &filename, &name, &format, &size, &width, &height, &alt, &author, &license, &tagsJSON); err != nil {
			fail(w, 500, "读取分享失败")
			return
		}
		if !safeFilename.MatchString(filename) {
			continue
		}
		var tags []string
		_ = json.Unmarshal([]byte(tagsJSON), &tags)
		if tags == nil {
			tags = []string{}
		}
		items = append(items, map[string]any{"id": id, "filename": filename, "originalName": name, "format": format, "size": size, "width": width, "height": height, "alt": alt, "author": author, "license": license, "tags": tags, "url": "/api/shares/" + r.PathValue("token") + "/files/" + filename})
	}
	if rows.Err() != nil {
		fail(w, 500, "读取分享失败")
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	ok(w, map[string]any{"id": share.ID, "title": share.Title, "createdAt": share.CreatedAt, "expiresAt": share.ExpiresAt, "images": items})
}

func (a *App) shareFile(w http.ResponseWriter, r *http.Request) {
	share, err := a.resolveShare(r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	filename := r.PathValue("filename")
	if !safeFilename.MatchString(filename) {
		http.NotFound(w, r)
		return
	}
	var format string
	err = a.DB.QueryRow(`SELECT i.format FROM image_share_items s JOIN images i ON i.id=s.image_id WHERE s.share_id=? AND i.filename=? AND i.is_deleted=0 AND i.is_nsfw=0`, share.ID, filename).Scan(&format)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	filePath := filepath.Join(a.DataDir, "uploads", filename)
	info, err := os.Lstat(filePath)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mimeFor(format))
	w.Header().Set("Content-Security-Policy", "sandbox")
	if format == "svg" {
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func (a *App) listShares(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	page, limit := pageParams(r)
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM image_shares`).Scan(&total); err != nil {
		fail(w, 500, "读取分享失败")
		return
	}
	rows, err := a.DB.Query(`SELECT s.id,s.title,s.created_at,s.expires_at,s.revoked_at,count(i.image_id) FROM image_shares s LEFT JOIN image_share_items i ON i.share_id=s.id GROUP BY s.id ORDER BY s.created_at DESC,s.id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		fail(w, 500, "读取分享失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id, title, created, revoked string
		var expires int64
		var count int
		if err := rows.Scan(&id, &title, &created, &expires, &revoked, &count); err != nil {
			fail(w, 500, "读取分享失败")
			return
		}
		items = append(items, map[string]any{"id": id, "title": title, "createdAt": created, "expiresAt": expires, "revokedAt": revoked, "imageCount": count})
	}
	if rows.Err() != nil {
		fail(w, 500, "读取分享失败")
		return
	}
	ok(w, map[string]any{"shares": items, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}

func (a *App) revokeShare(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	result, err := a.DB.Exec(`UPDATE image_shares SET revoked_at=? WHERE id=? AND revoked_at=''`, now(), r.PathValue("id"))
	if err != nil {
		fail(w, 500, "撤销分享失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "分享不存在或已撤销")
		return
	}
	ok(w, map[string]bool{"revoked": true})
}
