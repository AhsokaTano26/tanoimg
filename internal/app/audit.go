package app

import (
	"net/http"
	"strings"
)

// auditResponse records status without buffering the response body. Flush is
// forwarded so streaming handlers continue to work when audit logging is on.
type auditResponse struct {
	http.ResponseWriter
	status int
}

func (w *auditResponse) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *auditResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *auditResponse) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (a *App) auditHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || !auditablePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		actor := a.userID(r)
		wrapped := &auditResponse{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		if actor == "" {
			return
		}
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		// Query parameters and request bodies may contain credentials and are never recorded.
		_, _ = a.DB.Exec(`INSERT INTO audit_events(actor_id,method,path,status,ip,created_at) VALUES(?,?,?,?,?,?)`, actor, r.Method, r.URL.Path, status, a.clientIP(r), now())
	})
}

func auditablePath(path string) bool {
	return strings.HasPrefix(path, "/api/admin/") || strings.HasPrefix(path, "/api/images/") || strings.HasPrefix(path, "/api/settings/") || strings.HasPrefix(path, "/api/config/") || strings.HasPrefix(path, "/api/apikeys") || strings.HasPrefix(path, "/api/blacklist") || strings.HasPrefix(path, "/api/notification")
}

func ensureAuditSchema(a *App) error {
	_, err := a.DB.Exec(`CREATE TABLE IF NOT EXISTS audit_events (id INTEGER PRIMARY KEY AUTOINCREMENT, actor_id TEXT NOT NULL, method TEXT NOT NULL, path TEXT NOT NULL, status INTEGER NOT NULL, ip TEXT NOT NULL, created_at TEXT NOT NULL)`)
	return err
}

func (a *App) auditEvents(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	page, limit := pageParams(r)
	if limit > 100 {
		limit = 100
	}
	var total int
	if err := a.DB.QueryRow(`SELECT count(*) FROM audit_events`).Scan(&total); err != nil {
		fail(w, 500, "查询审计记录失败")
		return
	}
	rows, err := a.DB.Query(`SELECT id,actor_id,method,path,status,ip,created_at FROM audit_events ORDER BY id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		fail(w, 500, "查询审计记录失败")
		return
	}
	defer rows.Close()
	type event struct {
		ID        int64  `json:"id"`
		ActorID   string `json:"actorId"`
		Method    string `json:"method"`
		Path      string `json:"path"`
		Status    int    `json:"status"`
		IP        string `json:"ip"`
		CreatedAt string `json:"createdAt"`
	}
	events := make([]event, 0, limit)
	for rows.Next() {
		var item event
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Method, &item.Path, &item.Status, &item.IP, &item.CreatedAt); err != nil {
			fail(w, 500, "查询审计记录失败")
			return
		}
		events = append(events, item)
	}
	if rows.Err() != nil {
		fail(w, 500, "查询审计记录失败")
		return
	}
	ok(w, map[string]any{"events": events, "pagination": map[string]int{"page": page, "limit": limit, "total": total, "totalPages": (total + limit - 1) / limit}})
}
