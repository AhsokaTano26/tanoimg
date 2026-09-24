package app

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
)

func (a *App) listBlacklist(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM ip_blacklist`).Scan(&total); err != nil {
		fail(w, 500, "查询黑名单失败")
		return
	}
	rows, err := a.DB.Query(`SELECT id,ip,reason,created_at FROM ip_blacklist ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		fail(w, 500, "查询黑名单失败")
		return
	}
	defer rows.Close()
	records := make([]map[string]string, 0, limit)
	for rows.Next() {
		var id, ip, reason, created string
		if err := rows.Scan(&id, &ip, &reason, &created); err != nil {
			fail(w, 500, "查询黑名单失败")
			return
		}
		records = append(records, map[string]string{"id": id, "ip": ip, "reason": reason, "createdAt": created})
	}
	ok(w, map[string]any{"records": records, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}

func (a *App) addBlacklist(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		IP     string `json:"ip"`
		Reason string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil || net.ParseIP(strings.TrimSpace(body.IP)) == nil {
		fail(w, 400, "IP 地址无效")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, 500, "添加黑名单失败")
		return
	}
	_, err = a.DB.Exec(`INSERT INTO ip_blacklist(id,ip,reason,created_at) VALUES(?,?,?,?)`, id, strings.TrimSpace(body.IP), strings.TrimSpace(body.Reason), now())
	if err != nil {
		fail(w, 409, "IP 已在黑名单中")
		return
	}
	ok(w, map[string]string{"id": id, "ip": body.IP, "reason": body.Reason})
}

func (a *App) deleteBlacklist(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "删除黑名单失败")
		return
	}
	defer tx.Rollback()
	for _, table := range []string{"public_upload_attempts", "upload_rates"} {
		if _, err = tx.Exec("DELETE FROM "+table+" WHERE ip=(SELECT ip FROM ip_blacklist WHERE id=?)", r.PathValue("id")); err != nil {
			fail(w, 500, "删除黑名单失败")
			return
		}
	}
	result, err := tx.Exec(`DELETE FROM ip_blacklist WHERE id=?`, r.PathValue("id"))
	if err != nil {
		fail(w, 500, "删除黑名单失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "记录不存在")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "删除黑名单失败")
		return
	}
	ok(w, nil)
}
