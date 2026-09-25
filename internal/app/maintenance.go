package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type maintenanceConfig struct {
	Enabled           bool   `json:"enabled"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
}

func (a *App) maintenanceStatus() maintenanceConfig {
	c := maintenanceConfig{RetryAfterSeconds: 60}
	_ = json.Unmarshal(a.setting("maintenanceMode", c), &c)
	if c.RetryAfterSeconds < 1 || c.RetryAfterSeconds > 3600 {
		c.RetryAfterSeconds = 60
	}
	return c
}

func (a *App) registerMaintenanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/maintenance", a.getMaintenance)
	mux.HandleFunc("PUT /api/admin/maintenance", a.putMaintenance)
}

func (a *App) getMaintenance(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	ok(w, a.maintenanceStatus())
}

func (a *App) putMaintenance(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var c maintenanceConfig
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || len([]rune(c.Message)) > 500 || c.RetryAfterSeconds < 1 || c.RetryAfterSeconds > 3600 {
		fail(w, 400, "维护模式配置无效")
		return
	}
	c.Message = strings.TrimSpace(c.Message)
	value, _ := json.Marshal(c)
	if err := a.setSetting("maintenanceMode", value); err != nil {
		fail(w, 500, "保存维护模式失败")
		return
	}
	ok(w, c)
}

func (a *App) maintenanceHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || strings.HasPrefix(r.URL.Path, "/api/auth/") || r.URL.Path == "/api/admin/maintenance" {
			next.ServeHTTP(w, r)
			return
		}
		c := a.maintenanceStatus()
		if !c.Enabled || a.userID(r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Retry-After", "60")
		if c.RetryAfterSeconds != 60 {
			w.Header().Set("Retry-After", strconv.Itoa(c.RetryAfterSeconds))
		}
		message := c.Message
		if message == "" {
			message = "站点正在维护，请稍后重试"
		}
		fail(w, http.StatusServiceUnavailable, message)
	})
}
