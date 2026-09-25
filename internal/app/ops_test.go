package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func opsRequest(a *App, token, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.registerOpsRoutes(mux)
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestOpsReadyAndAdminHealth(t *testing.T) {
	a := testApp(t)
	if r := opsRequest(a, "", "/readyz"); r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"ready"`) {
		t.Fatalf("ready: %d %s", r.Code, r.Body.String())
	}
	if r := opsRequest(a, "", "/api/admin/health"); r.Code != 401 {
		t.Fatalf("anonymous deep health: %d", r.Code)
	}
	token := migrationLogin(t, a)
	r := opsRequest(a, token, "/api/admin/health")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"availableBytes"`) || !strings.Contains(r.Body.String(), `"moderation"`) {
		t.Fatalf("deep health: %d %s", r.Code, r.Body.String())
	}
	if err := a.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if r := opsRequest(a, "", "/readyz"); r.Code != 503 {
		t.Fatalf("closed database should fail readiness: %d %s", r.Code, r.Body.String())
	}
}

func TestOpsTaskCenterIsPagedAndRedacted(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	if _, err := a.DB.Exec(`INSERT INTO moderation_tasks(id,image_id,filename,status,error,created_at,updated_at) VALUES('m1','image-1','file.jpg','error','https://secret.example/token','2026-01-01','2026-01-01'),('m2','image-2','file2.jpg','pending','','2026-01-02','2026-01-02')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO notification_events(kind,payload,status,error) VALUES('upload','{"token":"secret"}','error','very-secret')`); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 64)
	if err := os.MkdirAll(filepath.Join(a.DataDir, "imports", id), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.saveMigration(&remoteMigration{ID: id, Owner: "owner-secret", Phase: "failed", Error: "secret-path", Size: 123}); err != nil {
		t.Fatal(err)
	}
	if r := opsRequest(a, "", "/api/admin/tasks?kind=moderation"); r.Code != 401 {
		t.Fatalf("anonymous tasks: %d", r.Code)
	}
	r := opsRequest(a, token, "/api/admin/tasks?kind=moderation&page=2&limit=1")
	if r.Code != 200 {
		t.Fatalf("moderation: %d %s", r.Code, r.Body.String())
	}
	var result struct {
		Data struct {
			Total int              `json:"total"`
			Tasks []map[string]any `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || result.Data.Total != 2 || len(result.Data.Tasks) != 1 {
		t.Fatalf("pagination: %s %v", r.Body.String(), err)
	}
	for _, kind := range []string{"moderation", "notification", "migration"} {
		r = opsRequest(a, token, "/api/admin/tasks?kind="+kind)
		if r.Code != 200 || strings.Contains(r.Body.String(), "secret") {
			t.Fatalf("%s should redact sensitive fields: %d %s", kind, r.Code, r.Body.String())
		}
	}
	r = opsRequest(a, token, "/api/admin/health")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"alerts"`) || !strings.Contains(r.Body.String(), `"notification_errors"`) {
		t.Fatalf("queue alert: %d %s", r.Code, r.Body.String())
	}
	r = opsRequest(a, token, "/api/admin/tasks/summary")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"moderation"`) || !strings.Contains(r.Body.String(), `"migration"`) {
		t.Fatalf("summary: %d %s", r.Code, r.Body.String())
	}
	if r := opsRequest(a, token, "/api/admin/tasks?kind=unknown"); r.Code != 400 {
		t.Fatalf("unknown kind: %d", r.Code)
	}
}
