package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditRecordsAdminMutationWithoutCredentials(t *testing.T) {
	a := testApp(t)
	if err := ensureAuditSchema(a); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := a.DB.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,4102444800)`, tokenHash("audit-test"), userID); err != nil {
		t.Fatal(err)
	}
	wrapped := a.auditHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodPut, "/api/settings/appearance?apiKey=secret-value", strings.NewReader(`{"password":"also-secret"}`))
	req.Header.Set("Authorization", "Bearer audit-test")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("response: %d", rec.Code)
	}
	var method, path string
	var status int
	if err := a.DB.QueryRow(`SELECT method,path,status FROM audit_events`).Scan(&method, &path, &status); err != nil {
		t.Fatal(err)
	}
	if method != "PUT" || path != "/api/settings/appearance" || status != http.StatusNoContent {
		t.Fatalf("event: %s %s %d", method, path, status)
	}
	if strings.Contains(path, "secret") {
		t.Fatal("audit event leaked credential")
	}
}
