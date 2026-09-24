package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

type gatedReader struct {
	data    *bytes.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gatedReader) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.started) })
	<-g.release
	return g.data.Read(p)
}

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

func TestEasyImgSettingsAndPrivateConfig(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	if got := adminRequest(a, "", http.MethodGet, "/api/settings", ""); got.Code != 401 {
		t.Fatalf("anonymous settings: %d", got.Code)
	}
	if got := adminRequest(a, token, http.MethodPut, "/api/settings", `{"appName":"Photo Vault","appLogo":"/i/logo.png","siteUrl":"https://photos.example/","announcement":{"enabled":true,"content":"Hello","displayType":"banner"}}`); got.Code != 200 {
		t.Fatalf("settings update: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodGet, "/api/settings", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"siteUrl":"https://photos.example"`) {
		t.Fatalf("settings read: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, "", http.MethodGet, "/api/settings/public", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"appName":"Photo Vault"`) || strings.Contains(got.Body.String(), "siteUrl") {
		t.Fatalf("public settings: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodPut, "/api/config/private", `{"maxFileSize":20971520,"showOnHomepage":true}`); got.Code != 200 {
		t.Fatalf("private config update: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodGet, "/api/config/private", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"showOnHomepage":true`) {
		t.Fatalf("private config read: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodPut, "/api/config/private", `{"maxFileSize":0}`); got.Code != 400 {
		t.Fatalf("invalid private config: %d", got.Code)
	}
}

func TestEasyImgUsernameAndAPIKeyUpdates(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	if got := adminRequest(a, token, http.MethodPut, "/api/admin/username", `{"username":"newadmin"}`); got.Code != 200 {
		t.Fatalf("username update: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodGet, "/api/auth/verify", ""); got.Code != 200 || !strings.Contains(got.Body.String(), "newadmin") {
		t.Fatalf("session after username change: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, "", http.MethodPost, "/api/auth/login", `{"username":"newadmin","password":"strong-test-password"}`); got.Code != 200 {
		t.Fatalf("new username login: %d %s", got.Code, got.Body.String())
	}
	if _, err := a.DB.Exec(`INSERT INTO apikeys(id,key,name,enabled,is_default,created_at) VALUES('key1','sk-old','Original',1,0,'')`); err != nil {
		t.Fatal(err)
	}
	if got := adminRequest(a, token, http.MethodPut, "/api/apikeys/key1", `{"name":"Renamed","enabled":false,"regenerate":true}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"name":"Renamed"`) || strings.Contains(got.Body.String(), `"key":"sk-old"`) {
		t.Fatalf("key update: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodPut, "/api/apikeys/key1", `{"enabled":true}`); got.Code != 200 {
		t.Fatalf("enable key: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodPut, "/api/apikeys/missing", `{"enabled":true}`); got.Code != 404 {
		t.Fatalf("missing key: %d", got.Code)
	}
}

func TestEasyImgURLUpload(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	pngData := tinyPNG(t)
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(pngData)), Request: r}, nil
	})}
	if got := adminRequest(a, "", http.MethodPost, "/api/upload/url", `{"url":"https://images.example/photo.png"}`); got.Code != 401 {
		t.Fatalf("anonymous url upload: %d", got.Code)
	}
	if got := adminRequest(a, token, http.MethodPost, "/api/upload/url", `{"url":"http://127.0.0.1/private.png"}`); got.Code != 400 {
		t.Fatalf("loopback fetched: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodPost, "/api/upload/url", `{"url":"https://images.example/photo.png"}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"uploadedByType":"url"`) {
		t.Fatalf("url upload: %d %s", got.Code, got.Body.String())
	}
	var count int
	if err := a.DB.QueryRow(`SELECT count(*) FROM images WHERE uploaded_by_type='url'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("url row: %d %v", count, err)
	}
	if got := adminRequest(a, token, http.MethodPost, "/api/upload/url", `{"url":["https://images.example/a.png","http://127.0.0.1/private.png"]}`); got.Code != 200 || !strings.Contains(got.Body.String(), `"successCount":1`) || !strings.Contains(got.Body.String(), `"errorCount":1`) {
		t.Fatalf("array upload: %d %s", got.Code, got.Body.String())
	}
}

func TestEasyImgURLUploadSSE(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	pngData := tinyPNG(t)
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(pngData)), Request: r}, nil
	})}
	got := adminRequest(a, token, http.MethodPost, "/api/upload/urls", `{"urls":["https://images.example/a.png","https://images.example/a.png"]}`)
	if got.Code != 200 || !strings.HasPrefix(got.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(got.Body.String(), "event: complete") || !strings.Contains(got.Body.String(), `"successCount":1`) {
		t.Fatalf("SSE: %d %s", got.Code, got.Body.String())
	}
}

func TestMigrationPreservesModerationMetadataAndStats(t *testing.T) {
	old := t.TempDir()
	if err := os.MkdirAll(filepath.Join(old, "db"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(old, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	name := "44444444-4444-4444-8444-444444444444.png"
	if err := os.WriteFile(filepath.Join(old, "uploads", name), tinyPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	doc := `{"_id":"moderated","uuid":"44444444-4444-4444-8444-444444444444","filename":"` + name + `","format":"png","size":100,"uploadedByType":"public","isNsfw":true,"moderationChecked":true,"moderationStatus":"completed","moderationResult":{"score":0.94},"ip":"192.0.2.10"}` + "\n"
	if err := os.WriteFile(filepath.Join(old, "db", "images.db"), []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	a := testApp(t)
	if _, err := a.MigrateEasyImg(old); err != nil {
		t.Fatal(err)
	}
	token := adminToken(t, a)
	if got := adminRequest(a, token, http.MethodGet, "/api/images/nsfw", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"moderationScore":0.94`) || !strings.Contains(got.Body.String(), `"moderationStatus":"completed"`) {
		t.Fatalf("moderation metadata: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodGet, "/api/settings/stats", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"moderatedImagesCount":1`) || !strings.Contains(got.Body.String(), `"nsfwImagesCount":1`) {
		t.Fatalf("moderation stats: %d %s", got.Code, got.Body.String())
	}
}

func TestEasyImgPublicModerationQueue(t *testing.T) {
	a := testApp(t)
	config := `{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576,"rateLimit":10,"contentSafety":{"enabled":true,"provider":"nsfwdet","autoBlacklistIp":true,"providers":{"nsfwdet":{"apiUrl":"https://moderator.example/check","apiKey":"test-key","threshold":0.5}}}}`
	if err := a.setSetting("publicApiConfig", json.RawMessage(config)); err != nil {
		t.Fatal(err)
	}
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("moderation request: %s %s", r.Method, r.Header.Get("X-API-Key"))
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"result":{"nsfw":0.93}}`)), Request: r}, nil
	})}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "photo.png")
	file.Write(tinyPNG(t))
	form.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload/public", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.RemoteAddr = "192.0.2.10:1234"
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("public upload: %d %s", rec.Code, rec.Body.String())
	}
	var pending int
	if err := a.DB.QueryRow(`SELECT count(*) FROM moderation_tasks WHERE status='pending'`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending task: %d %v", pending, err)
	}
	if err := a.processOneModeration(context.Background()); err != nil {
		t.Fatal(err)
	}
	var nsfw, checked bool
	var score float64
	if err := a.DB.QueryRow(`SELECT is_nsfw,moderation_checked,moderation_score FROM images LIMIT 1`).Scan(&nsfw, &checked, &score); err != nil || !nsfw || !checked || score != 0.93 {
		t.Fatalf("moderation outcome: %t %t %.2f %v", nsfw, checked, score, err)
	}
	var blocked int
	if err := a.DB.QueryRow(`SELECT count(*) FROM ip_blacklist WHERE ip='192.0.2.10'`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("auto blacklist: %d %v", blocked, err)
	}
}

func TestModerationDefaultsToElysia(t *testing.T) {
	a := testApp(t)
	filename := "55555555-5555-4555-8555-555555555555.png"
	imagePath := filepath.Join(a.DataDir, "uploads", filename)
	if err := os.WriteFile(imagePath, tinyPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"filePath":"remote-image"}`
		if strings.Contains(r.URL.Path, "/api/tools/") {
			body = `{"data":{"data":{"isSafe":true},"confidence":99}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	outcome, err := a.moderateFile(context.Background(), imagePath, filename, contentSafetyConfig{Enabled: true})
	if err != nil || outcome.NSFW {
		t.Fatalf("default provider: %+v %v", outcome, err)
	}
}

func TestPublicGalleryHidesSourceURL(t *testing.T) {
	a := testApp(t)
	im := fixtureImage(t, a, "66666666-6666-4666-8666-666666666666", false)
	im.UploadedByType = "public"
	im.SourceURL = "https://images.example/photo.png?token=secret"
	if err := a.saveImage(im); err != nil {
		t.Fatal(err)
	}
	got := adminRequest(a, "", http.MethodGet, "/api/images", "")
	if got.Code != 200 || strings.Contains(got.Body.String(), "token=secret") {
		t.Fatalf("public gallery leaked URL: %d %s", got.Code, got.Body.String())
	}
}

func TestPublicConfigRejectsUnsupportedModerationProvider(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	got := adminRequest(a, token, http.MethodPut, "/api/config/public", `{"contentSafety":{"enabled":true,"provider":"unknown"}}`)
	if got.Code != 400 {
		t.Fatalf("unsupported provider accepted: %d %s", got.Code, got.Body.String())
	}
}

func TestPublicUploadRespectsSameIPConcurrencySetting(t *testing.T) {
	a := testApp(t)
	a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576,"rateLimit":10,"allowConcurrent":false}`))
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "photo.png")
	file.Write(tinyPNG(t))
	form.Close()
	gated := &gatedReader{data: bytes.NewReader(body.Bytes()), started: make(chan struct{}), release: make(chan struct{})}
	first := httptest.NewRequest(http.MethodPost, "/api/upload/public", gated)
	first.Header.Set("Content-Type", form.FormDataContentType())
	first.RemoteAddr = "192.0.2.55:1000"
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { rec := httptest.NewRecorder(); a.Handler().ServeHTTP(rec, first); firstDone <- rec }()
	<-gated.started
	second := httptest.NewRequest(http.MethodPost, "/api/upload/public", bytes.NewReader(body.Bytes()))
	second.Header.Set("Content-Type", form.FormDataContentType())
	second.RemoteAddr = "192.0.2.55:2000"
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, second)
	close(gated.release)
	if rec.Code != 429 {
		t.Fatalf("same IP concurrent upload: %d %s", rec.Code, rec.Body.String())
	}
	if completed := <-firstDone; completed.Code != 200 {
		t.Fatalf("first upload: %d %s", completed.Code, completed.Body.String())
	}
}

func TestEasyImgNotificationConfigAndWebhook(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	if got := adminRequest(a, "", http.MethodGet, "/api/notification", ""); got.Code != 401 {
		t.Fatalf("anonymous notification config: %d", got.Code)
	}
	config := `{"enabled":true,"method":"webhook","types":{"login":true,"upload":true,"nsfw":true},"webhook":{"url":"https://hooks.example/notify","method":"POST","contentType":"application/json","headers":{"X-Test":"yes"},"bodyTemplate":"{\"kind\":\"{{type}}\",\"text\":\"{{message}}\"}"}}`
	if got := adminRequest(a, token, http.MethodPut, "/api/notification", config); got.Code != 200 {
		t.Fatalf("save notification config: %d %s", got.Code, got.Body.String())
	}
	if got := adminRequest(a, token, http.MethodGet, "/api/notification", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"method":"webhook"`) {
		t.Fatalf("notification config: %d %s", got.Code, got.Body.String())
	}
	called := false
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Test") != "yes" || !strings.Contains(string(b), `"kind":"test"`) {
			t.Errorf("webhook request: %s %s", r.Header.Get("X-Test"), b)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})}
	if got := adminRequest(a, token, http.MethodPost, "/api/notification/test", config); got.Code != 200 || !called {
		t.Fatalf("test notification: %d %s", got.Code, got.Body.String())
	}
}
