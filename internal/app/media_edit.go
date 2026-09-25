package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

func (a *App) imageByID(id string) (Image, error) {
	rows, err := a.DB.Query(`SELECT `+imageColumns+` FROM images WHERE id=?`, id)
	if err != nil {
		return Image{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Image{}, sql.ErrNoRows
	}
	return scanImage(rows)
}

func validVisibility(value string) bool {
	return value == "public" || value == "unlisted" || value == "private"
}

func (a *App) updateImageMetadata(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	patch, valid := decodePatch(w, r)
	if !valid {
		return
	}
	im, err := a.imageByID(r.PathValue("id"))
	if err == sql.ErrNoRows {
		fail(w, 404, "图片不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取图片失败")
		return
	}
	for key, raw := range patch {
		switch key {
		case "visibility":
			if err := json.Unmarshal(raw, &im.Visibility); err != nil || !validVisibility(im.Visibility) {
				fail(w, 400, "无效可见性")
				return
			}
		case "alt":
			if err := json.Unmarshal(raw, &im.Alt); err != nil {
				fail(w, 400, "图片描述无效")
				return
			}
		case "author":
			if err := json.Unmarshal(raw, &im.Author); err != nil {
				fail(w, 400, "作者无效")
				return
			}
		case "license":
			if err := json.Unmarshal(raw, &im.License); err != nil {
				fail(w, 400, "许可信息无效")
				return
			}
		case "tags":
			if err := json.Unmarshal(raw, &im.Tags); err != nil || im.Tags == nil {
				fail(w, 400, "标签无效")
				return
			}
		default:
			fail(w, 400, "未知图片属性")
			return
		}
	}
	if err := applyUploadMetadata(&im, im.Alt, im.Author, im.License, im.Tags); err != nil {
		fail(w, 400, err.Error())
		return
	}
	tags, _ := json.Marshal(im.Tags)
	_, err = a.DB.Exec(`UPDATE images SET visibility=?,alt=?,author=?,license=?,tags_json=?,updated_at=? WHERE id=?`, im.Visibility, im.Alt, im.Author, im.License, string(tags), now(), im.ID)
	if err != nil {
		fail(w, 500, "更新图片失败")
		return
	}
	im, err = a.imageByID(im.ID)
	if err != nil {
		fail(w, 500, "读取图片失败")
		return
	}
	ok(w, im)
}

func (a *App) batchUpdateImageVisibility(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		IDs        []string `json:"ids"`
		Visibility string   `json:"visibility"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || len(body.IDs) == 0 || len(body.IDs) > 1000 || !validVisibility(body.Visibility) {
		fail(w, 400, "请选择 1–1000 张图片和有效可见性")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "批量更新失败")
		return
	}
	defer tx.Rollback()
	seen := make(map[string]bool, len(body.IDs))
	count := int64(0)
	stamp := now()
	for _, id := range body.IDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result, err := tx.Exec(`UPDATE images SET visibility=?,updated_at=? WHERE id=? AND is_deleted=0 AND visibility<>?`, body.Visibility, stamp, id, body.Visibility)
		if err != nil {
			fail(w, 500, "批量更新失败")
			return
		}
		n, _ := result.RowsAffected()
		count += n
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "批量更新失败")
		return
	}
	ok(w, map[string]int64{"updatedCount": count})
}
