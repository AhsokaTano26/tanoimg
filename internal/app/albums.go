package app

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type Album struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	ImageCount  int    `json:"imageCount"`
}

func ensureAlbumSchema(a *App) error {
	for _, query := range []string{
		`CREATE TABLE IF NOT EXISTS albums (id TEXT PRIMARY KEY,title TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',visibility TEXT NOT NULL DEFAULT 'unlisted',created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS album_images (album_id TEXT NOT NULL,image_id TEXT NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(album_id,image_id))`,
		`CREATE INDEX IF NOT EXISTS album_images_order ON album_images(album_id,position)`,
	} {
		if _, err := a.DB.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) registerAlbumRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/albums", a.publicAlbums)
	mux.HandleFunc("GET /api/albums/{id}", a.publicAlbum)
	mux.HandleFunc("GET /api/admin/albums", a.adminAlbums)
	mux.HandleFunc("POST /api/admin/albums", a.createAlbum)
	mux.HandleFunc("GET /api/admin/albums/{id}", a.adminAlbum)
	mux.HandleFunc("PUT /api/admin/albums/{id}", a.updateAlbum)
	mux.HandleFunc("DELETE /api/admin/albums/{id}", a.deleteAlbum)
	mux.HandleFunc("PUT /api/admin/albums/{id}/images", a.setAlbumImages)
}

func albumInput(w http.ResponseWriter, r *http.Request) (Album, bool) {
	var input Album
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		fail(w, 400, "相册内容无效")
		return Album{}, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "相册内容无效")
		return Album{}, false
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if input.Title == "" || len([]rune(input.Title)) > 120 || len([]rune(input.Description)) > 1000 || !validVisibility(input.Visibility) {
		fail(w, 400, "相册标题、描述或可见性无效")
		return Album{}, false
	}
	return input, true
}

func (a *App) createAlbum(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	input, valid := albumInput(w, r)
	if !valid {
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, 500, "创建相册失败")
		return
	}
	stamp := now()
	_, err = a.DB.Exec(`INSERT INTO albums(id,title,description,visibility,created_at,updated_at) VALUES(?,?,?,?,?,?)`, id, input.Title, input.Description, input.Visibility, stamp, stamp)
	if err != nil {
		fail(w, 500, "创建相册失败")
		return
	}
	ok(w, Album{ID: id, Title: input.Title, Description: input.Description, Visibility: input.Visibility, CreatedAt: stamp, UpdatedAt: stamp})
}

func (a *App) updateAlbum(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	input, valid := albumInput(w, r)
	if !valid {
		return
	}
	result, err := a.DB.Exec(`UPDATE albums SET title=?,description=?,visibility=?,updated_at=? WHERE id=?`, input.Title, input.Description, input.Visibility, now(), r.PathValue("id"))
	if err != nil {
		fail(w, 500, "更新相册失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "相册不存在")
		return
	}
	a.albumDetail(w, r, true)
}

func (a *App) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "删除相册失败")
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM album_images WHERE album_id=?`, r.PathValue("id")); err != nil {
		fail(w, 500, "删除相册失败")
		return
	}
	result, err := tx.Exec(`DELETE FROM albums WHERE id=?`, r.PathValue("id"))
	if err != nil {
		fail(w, 500, "删除相册失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "相册不存在")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "删除相册失败")
		return
	}
	ok(w, map[string]bool{"deleted": true})
}

func (a *App) setAlbumImages(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		ImageIDs []string `json:"imageIds"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || len(body.ImageIDs) > 1000 {
		fail(w, 400, "相册最多包含 1000 张图片")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "相册内容无效")
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
	// Preflight targets without holding a write transaction on SQLite's sole connection.
	for _, id := range ids {
		var found int
		if err := a.DB.QueryRow(`SELECT 1 FROM images WHERE id=? AND is_deleted=0 AND is_nsfw=0`, id).Scan(&found); err != nil {
			fail(w, 404, "部分图片不存在")
			return
		}
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "保存相册失败")
		return
	}
	defer tx.Rollback()
	var found int
	if err := tx.QueryRow(`SELECT 1 FROM albums WHERE id=?`, r.PathValue("id")).Scan(&found); err != nil {
		fail(w, 404, "相册不存在")
		return
	}
	if _, err := tx.Exec(`DELETE FROM album_images WHERE album_id=?`, r.PathValue("id")); err != nil {
		fail(w, 500, "保存相册失败")
		return
	}
	for position, id := range ids {
		if _, err := tx.Exec(`INSERT INTO album_images(album_id,image_id,position) VALUES(?,?,?)`, r.PathValue("id"), id, position); err != nil {
			fail(w, 500, "保存相册失败")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "保存相册失败")
		return
	}
	ok(w, map[string]int{"imageCount": len(ids)})
}

func (a *App) publicAlbums(w http.ResponseWriter, r *http.Request) { a.listAlbums(w, r, false) }
func (a *App) adminAlbums(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.listAlbums(w, r, true)
}

func (a *App) listAlbums(w http.ResponseWriter, r *http.Request, admin bool) {
	page, limit := pageParams(r)
	where := "a.visibility='public'"
	if admin {
		where = "1=1"
	}
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM albums a WHERE ` + where).Scan(&total); err != nil {
		fail(w, 500, "读取相册失败")
		return
	}
	countExpr := `count(i.image_id)`
	join := `LEFT JOIN album_images i ON i.album_id=a.id`
	if !admin {
		join += ` LEFT JOIN images img ON img.id=i.image_id`
		countExpr = `count(CASE WHEN img.visibility='public' AND img.is_deleted=0 AND img.is_nsfw=0 THEN 1 END)`
	}
	rows, err := a.DB.Query(`SELECT a.id,a.title,a.description,a.visibility,a.created_at,a.updated_at,`+countExpr+` FROM albums a `+join+` WHERE `+where+` GROUP BY a.id ORDER BY a.created_at DESC,a.id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		fail(w, 500, "读取相册失败")
		return
	}
	defer rows.Close()
	items := make([]Album, 0, limit)
	for rows.Next() {
		var album Album
		if err := rows.Scan(&album.ID, &album.Title, &album.Description, &album.Visibility, &album.CreatedAt, &album.UpdatedAt, &album.ImageCount); err != nil {
			fail(w, 500, "读取相册失败")
			return
		}
		items = append(items, album)
	}
	if rows.Err() != nil {
		fail(w, 500, "读取相册失败")
		return
	}
	ok(w, map[string]any{"albums": items, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}

func (a *App) publicAlbum(w http.ResponseWriter, r *http.Request) { a.albumDetail(w, r, false) }
func (a *App) adminAlbum(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.albumDetail(w, r, true)
}

func (a *App) albumDetail(w http.ResponseWriter, r *http.Request, admin bool) {
	var album Album
	err := a.DB.QueryRow(`SELECT id,title,description,visibility,created_at,updated_at FROM albums WHERE id=?`, r.PathValue("id")).Scan(&album.ID, &album.Title, &album.Description, &album.Visibility, &album.CreatedAt, &album.UpdatedAt)
	if err == sql.ErrNoRows {
		fail(w, 404, "相册不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取相册失败")
		return
	}
	if !admin && album.Visibility == "private" {
		fail(w, 404, "相册不存在")
		return
	}
	where := `i.is_deleted=0 AND i.is_nsfw=0`
	if !admin {
		if album.Visibility == "unlisted" {
			where += ` AND i.visibility IN ('public','unlisted')`
		} else {
			where += ` AND i.visibility='public'`
		}
	}
	rows, err := a.DB.Query(`SELECT `+imageColumns+` FROM album_images ai JOIN images i ON i.id=ai.image_id WHERE ai.album_id=? AND `+where+` ORDER BY ai.position`, album.ID)
	if err != nil {
		fail(w, 500, "读取相册失败")
		return
	}
	defer rows.Close()
	images := make([]any, 0, 16)
	for rows.Next() {
		im, err := scanImage(rows)
		if err != nil {
			fail(w, 500, "读取相册失败")
			return
		}
		if admin {
			images = append(images, im)
		} else {
			images = append(images, map[string]any{"id": im.ID, "url": im.URL, "filename": im.Filename, "originalName": im.OriginalName, "size": im.Size, "width": im.Width, "height": im.Height, "alt": im.Alt, "tags": im.Tags})
		}
	}
	if rows.Err() != nil {
		fail(w, 500, "读取相册失败")
		return
	}
	album.ImageCount = len(images)
	ok(w, map[string]any{"album": album, "images": images})
}
