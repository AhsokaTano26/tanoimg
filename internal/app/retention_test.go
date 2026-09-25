package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func retentionRequest(a *App, token, method, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.registerRetentionRoutes(mux)
	r := httptest.NewRequest(method, "/api/admin/retention", strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestRecycleRetentionConfigAndDisabledWorker(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	if r := retentionRequest(a, "", "GET", ""); r.Code != 401 {
		t.Fatalf("anonymous config: %d", r.Code)
	}
	if r := retentionRequest(a, token, "GET", ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"days":30`) {
		t.Fatalf("default: %d %s", r.Code, r.Body.String())
	}
	for _, body := range []string{`{"days":-1}`, `{"days":3651}`, `{"days":"30"}`} {
		if r := retentionRequest(a, token, "PUT", body); r.Code != 400 {
			t.Fatalf("invalid %s: %d", body, r.Code)
		}
	}
	if r := retentionRequest(a, token, "PUT", `{"days":0}`); r.Code != 200 {
		t.Fatalf("disable: %d %s", r.Code, r.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := a.StartRetention(ctx)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop on cancellation")
	}
}

func TestRecycleRetentionDeletesOnlyExpiredTimestampedImages(t *testing.T) {
	a := testApp(t)
	nowTime := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	uploads := filepath.Join(a.DataDir, "uploads")
	for _, specimen := range []struct {
		id        string
		deletedAt string
	}{
		{id: "expired", deletedAt: nowTime.Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano)},
		{id: "recent", deletedAt: nowTime.Add(-10 * 24 * time.Hour).Format(time.RFC3339Nano)},
		{id: "legacy", deletedAt: ""},
		{id: "invalid", deletedAt: "not-a-date"},
	} {
		uuid, err := newID()
		if err != nil {
			t.Fatal(err)
		}
		filename := uuid + ".png"
		if err := os.WriteFile(filepath.Join(uploads, filename), []byte("image"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := a.saveImage(Image{ID: specimen.id, UUID: uuid, Filename: filename, Format: "png", Size: 5, IsDeleted: true, DeletedAt: specimen.deletedAt, UploadedAt: now(), UpdatedAt: now()}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.cleanupRetentionBatch(context.Background(), nowTime.Add(-30*24*time.Hour), "")
	if err != nil || result.Deleted != 1 {
		t.Fatalf("cleanup: %+v %v", result, err)
	}
	for _, specimen := range []struct {
		id      string
		present bool
	}{
		{id: "expired", present: false},
		{id: "recent", present: true},
		{id: "legacy", present: true},
		{id: "invalid", present: true},
	} {
		var filename string
		err := a.DB.QueryRow(`SELECT filename FROM images WHERE id=?`, specimen.id).Scan(&filename)
		if specimen.present && err != nil {
			t.Fatalf("%s must remain: %v", specimen.id, err)
		}
		if !specimen.present && err != sql.ErrNoRows {
			t.Fatalf("%s must be removed: %v", specimen.id, err)
		}
		if specimen.present {
			if _, err := os.Stat(filepath.Join(uploads, filename)); err != nil {
				t.Fatalf("%s file missing: %v", specimen.id, err)
			}
		}
	}
	var raw string
	if err := a.DB.QueryRow(`SELECT value FROM settings WHERE key='recycleRetentionDays'`).Scan(&raw); err != sql.ErrNoRows {
		t.Fatalf("cleanup must not change setting: %q %v", raw, err)
	}
}

func TestRecycleRetentionSkipsSuspiciousPathsAndRestoredRows(t *testing.T) {
	a := testApp(t)
	nowTime := time.Now().UTC()
	uuid, _ := newID()
	filename := uuid + ".png"
	if err := os.Symlink("outside", filepath.Join(a.DataDir, "uploads", filename)); err != nil {
		t.Fatal(err)
	}
	for _, im := range []Image{
		{ID: "symlink", UUID: uuid, Filename: filename, Size: 1, IsDeleted: true, DeletedAt: nowTime.Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano), UploadedAt: now(), UpdatedAt: now()},
		{ID: "restored", UUID: strings.Repeat("2", 8) + "-2222-2222-2222-222222222222", Filename: "22222222-2222-2222-2222-222222222222.png", Size: 1, IsDeleted: false, DeletedAt: nowTime.Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano), UploadedAt: now(), UpdatedAt: now()},
	} {
		if err := a.saveImage(im); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.cleanupRetentionBatch(context.Background(), nowTime.Add(-30*24*time.Hour), "")
	if err != nil || result.Deleted != 0 {
		t.Fatalf("cleanup should skip anomalies: %+v %v", result, err)
	}
	for _, id := range []string{"symlink", "restored"} {
		var count int
		if err := a.DB.QueryRow(`SELECT count(*) FROM images WHERE id=?`, id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s record changed: %d %v", id, count, err)
		}
	}
	if r := retentionRequest(a, migrationLogin(t, a), "PUT", `{"days":14}`); r.Code != 200 {
		t.Fatalf("setting: %d %s", r.Code, r.Body.String())
	}
	var days int
	if err := json.Unmarshal(a.setting("recycleRetentionDays", 30), &days); err != nil || days != 14 {
		t.Fatalf("persisted days: %d %v", days, err)
	}
}
