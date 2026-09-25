package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

var errDailyQuotaExceeded = errors.New("daily upload quota exceeded")

type uploadIPDailyQuota struct {
	DailyCount int64 `json:"dailyCount"`
	DailyBytes int64 `json:"dailyBytes"`
}

func (a *App) ipDailyQuota() uploadIPDailyQuota {
	var quota uploadIPDailyQuota
	if json.Unmarshal(a.setting("uploadIPDailyQuota", quota), &quota) != nil || !validKeyQuota(quota.DailyCount, quota.DailyBytes) {
		return uploadIPDailyQuota{}
	}
	return quota
}

func (a *App) getIPDailyQuota(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, a.ipDailyQuota())
}

func (a *App) putIPDailyQuota(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var quota uploadIPDailyQuota
	if !decodeKeyBody(w, r, &quota) {
		return
	}
	if !validKeyQuota(quota.DailyCount, quota.DailyBytes) {
		fail(w, 400, "每日 IP 配额无效")
		return
	}
	value, _ := json.Marshal(quota)
	if err := a.setSetting("uploadIPDailyQuota", value); err != nil {
		fail(w, 500, "无法保存每日 IP 配额")
		return
	}
	ok(w, quota)
}

func reserveDailyUsage(ctx context.Context, tx *sql.Tx, subjectType, subjectID, day string, bytes, countLimit, byteLimit int64) error {
	if byteLimit > 0 && bytes > byteLimit {
		return errDailyQuotaExceeded
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO upload_daily_usage(subject_type,subject_id,day,upload_count,upload_bytes) VALUES(?,?,?,1,?)
		ON CONFLICT(subject_type,subject_id,day) DO UPDATE SET
		upload_count=upload_daily_usage.upload_count+1,
		upload_bytes=upload_daily_usage.upload_bytes+excluded.upload_bytes
		WHERE (?=0 OR upload_daily_usage.upload_count+1<=?) AND (?=0 OR upload_daily_usage.upload_bytes+excluded.upload_bytes<=?)`, subjectType, subjectID, day, bytes, countLimit, countLimit, byteLimit, byteLimit)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errDailyQuotaExceeded
	}
	return nil
}

// Reserve after the final stored byte count and image ID are known, but before
// publishing the file. Both key and IP limits are claimed in one transaction.
// A failed upload must call finishUploadQuota(..., false).
func (a *App) reserveUploadQuota(ctx context.Context, principal apiKeyPrincipal, ip, imageID string, bytes int64) (string, error) {
	if imageID == "" || bytes < 0 || !validKeyQuota(principal.DailyCount, principal.DailyBytes) {
		return "", errors.New("invalid upload quota reservation")
	}
	if ip == "" {
		ip = "unknown"
	}
	ipHash := tokenHash(ip)
	ipQuota := a.ipDailyQuota() // Read before BeginTx: App has one SQLite connection.
	day := time.Now().UTC().Format("2006-01-02")
	id, err := newID()
	if err != nil {
		return "", err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if principal.ID != "" {
		if err = reserveDailyUsage(ctx, tx, "key", principal.ID, day, bytes, principal.DailyCount, principal.DailyBytes); err != nil {
			return "", err
		}
	}
	if err = reserveDailyUsage(ctx, tx, "ip", ipHash, day, bytes, ipQuota.DailyCount, ipQuota.DailyBytes); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO upload_quota_reservations(id,image_id,key_id,ip_hash,day,upload_bytes,created_at) VALUES(?,?,?,?,?,?,?)`, id, imageID, principal.ID, ipHash, day, bytes, time.Now().Unix()); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func releaseDailyUsage(ctx context.Context, tx *sql.Tx, subjectType, subjectID, day string, bytes int64) error {
	result, err := tx.ExecContext(ctx, `UPDATE upload_daily_usage SET upload_count=upload_count-1,upload_bytes=upload_bytes-? WHERE subject_type=? AND subject_id=? AND day=? AND upload_count>=1 AND upload_bytes>=?`, bytes, subjectType, subjectID, day, bytes)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("upload quota counter is inconsistent")
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM upload_daily_usage WHERE subject_type=? AND subject_id=? AND day=? AND upload_count=0`, subjectType, subjectID, day)
	return err
}

// Calling finish twice is safe. A committed reservation keeps its daily usage;
// a failed reservation releases both counters atomically.
func (a *App) finishUploadQuota(ctx context.Context, reservationID string, committed bool) error {
	if reservationID == "" {
		return nil
	}
	if ctx == nil || ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var keyID, ipHash, day string
	var bytes int64
	err = tx.QueryRowContext(ctx, `SELECT key_id,ip_hash,day,upload_bytes FROM upload_quota_reservations WHERE id=?`, reservationID).Scan(&keyID, &ipHash, &day, &bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !committed {
		if keyID != "" {
			if err = releaseDailyUsage(ctx, tx, "key", keyID, day, bytes); err != nil {
				return err
			}
		}
		if err = releaseDailyUsage(ctx, tx, "ip", ipHash, day, bytes); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM upload_quota_reservations WHERE id=?`, reservationID); err != nil {
		return err
	}
	return tx.Commit()
}

// On startup, reconcile reservations abandoned for over an hour. If an image
// record exists, the upload completed; otherwise release the claim. This does
// not touch image IDs, paths, or files.
func (a *App) recoverStaleUploadQuotaReservations() error {
	rows, err := a.DB.Query(`SELECT id,image_id FROM upload_quota_reservations WHERE created_at<? LIMIT 1000`, time.Now().Add(-time.Hour).Unix())
	if err != nil {
		return err
	}
	type abandoned struct{ reservationID, imageID string }
	var stale []abandoned
	for rows.Next() {
		var item abandoned
		if err := rows.Scan(&item.reservationID, &item.imageID); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range stale {
		var exists int
		err := a.DB.QueryRow(`SELECT 1 FROM images WHERE id=?`, item.imageID).Scan(&exists)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := a.finishUploadQuota(context.Background(), item.reservationID, exists == 1); err != nil {
			return err
		}
	}
	return nil
}
