package app

import (
	"archive/zip"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

// exportImages streams selected original files without building the archive in memory.
// The preflight prevents a missing image from producing a successful partial ZIP.
func (a *App) exportImages(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var ids []string
	if r.Method == http.MethodGet {
		ids = r.URL.Query()["id"]
	} else {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
			fail(w, http.StatusBadRequest, "无效导出请求")
			return
		}
		ids = body.IDs
	}
	if len(ids) == 0 || len(ids) > 1000 {
		fail(w, http.StatusBadRequest, "请选择 1–1000 张图片")
		return
	}
	type archiveFile struct {
		name string
		path string
	}
	files := make([]archiveFile, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		var name string
		var deleted, nsfw bool
		if err := a.DB.QueryRow(`SELECT filename,is_deleted,is_nsfw FROM images WHERE id=?`, id).Scan(&name, &deleted, &nsfw); err != nil || deleted || nsfw || !safeFilename.MatchString(name) {
			fail(w, http.StatusNotFound, "部分图片不存在或不可导出")
			return
		}
		path := filepath.Join(a.DataDir, "uploads", name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			fail(w, http.StatusNotFound, "部分图片文件缺失")
			return
		}
		files = append(files, archiveFile{name: name, path: path})
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="tanoimg-images.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Trailer", "X-Tanoimg-Export-Error")
	zw := zip.NewWriter(w)
	for _, item := range files {
		if r.Context().Err() != nil {
			break
		}
		f, err := os.Open(item.path)
		if err != nil {
			log.Printf("image export open %s: %v", item.name, err)
			w.Header().Set("X-Tanoimg-Export-Error", "incomplete")
			break
		}
		// Image formats are already compressed. Store avoids expensive Deflate work
		// during large exports while keeping the response fully streaming.
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: item.name, Method: zip.Store})
		if err == nil {
			_, err = io.Copy(entry, f)
		}
		f.Close()
		if err != nil {
			log.Printf("image export copy %s: %v", item.name, err)
			w.Header().Set("X-Tanoimg-Export-Error", "incomplete")
			break
		}
	}
	if err := zw.Close(); err != nil {
		log.Printf("image export close: %v", err)
	}
}
