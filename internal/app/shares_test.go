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

func TestPrivateShareAllowsOnlyScopedFileAndRevocation(t *testing.T) {
	a := testApp(t)
	if err := ensureShareSchema(a); err != nil {
		t.Fatal(err)
	}
	id := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	im := Image{ID: "private-share-image", UUID: id, Filename: id + ".png", Format: "png", Visibility: "private", Size: int64(len(tinyPNG(t))), UploadedAt: now(), UpdatedAt: now()}
	if err := a.saveImage(im); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.DataDir, "uploads", im.Filename), tinyPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := a.DB.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,4102444800)`, tokenHash("share-admin"), userID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.registerShareRoutes(mux)
	create := httptest.NewRequest("POST", "/api/admin/shares", strings.NewReader(`{"imageIds":["private-share-image"],"title":"One image"}`))
	create.Header.Set("Authorization", "Bearer share-admin")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, create)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.ID == "" || !strings.HasPrefix(payload.Data.URL, "/share/") {
		t.Fatalf("share: %+v", payload.Data)
	}
	token := strings.TrimPrefix(payload.Data.URL, "/share/")
	var stored string
	if err := a.DB.QueryRow(`SELECT token_hash FROM image_shares WHERE id=?`, payload.Data.ID).Scan(&stored); err != nil || stored == token {
		t.Fatalf("token storage: %q %v", stored, err)
	}
	detail := httptest.NewRecorder()
	mux.ServeHTTP(detail, httptest.NewRequest("GET", "/api/shares/"+token, nil))
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), "private-share-image") {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	file := httptest.NewRecorder()
	mux.ServeHTTP(file, httptest.NewRequest("GET", "/api/shares/"+token+"/files/"+im.Filename, nil))
	if file.Code != 200 || file.Body.Len() != len(tinyPNG(t)) {
		t.Fatalf("file: %d", file.Code)
	}
	revoke := httptest.NewRequest("DELETE", "/api/admin/shares/"+payload.Data.ID, nil)
	revoke.Header.Set("Authorization", "Bearer share-admin")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, revoke)
	if w.Code != 200 {
		t.Fatalf("revoke: %d", w.Code)
	}
	file = httptest.NewRecorder()
	mux.ServeHTTP(file, httptest.NewRequest("GET", "/api/shares/"+token+"/files/"+im.Filename, nil))
	if file.Code != 404 {
		t.Fatalf("revoked file: %d", file.Code)
	}
}
