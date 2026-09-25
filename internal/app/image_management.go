package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *App) batchDeleteImages(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) != nil || len(body.IDs) == 0 || len(body.IDs) > 1000 {
		fail(w, 400, "请选择 1–1000 张图片")
		return
	}
	actor := a.userID(r)
	deletedAt := now()
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "批量删除失败")
		return
	}
	defer tx.Rollback()
	count := int64(0)
	for _, id := range body.IDs {
		if id == "" {
			continue
		}
		result, err := tx.Exec(`UPDATE images SET is_deleted=1,updated_at=?,deleted_at=?,deleted_by=? WHERE id=? AND is_deleted=0`, deletedAt, deletedAt, actor, id)
		if err != nil {
			fail(w, 500, "批量删除失败")
			return
		}
		n, _ := result.RowsAffected()
		count += n
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "批量删除失败")
		return
	}
	ok(w, map[string]int64{"deletedCount": count})
}

func pageParams(r *http.Request) (int, int) {
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
	return page, limit
}

func (a *App) deletedImages(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	page, limit := pageParams(r)
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM images WHERE is_deleted=1 AND is_nsfw=0`).Scan(&total); err != nil {
		fail(w, 500, "查询回收站失败")
		return
	}
	rows, err := a.DB.Query(`SELECT `+imageColumns+` FROM images WHERE is_deleted=1 AND is_nsfw=0 ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		fail(w, 500, "查询回收站失败")
		return
	}
	defer rows.Close()
	images := make([]Image, 0, limit)
	for rows.Next() {
		im, err := scanImage(rows)
		if err != nil {
			fail(w, 500, "查询回收站失败")
			return
		}
		im.URL = "/api/images/preview/" + im.Filename
		images = append(images, im)
	}
	if rows.Err() != nil {
		fail(w, 500, "查询回收站失败")
		return
	}
	ok(w, map[string]any{"images": images, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}

func (a *App) restoreImage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	result, err := a.DB.Exec(`UPDATE images SET is_deleted=0,updated_at=?,deleted_at='',deleted_by='' WHERE id=? AND is_deleted=1 AND is_nsfw=0`, now(), r.PathValue("id"))
	if err != nil {
		fail(w, 500, "恢复图片失败")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fail(w, 404, "回收站中没有该图片")
		return
	}
	ok(w, map[string]bool{"restored": true})
}

func (a *App) nsfwImages(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	page, limit := pageParams(r)
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM images WHERE is_nsfw=1`).Scan(&total); err != nil {
		fail(w, 500, "查询违规图片失败")
		return
	}
	rows, err := a.DB.Query(`SELECT `+imageColumns+` FROM images WHERE is_nsfw=1 ORDER BY uploaded_at DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		fail(w, 500, "查询违规图片失败")
		return
	}
	defer rows.Close()
	images := make([]Image, 0, limit)
	for rows.Next() {
		im, err := scanImage(rows)
		if err != nil {
			fail(w, 500, "查询违规图片失败")
			return
		}
		im.URL = "/api/images/preview/" + im.Filename
		images = append(images, im)
	}
	if rows.Err() != nil {
		fail(w, 500, "查询违规图片失败")
		return
	}
	ok(w, map[string]any{"images": images, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}

func (a *App) previewImage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.serveStoredImage(w, r, r.PathValue("filename"), true)
}

func (a *App) unmarkNSFW(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var wasDeleted, nsfw bool
	err := a.DB.QueryRow(`SELECT is_deleted,is_nsfw FROM images WHERE id=?`, r.PathValue("id")).Scan(&wasDeleted, &nsfw)
	if err == sql.ErrNoRows {
		fail(w, 404, "图片不存在")
		return
	}
	if err != nil {
		fail(w, 500, "查询图片失败")
		return
	}
	if nsfw {
		if _, err := a.DB.Exec(`UPDATE images SET is_nsfw=0,is_deleted=0,updated_at=? WHERE id=?`, now(), r.PathValue("id")); err != nil {
			fail(w, 500, "更新图片失败")
			return
		}
	}
	ok(w, map[string]bool{"restored": nsfw && wasDeleted})
}

func (a *App) hardDeleteImages(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.purgeImages(w, "is_deleted=1")
}

func (a *App) clearNSFWImages(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.purgeImages(w, "is_nsfw=1")
}

func (a *App) purgeImages(w http.ResponseWriter, predicate string) {
	count := 0
	errors := make([]string, 0)
	errorCount := 0
	addError := func(uuid string) {
		errorCount++
		if len(errors) < 100 {
			errors = append(errors, uuid)
		}
	}
	lastID := ""
	for {
		rows, err := a.DB.Query(`SELECT id,uuid,filename FROM images WHERE `+predicate+` AND id>? ORDER BY id LIMIT 100`, lastID)
		if err != nil {
			fail(w, 500, "清理图片失败")
			return
		}
		type target struct{ id, uuid, filename string }
		batch := make([]target, 0, 100)
		for rows.Next() {
			var item target
			if err := rows.Scan(&item.id, &item.uuid, &item.filename); err != nil {
				rows.Close()
				fail(w, 500, "清理图片失败")
				return
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			fail(w, 500, "清理图片失败")
			return
		}
		if len(batch) == 0 {
			break
		}
		for _, item := range batch {
			lastID = item.id
			if !safeFilename.MatchString(item.filename) || !strings.HasPrefix(item.filename, item.uuid+".") {
				addError(item.uuid)
				continue
			}
			if err := os.Remove(filepath.Join(a.DataDir, "uploads", item.filename)); err != nil && !os.IsNotExist(err) {
				addError(item.uuid)
				continue
			}
			if _, err := a.DB.Exec(`DELETE FROM images WHERE id=?`, item.id); err != nil {
				addError(item.uuid)
				continue
			}
			count++
		}
	}
	ok(w, map[string]any{"deletedCount": count, "errors": errors, "errorCount": errorCount})
}
