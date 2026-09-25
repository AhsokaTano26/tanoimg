package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaintenanceBlocksVisitorWritesAndAllowsAdminRecovery(t *testing.T) {
	a := testApp(t)
	var userID string
	if err := a.DB.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,4102444800)`, tokenHash("maintenance-test"), userID); err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(maintenanceConfig{Enabled: true, Message: "升级中", RetryAfterSeconds: 90})
	if err := a.setSetting("maintenanceMode", value); err != nil {
		t.Fatal(err)
	}
	called := 0
	handler := a.maintenanceHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(204) }))
	visitor := httptest.NewRequest(http.MethodPost, "/api/upload/public", strings.NewReader("payload"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, visitor)
	if rec.Code != 503 || rec.Header().Get("Retry-After") != "90" || called != 0 {
		t.Fatalf("visitor: %d %s", rec.Code, rec.Body.String())
	}
	admin := httptest.NewRequest(http.MethodPost, "/api/upload/private", nil)
	admin.Header.Set("Authorization", "Bearer maintenance-test")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, admin)
	if rec.Code != 204 || called != 1 {
		t.Fatalf("admin: %d", rec.Code)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, login)
	if rec.Code != 204 || called != 2 {
		t.Fatalf("login: %d", rec.Code)
	}
}
