package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

type imageLifecycleSettings struct {
	RetainVersions    int  `json:"retainVersions"`
	StripJPEGMetadata bool `json:"stripJPEGMetadata"`
}

type imageVersion struct {
	ID           string `json:"id"`
	Revision     int64  `json:"revision"`
	Size         int64  `json:"size"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	OriginalName string `json:"originalName"`
	MD5          string `json:"md5"`
	CreatedAt    string `json:"createdAt"`
}

func ensureMediaLifecycleSchema(a *App) error {
	for _, dir := range []string{"versions", "thumbnails"} {
		if err := os.MkdirAll(filepath.Join(a.DataDir, dir), 0700); err != nil {
			return err
		}
	}
	_, err := a.DB.Exec(`CREATE TABLE IF NOT EXISTS image_versions (id TEXT PRIMARY KEY,image_id TEXT NOT NULL,revision INTEGER NOT NULL,size INTEGER NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,original_name TEXT NOT NULL,md5 TEXT NOT NULL,created_at TEXT NOT NULL)`)
	if err != nil {
		return err
	}
	_, err = a.DB.Exec(`CREATE INDEX IF NOT EXISTS image_versions_image ON image_versions(image_id,created_at DESC,id DESC)`)
	return err
}

func (a *App) lifecycleSettings() imageLifecycleSettings {
	c := imageLifecycleSettings{RetainVersions: 3}
	json.Unmarshal(a.setting("imageLifecycle", c), &c)
	if c.RetainVersions < 0 || c.RetainVersions > 10 {
		c.RetainVersions = 3
	}
	return c
}

func (a *App) registerMediaLifecycleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /t/{filename}", a.thumbnailFile)
	mux.HandleFunc("GET /api/settings/image-lifecycle", a.getLifecycleSettings)
	mux.HandleFunc("PUT /api/settings/image-lifecycle", a.putLifecycleSettings)
	mux.HandleFunc("POST /api/admin/images/{id}/replace", a.replaceImage)
	mux.HandleFunc("GET /api/admin/images/{id}/versions", a.imageVersions)
	mux.HandleFunc("POST /api/admin/images/{id}/rollback/{version}", a.rollbackImage)
}

func (a *App) getLifecycleSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	ok(w, a.lifecycleSettings())
}

func (a *App) putLifecycleSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var input struct {
		RetainVersions    *int  `json:"retainVersions"`
		StripJPEGMetadata *bool `json:"stripJPEGMetadata"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || (input.RetainVersions == nil && input.StripJPEGMetadata == nil) {
		fail(w, 400, "无效图片生命周期设置")
		return
	}
	c := a.lifecycleSettings()
	if input.RetainVersions != nil {
		c.RetainVersions = *input.RetainVersions
	}
	if input.StripJPEGMetadata != nil {
		c.StripJPEGMetadata = *input.StripJPEGMetadata
	}
	if c.RetainVersions < 0 || c.RetainVersions > 10 {
		fail(w, 400, "历史版本保留数须为 0–10")
		return
	}
	data, _ := json.Marshal(c)
	if err := a.setSetting("imageLifecycle", data); err != nil {
		fail(w, 500, "保存设置失败")
		return
	}
	ok(w, c)
}

func (a *App) versionPath(uuid, id string) string {
	return filepath.Join(a.DataDir, "versions", uuid, id+".bin")
}

func (a *App) replaceImage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	im, err := a.imageByID(r.PathValue("id"))
	if err == sql.ErrNoRows {
		fail(w, 404, "图片不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取图片失败")
		return
	}
	if im.IsDeleted || im.IsNsfw {
		fail(w, 409, "无法替换回收站或违规图片")
		return
	}
	f, name, size, _, err := multipartFile(w, r, privateLimit(a), filepath.Join(a.DataDir, "uploads"))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	defer func(uploaded *os.File) { uploaded.Close(); os.Remove(uploaded.Name()) }(f)
	var head [512]byte
	n, _ := f.Read(head[:])
	format := detectFormat(head[:n])
	if format == "" {
		fail(w, 400, "不支持的图片内容格式")
		return
	}
	format, err = inspectImageFormat(f, format)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if format != im.Format {
		fail(w, 400, "替换图片必须与原图片格式一致，以保持原直链")
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		fail(w, 500, "读取替换图片失败")
		return
	}
	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	processed, processedSize, err := a.maybeStripJPEGMetadata(f, format, a.lifecycleSettings().StripJPEGMetadata)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if processed != f {
		defer func() { processed.Close(); os.Remove(processed.Name()) }()
		f = processed
		size = processedSize
	}
	digest, err := fileMD5(f)
	if err != nil {
		fail(w, 500, "计算图片摘要失败")
		return
	}
	if err := a.replaceStoredImage(im, f, name, size, width, height, digest); err != nil {
		fail(w, 500, "替换图片失败: "+err.Error())
		return
	}
	updated, err := a.imageByID(im.ID)
	if err != nil {
		fail(w, 500, "读取图片失败")
		return
	}
	ok(w, updated)
}

func (a *App) replaceStoredImage(im Image, source *os.File, originalName string, size int64, width, height int, digest string) error {
	if !safeFilename.MatchString(im.Filename) {
		return errors.New("invalid stored filename")
	}
	oldPath := filepath.Join(a.DataDir, "uploads", im.Filename)
	versionID, err := newID()
	if err != nil {
		return err
	}
	dir := filepath.Join(a.DataDir, "versions", im.UUID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	backup := a.versionPath(im.UUID, versionID)
	if err := os.Link(oldPath, backup); err != nil {
		if err := copyIfMissing(oldPath, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(source.Name(), oldPath); err != nil {
		os.Remove(backup)
		return err
	}
	restore := func() error { return os.Rename(backup, oldPath) }
	retain := a.lifecycleSettings().RetainVersions
	tx, err := a.DB.Begin()
	if err != nil {
		restore()
		return err
	}
	defer tx.Rollback()
	if retain > 0 {
		_, err = tx.Exec(`INSERT INTO image_versions(id,image_id,revision,size,width,height,original_name,md5,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, versionID, im.ID, im.Revision, im.Size, im.Width, im.Height, im.OriginalName, im.MD5, now())
		if err != nil {
			restore()
			return err
		}
	}
	result, err := tx.Exec(`UPDATE images SET original_name=?,size=?,width=?,height=?,md5=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND is_deleted=0`, originalName, size, width, height, digest, now(), im.ID, im.Revision)
	if err != nil {
		restore()
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		restore()
		return errors.New("image changed during replacement")
	}
	if err := tx.Commit(); err != nil {
		restore()
		return err
	}
	if retain == 0 {
		os.Remove(backup)
	} else if err := a.pruneImageVersions(im, retain); err != nil {
		log.Printf("prune image versions %s: %v", im.ID, err)
	}
	os.Remove(a.thumbnailPath(im))
	return nil
}

func (a *App) pruneImageVersions(im Image, retain int) error {
	rows, err := a.DB.Query(`SELECT id FROM image_versions WHERE image_id=? ORDER BY created_at DESC,id DESC LIMIT -1 OFFSET ?`, im.ID, retain)
	if err != nil {
		return err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := a.DB.Exec(`DELETE FROM image_versions WHERE id=? AND image_id=?`, id, im.ID); err != nil {
			return err
		}
		if err := os.Remove(a.versionPath(im.UUID, id)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (a *App) cleanupMediaArtifacts(imageID, uuid string, revision int64) error {
	return a.cleanupMediaArtifactsWith(a.DB, imageID, uuid, revision)
}

type mediaExecer interface {
	Exec(string, ...any) (sql.Result, error)
}

func (a *App) cleanupMediaArtifactsWith(db mediaExecer, imageID, uuid string, revision int64) error {
	if !safeFilename.MatchString(uuid + ".png") {
		return errors.New("invalid image uuid")
	}
	if _, err := db.Exec(`DELETE FROM image_versions WHERE image_id=?`, imageID); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(a.DataDir, "versions", uuid)); err != nil {
		return err
	}
	thumb := a.thumbnailPath(Image{UUID: uuid, Revision: revision})
	if err := os.Remove(thumb); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *App) imageVersions(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
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
	rows, err := a.DB.Query(`SELECT id,revision,size,width,height,original_name,md5,created_at FROM image_versions WHERE image_id=? ORDER BY created_at DESC,id DESC`, im.ID)
	if err != nil {
		fail(w, 500, "读取历史版本失败")
		return
	}
	defer rows.Close()
	versions := make([]imageVersion, 0)
	for rows.Next() {
		var v imageVersion
		if err := rows.Scan(&v.ID, &v.Revision, &v.Size, &v.Width, &v.Height, &v.OriginalName, &v.MD5, &v.CreatedAt); err != nil {
			fail(w, 500, "读取历史版本失败")
			return
		}
		versions = append(versions, v)
	}
	if rows.Err() != nil {
		fail(w, 500, "读取历史版本失败")
		return
	}
	ok(w, map[string]any{"versions": versions})
}

func (a *App) rollbackImage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	im, err := a.imageByID(r.PathValue("id"))
	if err == sql.ErrNoRows {
		fail(w, 404, "图片不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取图片失败")
		return
	}
	if im.IsDeleted || im.IsNsfw {
		fail(w, 409, "无法恢复回收站或违规图片的历史版本")
		return
	}
	var v imageVersion
	err = a.DB.QueryRow(`SELECT id,revision,size,width,height,original_name,md5,created_at FROM image_versions WHERE image_id=? AND id=?`, im.ID, r.PathValue("version")).Scan(&v.ID, &v.Revision, &v.Size, &v.Width, &v.Height, &v.OriginalName, &v.MD5, &v.CreatedAt)
	if err == sql.ErrNoRows {
		fail(w, 404, "历史版本不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取历史版本失败")
		return
	}
	previous, err := os.Open(a.versionPath(im.UUID, v.ID))
	if err != nil {
		fail(w, 404, "历史版本文件不存在")
		return
	}
	defer previous.Close()
	temp, err := os.CreateTemp(filepath.Join(a.DataDir, "uploads"), ".rollback-*")
	if err != nil {
		fail(w, 500, "创建回滚文件失败")
		return
	}
	defer func() { temp.Close(); os.Remove(temp.Name()) }()
	if _, err := io.Copy(temp, previous); err != nil {
		fail(w, 500, "复制历史版本失败")
		return
	}
	if err := temp.Sync(); err != nil {
		fail(w, 500, "保存历史版本失败")
		return
	}
	if err := a.replaceStoredImage(im, temp, v.OriginalName, v.Size, v.Width, v.Height, v.MD5); err != nil {
		fail(w, 500, fmt.Sprintf("回滚失败: %v", err))
		return
	}
	updated, err := a.imageByID(im.ID)
	if err != nil {
		fail(w, 500, "读取图片失败")
		return
	}
	ok(w, updated)
}
