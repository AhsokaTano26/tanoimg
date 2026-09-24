package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func adminToken(t *testing.T, a *App) string {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Data.Token == "" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	return result.Data.Token
}

func adminRequest(a *App, token, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func fixtureImage(t *testing.T, a *App, id string, nsfw bool) Image {
	t.Helper()
	im := Image{ID: id, UUID: id, Filename: id + ".png", OriginalName: id + ".png", Format: "png", Size: 100, UploadedByType: "private", UploadedAt: now(), UpdatedAt: now(), IsNsfw: nsfw}
	if err := os.WriteFile(filepath.Join(a.DataDir, "uploads", im.Filename), tinyPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.saveImage(im); err != nil {
		t.Fatal(err)
	}
	return im
}

func TestEasyImgBatchDeleteAndRecycleBin(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	one := fixtureImage(t, a, "11111111-1111-4111-8111-111111111111", false)
	two := fixtureImage(t, a, "22222222-2222-4222-8222-222222222222", false)
	if got := adminRequest(a, "", http.MethodDelete, "/api/images/batch", `{"ids":["`+one.ID+`"]}`); got.Code != 401 {
		t.Fatalf("anonymous batch delete: %d", got.Code)
	}
	if got := adminRequest(a, token, http.MethodDelete, "/api/images/batch", `{"ids":["`+one.ID+`","`+two.ID+`","missing"]}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"deletedCount":2`) {
		t.Fatalf("batch delete: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodPost, "/api/settings/hard-delete", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"deletedCount":2`) {
		t.Fatalf("hard delete: %d %s", got.Code, got.Body.String())
	}
	for _, im := range []Image{one, two} {
		if _, err := os.Stat(filepath.Join(a.DataDir, "uploads", im.Filename)); !os.IsNotExist(err) {
			t.Fatalf("file remains: %s %v", im.Filename, err)
		}
		var count int
		if err := a.DB.QueryRow(`SELECT count(*) FROM images WHERE id=?`, im.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("row remains: %s %d %v", im.ID, count, err)
		}
	}
}

func TestEasyImgNSFWReviewAndPreview(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	im := fixtureImage(t, a, "33333333-3333-4333-8333-333333333333", true)
	if _, err := a.DB.Exec(`UPDATE images SET is_deleted=1 WHERE id=?`, im.ID); err != nil {
		t.Fatal(err)
	}
	preview := "/api/images/preview/" + im.Filename
	if got := adminRequest(a, "", http.MethodGet, preview, ""); got.Code != 401 {
		t.Fatalf("public preview: %d", got.Code)
	}
	if got := adminRequest(a, token, http.MethodGet, preview, ""); got.Code != 200 || !bytes.Equal(got.Body.Bytes(), tinyPNG(t)) {
		t.Fatalf("admin preview: %d", got.Code)
	}
	if got := adminRequest(a, token, http.MethodGet, "/api/images/nsfw", ""); got.Code != 200 || !strings.Contains(got.Body.String(), im.ID) {
		t.Fatalf("nsfw list: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodPut, "/api/images/"+im.ID+"/unmark-nsfw", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"restored":true`) {
		t.Fatalf("unmark: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, "", http.MethodGet, "/i/"+im.Filename, ""); got.Code != 200 {
		t.Fatalf("restored image: %d", got.Code)
	}
}
