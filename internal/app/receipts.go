package app

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

func ensureReceiptSchema(a *App) error {
	_, err := a.DB.Exec(`CREATE TABLE IF NOT EXISTS upload_receipts (image_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL)`)
	return err
}

// issueUploadReceipt returns the secret once; only its hash is stored.
func (a *App) issueUploadReceipt(imageID string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	_, err := a.DB.Exec(`INSERT INTO upload_receipts(image_id,token_hash,created_at) VALUES(?,?,?)`, imageID, tokenHash(token), now())
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *App) registerReceiptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /api/images/{id}/self", a.deleteWithUploadReceipt)
}

func (a *App) deleteWithUploadReceipt(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.Header.Get("X-Upload-Receipt"))
	if len(token) < 32 || len(token) > 128 {
		fail(w, 404, "上传凭据无效")
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "删除失败")
		return
	}
	defer tx.Rollback()
	var stored string
	if err := tx.QueryRow(`SELECT token_hash FROM upload_receipts WHERE image_id=?`, r.PathValue("id")).Scan(&stored); err != nil || stored != tokenHash(token) {
		fail(w, 404, "上传凭据无效")
		return
	}
	stamp := now()
	result, err := tx.Exec(`UPDATE images SET is_deleted=1,deleted_at=?,deleted_by='receipt',updated_at=? WHERE id=? AND is_deleted=0`, stamp, stamp, r.PathValue("id"))
	if err != nil {
		fail(w, 500, "删除失败")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		fail(w, 404, "图片不存在或已删除")
		return
	}
	if _, err := tx.Exec(`DELETE FROM upload_receipts WHERE image_id=?`, r.PathValue("id")); err != nil {
		fail(w, 500, "删除失败")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "删除失败")
		return
	}
	ok(w, map[string]bool{"deleted": true})
}
