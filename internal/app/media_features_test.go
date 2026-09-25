package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPreVisibilityDatabaseKeepsLegacyLinksAndBackfillsPublic(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "tanoimg.db")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE images (id TEXT PRIMARY KEY, uuid TEXT NOT NULL UNIQUE, filename TEXT NOT NULL, original_name TEXT NOT NULL DEFAULT '', format TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, uploaded_by TEXT NOT NULL DEFAULT '', uploaded_by_type TEXT NOT NULL DEFAULT 'private', uploaded_at TEXT NOT NULL, updated_at TEXT NOT NULL, is_deleted INTEGER NOT NULL DEFAULT 0, is_nsfw INTEGER NOT NULL DEFAULT 0)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, kind string }{
		{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "private"},
		{"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "public"},
	} {
		name := item.id + ".png"
		if _, err := db.Exec(`INSERT INTO images(id,uuid,filename,format,size,uploaded_by_type,uploaded_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, item.id, item.id, name, "png", len(tinyPNG(t)), item.kind, now(), now()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "uploads", name), tinyPNG(t), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	gallery := adminRequest(a, "", http.MethodGet, "/api/images", "")
	if gallery.Code != 200 || strings.Contains(gallery.Body.String(), "aaaaaaaa-aaaa") || !strings.Contains(gallery.Body.String(), "bbbbbbbb-bbbb") {
		t.Fatalf("backfilled gallery: %d %s", gallery.Code, gallery.Body.String())
	}
	for _, id := range []string{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"} {
		if rec := adminRequest(a, "", http.MethodGet, "/i/"+id+".png", ""); rec.Code != 200 {
			t.Fatalf("legacy link %s: %d", id, rec.Code)
		}
	}
}

func mediaUpload(t *testing.T, a *App, token, target string, fields map[string]string) (Image, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "sample.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(tinyPNG(t)); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if err := form.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	if strings.HasPrefix(token, "sk-") {
		req.Header.Set("X-API-Key", token)
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	var envelope struct {
		Data Image `json:"data"`
	}
	if rec.Code == 200 && json.Unmarshal(rec.Body.Bytes(), &envelope) != nil {
		t.Fatalf("invalid upload response: %s", rec.Body.String())
	}
	return envelope.Data, rec
}

func TestEasyImgRerunPreservesEditedMediaFields(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "db"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	id := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	name := id + ".png"
	data := tinyPNG(t)
	if err := os.WriteFile(filepath.Join(root, "uploads", name), data, 0600); err != nil {
		t.Fatal(err)
	}
	doc := `{"_id":"legacy-record","uuid":"` + id + `","filename":"` + name + `","size":` + strconv.Itoa(len(data)) + `,"uploadedByType":"private"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "db", "images.db"), []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MigrateEasyImg(root); err != nil {
		t.Fatal(err)
	}
	token := adminToken(t, a)
	if rec := adminRequest(a, token, http.MethodPut, "/api/images/legacy-record/metadata", `{"visibility":"private","alt":"Saved alt","tags":["edited"]}`); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if _, err := a.MigrateEasyImg(root); err != nil {
		t.Fatal(err)
	}
	im, err := a.imageByID("legacy-record")
	if err != nil || im.Visibility != "private" || im.Alt != "Saved alt" || len(im.Tags) != 1 || im.Tags[0] != "edited" {
		t.Fatalf("metadata lost on reimport: %+v %v", im, err)
	}
}

func TestAPIKeyDuplicateHintDoesNotRevealOtherPrivateImage(t *testing.T) {
	a := testApp(t)
	admin := adminToken(t, a)
	secret, rec := mediaUpload(t, a, admin, "/api/upload/private?visibility=private", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if _, err := a.DB.Exec(`INSERT INTO apikeys(id,key,name,enabled,is_default,created_at) VALUES('key-owner','sk-owner','Owner',1,0,'')`); err != nil {
		t.Fatal(err)
	}
	first, rec := mediaUpload(t, a, "sk-owner", "/api/upload/private?visibility=private", nil)
	if rec.Code != 200 || first.DuplicateOf != "" {
		t.Fatalf("private ID leaked: %d %s", rec.Code, rec.Body.String())
	}
	second, rec := mediaUpload(t, a, "sk-owner", "/api/upload/private?visibility=private", nil)
	if rec.Code != 200 || second.DuplicateOf != first.ID || second.DuplicateOf == secret.ID {
		t.Fatalf("same-key duplicate hint: %d %s", rec.Code, rec.Body.String())
	}
}

func TestLegacyUnlistedLinksSurvivePrivateAccessControl(t *testing.T) {
	a := testApp(t)
	legacy := fixtureImage(t, a, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", false)
	if rec := adminRequest(a, "", http.MethodGet, "/i/"+legacy.Filename, ""); rec.Code != 200 {
		t.Fatalf("legacy direct link: %d", rec.Code)
	}
	token := adminToken(t, a)
	private, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=private", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"visibility":"private"`) {
		t.Fatalf("private upload: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(a, "", http.MethodGet, private.URL, ""); rec.Code != 404 {
		t.Fatalf("anonymous private file: %d", rec.Code)
	}
	if rec := adminRequest(a, token, http.MethodGet, private.URL, ""); rec.Code != 200 {
		t.Fatalf("admin private file: %d", rec.Code)
	}
	if rec := adminRequest(a, "", http.MethodGet, "/api/images", ""); strings.Contains(rec.Body.String(), private.ID) || strings.Contains(rec.Body.String(), legacy.ID) {
		t.Fatalf("unlisted/private in public gallery: %s", rec.Body.String())
	}
}

func TestPublicGalleryNeverPromotesUnlistedWithLegacySetting(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	a.setSetting("privateApiConfig", json.RawMessage(`{"showOnHomepage":true}`))
	unlisted, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=unlisted", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec := adminRequest(a, "", http.MethodGet, unlisted.URL, ""); rec.Code != 200 {
		t.Fatalf("unlisted direct link: %d", rec.Code)
	}
	if rec := adminRequest(a, "", http.MethodGet, "/api/images", ""); strings.Contains(rec.Body.String(), unlisted.ID) {
		t.Fatalf("unlisted in public gallery: %s", rec.Body.String())
	}
}

func TestImageMetadataSearchAndDuplicateHint(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	first, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=public", map[string]string{
		"alt": "Golden bridge", "author": "Ada", "license": "CC BY 4.0", "tags": "travel, sunset",
	})
	if rec.Code != 200 || first.Alt != "Golden bridge" || first.Author != "Ada" || first.License != "CC BY 4.0" || len(first.Tags) != 2 || first.Tags[0] != "travel" || first.Tags[1] != "sunset" {
		t.Fatalf("upload metadata: %d %s", rec.Code, rec.Body.String())
	}
	second, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=unlisted", nil)
	if rec.Code != 200 || second.DuplicateOf != first.ID {
		t.Fatalf("exact duplicate hint: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(a, token, http.MethodGet, "/api/images?q=Golden&tag=travel&visibility=public", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), first.ID) || strings.Contains(rec.Body.String(), second.ID) {
		t.Fatalf("search/filter: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(a, "", http.MethodGet, "/api/images?tag=travel", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), first.ID) || strings.Contains(rec.Body.String(), second.ID) {
		t.Fatalf("public tag filter: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImageMetadataAndBatchVisibilityUpdates(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	first, rec := mediaUpload(t, a, token, "/api/upload/private?visibility=private", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec := adminRequest(a, token, http.MethodPatch, "/api/images/"+first.ID, `{"alt":"Edited","tags":["family","2026"]}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"alt":"Edited"`) {
		t.Fatalf("metadata patch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(a, token, http.MethodPatch, "/api/images/batch", `{"ids":["`+first.ID+`"],"visibility":"unlisted"}`); rec.Code != 200 {
		t.Fatalf("batch visibility: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(a, "", http.MethodGet, first.URL, ""); rec.Code != 200 {
		t.Fatalf("unlisted file after batch: %d", rec.Code)
	}
	if rec := adminRequest(a, "", http.MethodGet, "/api/images?tag=family", ""); strings.Contains(rec.Body.String(), first.ID) {
		t.Fatalf("unlisted tagged image in public gallery: %s", rec.Body.String())
	}
}
