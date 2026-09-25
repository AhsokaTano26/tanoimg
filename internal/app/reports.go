package app

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func ensureReportSchema(a *App) error {
	for _, query := range []string{
		`CREATE TABLE IF NOT EXISTS image_reports (id INTEGER PRIMARY KEY AUTOINCREMENT, image_id TEXT NOT NULL, reason TEXT NOT NULL, details TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL, report_day TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'open', created_at TEXT NOT NULL, resolved_at TEXT NOT NULL DEFAULT '')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS image_reports_daily ON image_reports(image_id,ip,report_day)`,
		`CREATE INDEX IF NOT EXISTS image_reports_queue ON image_reports(status,created_at DESC,id DESC)`,
	} {
		if _, err := a.DB.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) registerReportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/images/{id}/report", a.reportImage)
	mux.HandleFunc("GET /api/admin/reports", a.listImageReports)
	mux.HandleFunc("PUT /api/admin/reports/{id}", a.updateImageReport)
}

func (a *App) reportImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason  string `json:"reason"`
		Details string `json:"details"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		fail(w, 400, "举报内容无效")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "举报内容无效")
		return
	}
	switch body.Reason {
	case "copyright", "privacy", "abuse", "spam", "other":
	default:
		fail(w, 400, "请选择举报原因")
		return
	}
	body.Details = strings.TrimSpace(body.Details)
	if len([]rune(body.Details)) > 1000 {
		fail(w, 400, "举报说明过长")
		return
	}
	var found int
	if err := a.DB.QueryRow(`SELECT 1 FROM images WHERE id=? AND visibility='public' AND is_deleted=0 AND is_nsfw=0`, r.PathValue("id")).Scan(&found); err != nil {
		if err == sql.ErrNoRows {
			fail(w, 404, "图片不存在")
			return
		}
		fail(w, 500, "查询图片失败")
		return
	}
	stamp := now()
	day := time.Now().UTC().Format("2006-01-02")
	result, err := a.DB.Exec(`INSERT OR IGNORE INTO image_reports(image_id,reason,details,ip,report_day,created_at) VALUES(?,?,?,?,?,?)`, r.PathValue("id"), body.Reason, body.Details, a.clientIP(r), day, stamp)
	if err != nil {
		fail(w, 500, "提交举报失败")
		return
	}
	n, _ := result.RowsAffected()
	ok(w, map[string]bool{"received": true, "new": n > 0})
}

func (a *App) listImageReports(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	page, limit := pageParams(r)
	where := "1=1"
	args := []any{}
	if status := r.URL.Query().Get("status"); status != "" {
		if status != "open" && status != "resolved" && status != "dismissed" {
			fail(w, 400, "状态无效")
			return
		}
		where = "status=?"
		args = append(args, status)
	}
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM image_reports WHERE `+where, args...).Scan(&total); err != nil {
		fail(w, 500, "读取举报失败")
		return
	}
	queryArgs := append(args, limit, (page-1)*limit)
	rows, err := a.DB.Query(`SELECT id,image_id,reason,details,ip,status,created_at,resolved_at FROM image_reports WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		fail(w, 500, "读取举报失败")
		return
	}
	defer rows.Close()
	type item struct {
		ID         int64  `json:"id"`
		ImageID    string `json:"imageId"`
		Reason     string `json:"reason"`
		Details    string `json:"details"`
		IP         string `json:"ip"`
		Status     string `json:"status"`
		CreatedAt  string `json:"createdAt"`
		ResolvedAt string `json:"resolvedAt"`
	}
	items := make([]item, 0, limit)
	for rows.Next() {
		var x item
		if err := rows.Scan(&x.ID, &x.ImageID, &x.Reason, &x.Details, &x.IP, &x.Status, &x.CreatedAt, &x.ResolvedAt); err != nil {
			fail(w, 500, "读取举报失败")
			return
		}
		items = append(items, x)
	}
	if rows.Err() != nil {
		fail(w, 500, "读取举报失败")
		return
	}
	ok(w, map[string]any{"reports": items, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}

func (a *App) updateImageReport(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || (body.Status != "resolved" && body.Status != "dismissed" && body.Status != "open") {
		fail(w, 400, "状态无效")
		return
	}
	resolved := now()
	if body.Status == "open" {
		resolved = ""
	}
	result, err := a.DB.Exec(`UPDATE image_reports SET status=?,resolved_at=? WHERE id=?`, body.Status, resolved, r.PathValue("id"))
	if err != nil {
		fail(w, 500, "更新举报失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "举报不存在")
		return
	}
	ok(w, map[string]string{"status": body.Status})
}
