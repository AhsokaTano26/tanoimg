package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
)

var uploadKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
var errUploadKeyBusy = errors.New("upload with this idempotency key is still running")

func ensureIdempotencySchema(a *App) error {
	_, err := a.DB.Exec(`CREATE TABLE IF NOT EXISTS upload_idempotency (principal TEXT NOT NULL, key TEXT NOT NULL, status TEXT NOT NULL, image_id TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(principal,key))`)
	return err
}

func (a *App) uploadPrincipal(r *http.Request, public bool) string {
	if userID := a.userID(r); userID != "" {
		return "user:" + userID
	}
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = r.URL.Query().Get("apiKey")
	}
	if key != "" {
		if _, ok := a.resolveAPIKey(r, ""); ok {
		return "key:" + tokenHash(key)
		}
	}
	if public {
		return "ip:" + a.clientIP(r)
	}
	return ""
}

// claimUploadKey prevents concurrent retries from creating multiple files.
// A completed claim returns the original image; callers must return it before
// applying upload rate limits or reading the request body.
func (a *App) claimUploadKey(r *http.Request, public bool) (string, *Image, error) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return "", nil, nil
	}
	if !uploadKeyPattern.MatchString(key) {
		return "", nil, errors.New("invalid Idempotency-Key")
	}
	principal := a.uploadPrincipal(r, public)
	if principal == "" {
		return "", nil, errors.New("upload credentials required")
	}
	stamp := time.Now().Unix()
	result, err := a.DB.ExecContext(r.Context(), `INSERT OR IGNORE INTO upload_idempotency(principal,key,status,created_at,updated_at) VALUES(?,?,'pending',?,?)`, principal, key, stamp, stamp)
	if err != nil {
		return "", nil, err
	}
	n, _ := result.RowsAffected()
	if n == 1 {
		return key, nil, nil
	}
	var status, imageID string
	var updated int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT status,image_id,updated_at FROM upload_idempotency WHERE principal=? AND key=?`, principal, key).Scan(&status, &imageID, &updated); err != nil {
		return "", nil, err
	}
	if status == "complete" {
		im, err := a.imageByID(imageID)
		if err == sql.ErrNoRows {
			return "", nil, errors.New("original upload was removed")
		}
		if err != nil {
			return "", nil, err
		}
		if im.IsDeleted {
			return "", nil, errors.New("original upload was removed")
		}
		return "", &im, nil
	}
	if imageID != "" {
		if im, lookupErr := a.imageByID(imageID); lookupErr == nil {
			_, _ = a.DB.Exec(`UPDATE upload_idempotency SET status='complete',updated_at=? WHERE principal=? AND key=? AND status='pending'`, stamp, principal, key)
			return "", &im, nil
		} else if lookupErr != sql.ErrNoRows {
			return "", nil, lookupErr
		}
	}
	if updated <= stamp-600 {
		result, err := a.DB.ExecContext(r.Context(), `UPDATE upload_idempotency SET updated_at=? WHERE principal=? AND key=? AND status='pending' AND updated_at=?`, stamp, principal, key, updated)
		if err != nil {
			return "", nil, err
		}
		n, _ = result.RowsAffected()
		if n == 1 {
			return key, nil, nil
		}
	}
	return "", nil, errUploadKeyBusy
}

// bindUploadKey records the planned image ID before saving it. If the process
// stops after the image commits, a retry can reconcile the pending claim.
func (a *App) bindUploadKey(r *http.Request, public bool, key, imageID string) error {
	if key == "" {
		return nil
	}
	principal := a.uploadPrincipal(r, public)
	result, err := a.DB.Exec(`UPDATE upload_idempotency SET image_id=?,updated_at=? WHERE principal=? AND key=? AND status='pending'`, imageID, time.Now().Unix(), principal, key)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("upload idempotency claim changed")
	}
	return nil
}

func (a *App) finishUploadKey(r *http.Request, public bool, key, imageID string) error {
	if key == "" {
		return nil
	}
	principal := a.uploadPrincipal(r, public)
	if principal == "" {
		return errors.New("upload credentials missing at completion")
	}
	if imageID == "" {
		_, err := a.DB.Exec(`DELETE FROM upload_idempotency WHERE principal=? AND key=? AND status='pending'`, principal, key)
		return err
	}
	result, err := a.DB.Exec(`UPDATE upload_idempotency SET status='complete',image_id=?,updated_at=? WHERE principal=? AND key=? AND status='pending'`, imageID, time.Now().Unix(), principal, key)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("upload idempotency claim changed")
	}
	return nil
}

func (a *App) registerIdempotencyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/uploads/reconcile", a.reconcileUploads)
}

func (a *App) reconcileUploads(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Keys []string `json:"keys"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || len(body.Keys) == 0 || len(body.Keys) > 1000 {
		fail(w, 400, "请提供 1–1000 个幂等键")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "请求内容无效")
		return
	}
	principal := a.uploadPrincipal(r, true)
	if principal == "" {
		fail(w, 401, "无法识别上传来源")
		return
	}
	type result struct {
		Key     string `json:"key"`
		Status  string `json:"status"`
		ImageID string `json:"imageId,omitempty"`
		URL     string `json:"url,omitempty"`
	}
	results := make([]result, 0, len(body.Keys))
	for _, key := range body.Keys {
		if !uploadKeyPattern.MatchString(key) {
			fail(w, 400, "幂等键无效")
			return
		}
		item := result{Key: key, Status: "unknown"}
		var imageID string
		err := a.DB.QueryRowContext(r.Context(), `SELECT status,image_id FROM upload_idempotency WHERE principal=? AND key=?`, principal, key).Scan(&item.Status, &imageID)
		if err != nil && err != sql.ErrNoRows {
			fail(w, 500, "查询上传结果失败")
			return
		}
		if err == nil && (item.Status == "complete" || item.Status == "pending" && imageID != "") {
			im, lookupErr := a.imageByID(imageID)
			if lookupErr == nil && !im.IsDeleted {
				item.Status = "complete"
				item.ImageID = im.ID
				if im.Visibility != "private" || a.userID(r) != "" {
					item.URL = im.URL
				}
			} else {
				item.Status = "removed"
			}
		}
		results = append(results, item)
	}
	ok(w, map[string]any{"results": results})
}
