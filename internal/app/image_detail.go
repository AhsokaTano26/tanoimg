package app

import (
	"database/sql"
	"net/http"
)

func (a *App) imageDetail(w http.ResponseWriter, r *http.Request) {
	im, err := a.imageByID(r.PathValue("id"))
	if err == sql.ErrNoRows || err == nil && (im.IsDeleted || im.IsNsfw || im.Visibility != "public" && a.userID(r) == "") {
		fail(w, 404, "图片不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取图片失败")
		return
	}
	if a.userID(r) != "" {
		ok(w, im)
		return
	}
	ok(w, map[string]any{"id": im.ID, "uuid": im.UUID, "filename": im.Filename, "originalName": im.OriginalName, "format": im.Format, "size": im.Size, "width": im.Width, "height": im.Height, "url": im.URL, "uploadedBy": im.UploadedBy, "uploadedAt": im.UploadedAt, "visibility": im.Visibility, "alt": im.Alt, "author": im.Author, "license": im.License, "tags": im.Tags})
}
