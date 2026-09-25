package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type autoBanConfig struct {
	Enabled       bool `json:"enabled"`
	WindowMinutes int  `json:"windowMinutes"`
	MaxAttempts   int  `json:"maxAttempts"`
}

// Counters live in SQLite rather than an unbounded per-IP memory map.
// A transaction makes counting, threshold detection and blacklisting atomic.
func (a *App) recordPublicAttempt(ctx context.Context, ip string, c autoBanConfig) (bool, error) {
	if !c.Enabled {
		return false, nil
	}
	if c.WindowMinutes < 1 {
		c.WindowMinutes = 10
	}
	if c.MaxAttempts < 1 {
		c.MaxAttempts = 120
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	current := time.Now().Unix()
	cutoff := current - int64(c.WindowMinutes)*60
	_, err = tx.ExecContext(ctx, `INSERT INTO public_upload_attempts(ip,window_start,count) VALUES(?,?,1)
 ON CONFLICT(ip) DO UPDATE SET
 window_start=CASE WHEN public_upload_attempts.window_start<=? THEN excluded.window_start ELSE public_upload_attempts.window_start END,
 count=CASE WHEN public_upload_attempts.window_start<=? THEN 1 ELSE public_upload_attempts.count+1 END`, ip, current, cutoff, cutoff)
	if err != nil {
		return false, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count FROM public_upload_attempts WHERE ip=?`, ip).Scan(&count); err != nil {
		return false, err
	}
	banned := count > c.MaxAttempts
	if banned {
		id, err := newID()
		if err != nil {
			return false, err
		}
		reason := fmt.Sprintf("自动封禁：%d 分钟内公共上传请求超过 %d 次", c.WindowMinutes, c.MaxAttempts)
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ip_blacklist(id,ip,reason,created_at) VALUES(?,?,?,?)`, id, ip, reason, now()); err != nil {
			return false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM public_upload_attempts WHERE window_start<?`, current-86400); err != nil {
		return false, err
	}
	return banned, tx.Commit()
}

func (a *App) beginPublicUpload(w http.ResponseWriter, r *http.Request, c uploadConfig) (func(), bool) {
	if !c.Enabled {
		fail(w, 403, "公共上传已禁用")
		return nil, false
	}
	ip := a.clientIP(r)
	var blocked int
	err := a.DB.QueryRow(`SELECT 1 FROM ip_blacklist WHERE ip=?`, ip).Scan(&blocked)
	if err == nil {
		fail(w, 403, "该 IP 已被禁止上传")
		return nil, false
	}
	if err != sql.ErrNoRows {
		fail(w, 503, "暂时无法检查上传权限")
		return nil, false
	}
	banned, err := a.recordPublicAttempt(r.Context(), ip, c.AutoBan)
	if err != nil {
		fail(w, 503, "暂时无法检查上传频率")
		return nil, false
	}
	if banned {
		fail(w, 403, "公共上传请求过多，该 IP 已自动加入黑名单")
		return nil, false
	}
	if !a.allowPublicRequest(ip, c.RateLimit) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "公开上传过于频繁，请稍后重试")
		return nil, false
	}
	if !c.AllowConcurrent {
		a.publicMu.Lock()
		if a.publicActive[ip] {
			a.publicMu.Unlock()
			fail(w, 429, "请等待上一张图片上传完成")
			return nil, false
		}
		a.publicActive[ip] = true
		a.publicMu.Unlock()
		return func() { a.publicMu.Lock(); delete(a.publicActive, ip); a.publicMu.Unlock() }, true
	}
	return func() {}, true
}

func uploadVisibility(r *http.Request) (string, bool) {
	value := r.URL.Query().Get("visibility")
	if value == "" {
		value = "unlisted"
	}
	return value, value == "private" || value == "unlisted" || value == "public"
}
func formatAllowed(formats []string, format string) bool {
	for _, ext := range formats {
		if strings.EqualFold(ext, format) || (format == "jpg" && strings.EqualFold(ext, "jpeg")) {
			return true
		}
	}
	return false
}
