package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportImagesStreamsSelectedOriginals(t *testing.T) {
	a := testApp(t)
	imageA := Image{ID: "a", UUID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Filename: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.png", OriginalName: "first.png", Format: "png", Size: 3, UploadedAt: now(), UpdatedAt: now()}
	imageB := Image{ID: "b", UUID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Filename: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb.png", OriginalName: "second.png", Format: "png", Size: 4, UploadedAt: now(), UpdatedAt: now()}
	for _, im := range []Image{imageA, imageB} {
		if err := a.saveImage(im); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.DataDir, "uploads", imageA.Filename), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.DataDir, "uploads", imageB.Filename), []byte("two!"), 0600); err != nil {
		t.Fatal(err)
	}

	request := func(auth bool, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/images/export", strings.NewReader(body))
		if auth {
			var userID string
			if err := a.DB.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
				t.Fatal(err)
			}
			if _, err := a.DB.Exec(`INSERT OR IGNORE INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,4102444800)`, tokenHash("zip-test"), userID); err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer zip-test")
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := request(false, `{"ids":["a"]}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	if rec := request(true, `{"ids":["a","missing"]}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", rec.Code, rec.Body.String())
	}
	rec := request(true, `{"ids":["a","b"]}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("files: %d", len(zr.File))
	}
	got := make(map[string]string)
	for _, file := range zr.File {
		f, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[file.Name] = string(content)
	}
	if got[imageA.Filename] != "one" || got[imageB.Filename] != "two!" {
		t.Fatalf("contents: %#v", got)
	}
	var parsed map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &parsed) == nil {
		t.Fatal("expected ZIP, got JSON")
	}
}
