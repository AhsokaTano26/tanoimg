package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultRecycleRetentionDays = 30
	maximumRecycleRetentionDays = 3650
	retentionScanBatch          = 200
	retentionDeleteBatch        = 100
)

func (a *App) registerRetentionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/retention", a.getRetention)
	mux.HandleFunc("PUT /api/admin/retention", a.putRetention)
}

func (a *App) recycleRetentionDays() int {
	days := defaultRecycleRetentionDays
	if json.Unmarshal(a.setting("recycleRetentionDays", days), &days) != nil || days < 0 || days > maximumRecycleRetentionDays {
		return defaultRecycleRetentionDays
	}
	return days
}

func (a *App) getRetention(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, map[string]int{"days": a.recycleRetentionDays()})
}

func (a *App) putRetention(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var body struct {
		Days *int `json:"days"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Days == nil || *body.Days < 0 || *body.Days > maximumRecycleRetentionDays {
		fail(w, 400, "保留天数须为 0–3650；0 表示关闭自动清理")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		fail(w, 400, "请求内容无效")
		return
	}
	value, _ := json.Marshal(*body.Days)
	if err := a.setSetting("recycleRetentionDays", value); err != nil {
		fail(w, 500, "无法保存保留天数")
		return
	}
	ok(w, map[string]int{"days": *body.Days})
}

// StartRetention runs only on serving instances. The caller must cancel ctx and
// wait for the returned channel before closing the database.
func (a *App) StartRetention(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		cursor := ""
		pause := time.Duration(0)
		for {
			if pause > 0 {
				timer := time.NewTimer(pause)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			if err := ctx.Err(); err != nil {
				return
			}
			days := a.recycleRetentionDays()
			if days == 0 {
				cursor = ""
				pause = 5 * time.Minute
				continue
			}
			cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
			result, err := a.cleanupRetentionBatch(ctx, cutoff, cursor)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("recycle retention cleanup failed: %v", err)
				pause = time.Minute
				continue
			}
			cursor = result.NextCursor
			if result.Done {
				cursor = ""
				pause = time.Hour
			} else {
				pause = 100 * time.Millisecond
			}
		}
	}()
	return done
}

type retentionBatchResult struct {
	Scanned    int
	Deleted    int
	Skipped    int
	NextCursor string
	Done       bool
}

type retentionImage struct {
	id        string
	uuid      string
	filename  string
	deletedAt string
}

func (a *App) cleanupRetentionBatch(ctx context.Context, cutoff time.Time, cursor string) (retentionBatchResult, error) {
	result := retentionBatchResult{NextCursor: cursor}
	rows, err := a.DB.QueryContext(ctx, `SELECT id,uuid,filename,deleted_at FROM images WHERE is_deleted=1 AND deleted_at!='' AND id>? ORDER BY id LIMIT ?`, cursor, retentionScanBatch+1)
	if err != nil {
		return result, err
	}
	items := make([]retentionImage, 0, retentionScanBatch+1)
	for rows.Next() {
		var item retentionImage
		if err := rows.Scan(&item.id, &item.uuid, &item.filename, &item.deletedAt); err != nil {
			rows.Close()
			return result, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.Done = len(items) <= retentionScanBatch
	if !result.Done {
		items = items[:retentionScanBatch]
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Scanned++
		result.NextCursor = item.id
		deletedAt, err := time.Parse(time.RFC3339Nano, item.deletedAt)
		if err != nil || !deletedAt.Before(cutoff) {
			result.Skipped++
			continue
		}
		removed, err := a.removeExpiredImage(ctx, item)
		if err != nil {
			return result, err
		}
		if removed {
			result.Deleted++
		} else {
			result.Skipped++
		}
		if result.Deleted >= retentionDeleteBatch {
			result.Done = false
			break
		}
	}
	return result, nil
}

func (a *App) removeExpiredImage(ctx context.Context, item retentionImage) (bool, error) {
	if !safeFilename.MatchString(item.filename) || !strings.HasPrefix(item.filename, item.uuid+".") {
		return false, nil
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT deleted_at FROM images WHERE id=? AND uuid=? AND filename=? AND is_deleted=1`, item.id, item.uuid, item.filename).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) || err == nil && current != item.deletedAt {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var references int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM images WHERE filename=? AND id!=?`, item.filename, item.id).Scan(&references); err != nil {
		return false, err
	}
	if references != 0 {
		return false, nil
	}
	// Keep artifact removal in one place so thumbnail files can be included later.
	path := filepath.Join(a.DataDir, "uploads", item.filename)
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil {
		if err := os.Remove(path); err != nil {
			return false, err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM images WHERE id=? AND is_deleted=1 AND deleted_at=?`, item.id, item.deletedAt)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return false, errors.New("retention record changed during cleanup")
	}
	return true, tx.Commit()
}
