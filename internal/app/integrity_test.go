package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type integrityPage struct {
	Data struct {
		Kind       string `json:"kind"`
		Scanned    int    `json:"scanned"`
		Done       bool   `json:"done"`
		NextCursor string `json:"nextCursor"`
		Findings   []struct {
			Issue string `json:"issue"`
		} `json:"findings"`
	} `json:"data"`
}

func integrityRequest(a *App, token, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.registerIntegrityRoutes(mux)
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestIntegrityScanFindsImageAndUploadDiscrepancies(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	uploads := filepath.Join(a.DataDir, "uploads")
	issues := map[string]bool{}
	for _, specimen := range []struct {
		kind string
		size int64
	}{
		{kind: "ok", size: 3},
		{kind: "missing", size: 3},
		{kind: "size_mismatch", size: 8},
		{kind: "symlink", size: 3},
	} {
		id, err := newID()
		if err != nil {
			t.Fatal(err)
		}
		filename := id + ".png"
		if err := a.saveImage(Image{ID: specimen.kind, UUID: id, Filename: filename, Format: "png", Size: specimen.size, UploadedAt: now(), UpdatedAt: now()}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(uploads, filename)
		switch specimen.kind {
		case "ok", "size_mismatch":
			if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Symlink("nonexistent-target", path); err != nil {
				t.Fatal(err)
			}
		}
	}
	if r := integrityRequest(a, "", "/api/admin/integrity?kind=images"); r.Code != 401 {
		t.Fatalf("anonymous scan: %d", r.Code)
	}
	cursor := ""
	for range 4 {
		target := "/api/admin/integrity?kind=images&limit=2&cursor=" + url.QueryEscape(cursor)
		r := integrityRequest(a, token, target)
		if r.Code != 200 {
			t.Fatalf("images: %d %s", r.Code, r.Body.String())
		}
		var page integrityPage
		if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil || page.Data.Scanned > 2 {
			t.Fatalf("image page: %s %v", r.Body.String(), err)
		}
		for _, finding := range page.Data.Findings {
			issues[finding.Issue] = true
		}
		if page.Data.Done {
			break
		}
		if page.Data.NextCursor == cursor {
			t.Fatal("image cursor did not advance")
		}
		cursor = page.Data.NextCursor
	}
	for _, issue := range []string{"missing", "size_mismatch", "symlink"} {
		if !issues[issue] {
			t.Fatalf("did not find %s: %#v", issue, issues)
		}
	}

	orphanID, _ := newID()
	if err := os.WriteFile(filepath.Join(uploads, orphanID+".png"), []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	issues = map[string]bool{}
	cursor = ""
	for range 12 {
		target := "/api/admin/integrity?kind=uploads&limit=1&cursor=" + url.QueryEscape(cursor)
		r := integrityRequest(a, token, target)
		if r.Code != 200 {
			t.Fatalf("uploads: %d %s", r.Code, r.Body.String())
		}
		var page integrityPage
		if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil || page.Data.Scanned > 1 {
			t.Fatalf("upload page: %s %v", r.Body.String(), err)
		}
		for _, finding := range page.Data.Findings {
			issues[finding.Issue] = true
		}
		if page.Data.Done {
			break
		}
		if page.Data.NextCursor == cursor {
			t.Fatal("upload cursor did not advance")
		}
		cursor = page.Data.NextCursor
	}
	if !issues["orphan"] || !issues["symlink"] {
		t.Fatalf("upload findings: %#v", issues)
	}
	if _, err := os.Lstat(filepath.Join(uploads, orphanID+".png")); err != nil {
		t.Fatal("scan must not delete orphan", err)
	}
}

func TestIntegrityScanRejectsInvalidParameters(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	for _, target := range []string{
		"/api/admin/integrity?kind=unknown",
		"/api/admin/integrity?kind=uploads&limit=0",
		"/api/admin/integrity?kind=uploads&cursor=../secrets",
		"/api/admin/integrity?kind=images&cursor=" + strings.Repeat("x", 513),
	} {
		if r := integrityRequest(a, token, target); r.Code != 400 {
			t.Fatalf("%s: %d %s", target, r.Code, r.Body.String())
		}
	}
}
