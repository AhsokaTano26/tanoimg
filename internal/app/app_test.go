package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func testApp(t *testing.T) *App {
	t.Helper()
	a, err := New(Config{DataDir: t.TempDir(), AdminUsername: "admin", AdminPassword: "strong-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 3))
	im.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAppearanceSettings(t *testing.T) {
	a := testApp(t)
	if err := a.setSetting("appSettings", json.RawMessage(`{"appName":"Legacy","announcement":"keep me"}`)); err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	a.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	var auth struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &auth); err != nil || auth.Data.Token == "" {
		t.Fatalf("login: %s %v", login.Body.String(), err)
	}

	request := func(method, path, body string, admin bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if admin {
			req.Header.Set("Authorization", "Bearer "+auth.Data.Token)
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}
	if got := request(http.MethodPut, "/api/settings/appearance", `{"backgroundUrl":"https://example.com/bg.jpg","backgroundBlur":12}`, false); got.Code != 401 {
		t.Fatalf("anonymous update: %d", got.Code)
	}
	if got := request(http.MethodPut, "/api/settings/appearance", `{"backgroundUrl":"javascript:alert(1)","backgroundBlur":12}`, true); got.Code != 400 {
		t.Fatalf("unsafe URL: %d", got.Code)
	}
	if got := request(http.MethodPut, "/api/settings/appearance", `{"backgroundUrl":"https://example.com/bg.jpg","backgroundBlur":41}`, true); got.Code != 400 {
		t.Fatalf("invalid blur: %d", got.Code)
	}
	if got := request(http.MethodPut, "/api/settings/appearance", `{"backgroundUrl":"https://example.com/bg.jpg","backgroundBlur":12}`, true); got.Code != 200 {
		t.Fatalf("update: %d %s", got.Code, got.Body.String())
	}
	public := request(http.MethodGet, "/api/settings/public", "", false)
	if public.Code != 200 || !strings.Contains(public.Body.String(), `"backgroundBlur":12`) || !strings.Contains(public.Body.String(), `"backgroundUrl":"https://example.com/bg.jpg"`) || !strings.Contains(public.Body.String(), `"announcement":"keep me"`) {
		t.Fatalf("public settings: %s", public.Body.String())
	}
	if got := request(http.MethodPut, "/api/settings/appearance", `{"backgroundUrl":"","backgroundBlur":0}`, true); got.Code != 200 {
		t.Fatalf("reset: %d %s", got.Code, got.Body.String())
	}
}

func TestUploadListAndServeImage(t *testing.T) {
	a := testApp(t)
	login := httptest.NewRecorder()
	a.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var auth struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Data.Token == "" {
		t.Fatal("missing login token")
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, _ := w.CreateFormFile("file", "test.png")
	f.Write(tinyPNG(t))
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload/private", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+auth.Data.Token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var uploaded struct {
		Data struct {
			URL string `json:"url"`
			ID  string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &uploaded)
	if !strings.HasPrefix(uploaded.Data.URL, "/i/") {
		t.Fatalf("bad url: %s", uploaded.Data.URL)
	}
	img := httptest.NewRecorder()
	a.Handler().ServeHTTP(img, httptest.NewRequest(http.MethodGet, uploaded.Data.URL, nil))
	if img.Code != 200 || !bytes.Equal(img.Body.Bytes(), tinyPNG(t)) {
		t.Fatalf("image: %d", img.Code)
	}
	list := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/images?page=1&limit=20", nil)
	req.Header.Set("Authorization", "Bearer "+auth.Data.Token)
	a.Handler().ServeHTTP(list, req)
	if list.Code != 200 || !strings.Contains(list.Body.String(), uploaded.Data.ID) {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
}

func TestUploadBurstWaitsForFreeSlot(t *testing.T) {
	a := testApp(t)
	if _, err := a.DB.Exec(`INSERT INTO apikeys(id,key,name,enabled,is_default,created_at) VALUES('burst','sk-burst','burst',1,0,'')`); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "burst.png")
	if err != nil {
		t.Fatal(err)
	}
	file.Write(tinyPNG(t))
	form.Close()
	releases := make([]func(), 0, 4)
	for range 4 {
		release, err := a.uploadScheduler.Acquire(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "saturated")
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	req := httptest.NewRequest(http.MethodPost, "/api/upload/private", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-API-Key", "sk-burst")
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		result <- rec
	}()
	time.Sleep(20 * time.Millisecond)
	releases[0]()
	select {
	case rec := <-result:
		if rec.Code != http.StatusOK {
			t.Fatalf("queued upload: %d %s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued upload did not resume")
	}
}

func TestFrontendEntryModuleIsServed(t *testing.T) {
	a := testApp(t)
	page := adminRequest(a, "", http.MethodGet, "/", "")
	match := regexp.MustCompile(`src="(/assets/[^"]+\.js)"`).FindStringSubmatch(page.Body.String())
	if len(match) != 2 {
		t.Fatalf("missing bundled frontend: %s", page.Body.String())
	}
	rec := adminRequest(a, "", http.MethodGet, match[1], "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("frontend module: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestMigrationPreservesLinksAndIsIdempotent(t *testing.T) {
	old := t.TempDir()
	newDir := t.TempDir()
	os.MkdirAll(filepath.Join(old, "db"), 0700)
	os.MkdirAll(filepath.Join(old, "uploads"), 0700)
	name := "123e4567-e89b-12d3-a456-426614174000.png"
	data := tinyPNG(t)
	os.WriteFile(filepath.Join(old, "uploads", name), data, 0600)
	images := `{"_id":"old-id","uuid":"123e4567-e89b-12d3-a456-426614174000","filename":"` + name + `","format":"png","originalName":"old.png","size":` + "123" + `,"uploadedByType":"public","uploadedAt":"2026-01-01T00:00:00.000Z","isDeleted":false}` + "\n"
	os.WriteFile(filepath.Join(old, "db", "images.db"), []byte(images), 0600)
	settings := `{"_id":"appearance","key":"appSettings","value":{"appName":"EasyImg","backgroundUrl":"/i/` + name + `","backgroundBlur":12,"announcement":{"enabled":false}}}` + "\n"
	os.WriteFile(filepath.Join(old, "db", "settings.db"), []byte(settings), 0600)
	a, err := New(Config{DataDir: newDir, AdminUsername: "admin", AdminPassword: "strong-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for i := 0; i < 2; i++ {
		if _, err := a.MigrateEasyImg(old); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+name, nil))
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), data) {
		t.Fatalf("migrated image: %d %s", rec.Code, rec.Body.String())
	}
	publicSettings := httptest.NewRecorder()
	a.Handler().ServeHTTP(publicSettings, httptest.NewRequest(http.MethodGet, "/api/settings/public", nil))
	if publicSettings.Code != 200 || !strings.Contains(publicSettings.Body.String(), `"backgroundBlur":12`) || !strings.Contains(publicSettings.Body.String(), `"backgroundUrl":"/i/`+name+`"`) {
		t.Fatalf("migrated appearance: %d %s", publicSettings.Code, publicSettings.Body.String())
	}
	var count int
	a.DB.QueryRow("SELECT count(*) FROM images").Scan(&count)
	if count != 1 {
		t.Fatalf("duplicates: %d", count)
	}
}

func TestMigrationPreservesLoginAndAPIKey(t *testing.T) {
	old := t.TempDir()
	os.MkdirAll(filepath.Join(old, "db"), 0700)
	os.WriteFile(filepath.Join(old, "db", "images.db"), nil, 0600)
	hash, err := bcrypt.GenerateFromPassword([]byte("migrated-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(old, "db", "users.db"), []byte(`{"_id":"user-1","username":"easyimg","password":"`+string(hash)+`"}`+"\n"), 0600)
	os.WriteFile(filepath.Join(old, "db", "apikeys.db"), []byte(`{"_id":"key-1","key":"sk-old-key","name":"old","enabled":true}`+"\n"), 0600)
	a, err := New(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.MigrateEasyImg(old); err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	a.Handler().ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"easyimg","password":"migrated-password"}`)))
	if login.Code != 200 {
		t.Fatalf("migrated login: %d %s", login.Code, login.Body.String())
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "photo.png")
	file.Write(tinyPNG(t))
	form.Close()
	req := httptest.NewRequest("POST", "/api/upload/private", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-API-Key", "sk-old-key")
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("migrated API key: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPublicUploadRateLimit(t *testing.T) {
	a := testApp(t)
	if err := a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576,"rateLimit":1}`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, _ := form.CreateFormFile("file", "photo.png")
		file.Write(tinyPNG(t))
		form.Close()
		req := httptest.NewRequest("POST", "/api/upload/public", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		want := 200
		if i == 1 {
			want = 429
		}
		if rec.Code != want {
			t.Fatalf("attempt %d: got %d want %d: %s", i+1, rec.Code, want, rec.Body.String())
		}
	}
}

func TestAdminCanChangePassword(t *testing.T) {
	a := testApp(t)
	login := httptest.NewRecorder()
	a.Handler().ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	var auth struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	json.Unmarshal(login.Body.Bytes(), &auth)
	req := httptest.NewRequest("PUT", "/api/admin/password", strings.NewReader(`{"oldPassword":"strong-test-password","newPassword":"new-strong-password"}`))
	req.Header.Set("Authorization", "Bearer "+auth.Data.Token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	old := httptest.NewRecorder()
	a.Handler().ServeHTTP(old, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	if old.Code != 401 {
		t.Fatalf("old password accepted: %d", old.Code)
	}
	fresh := httptest.NewRecorder()
	a.Handler().ServeHTTP(fresh, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"new-strong-password"}`)))
	if fresh.Code != 200 {
		t.Fatalf("new password rejected: %d %s", fresh.Code, fresh.Body.String())
	}
}

func TestCookieAuthenticatedMutationRejectsForeignOrigin(t *testing.T) {
	a := testApp(t)
	login := httptest.NewRecorder()
	a.Handler().ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	req := httptest.NewRequest("PUT", "/api/config/public", strings.NewReader(`{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576}`))
	req.Header.Set("Origin", "https://attacker.example")
	req.AddCookie(login.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("foreign origin accepted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMigrationPreservesBlacklistAndModerationTasks(t *testing.T) {
	old := t.TempDir()
	os.MkdirAll(filepath.Join(old, "db"), 0700)
	os.WriteFile(filepath.Join(old, "db", "images.db"), nil, 0600)
	os.WriteFile(filepath.Join(old, "db", "ip_blacklist.db"), []byte(`{"_id":"blocked-id","ip":"192.0.2.10","reason":"old rule"}`+"\n"), 0600)
	os.WriteFile(filepath.Join(old, "db", "moderation_tasks.db"), []byte(`{"_id":"task-id","imageId":"old-id","status":"completed","result":{"score":0.9}}`+"\n"), 0600)
	a := testApp(t)
	if _, err := a.MigrateEasyImg(old); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.DB.QueryRow(`SELECT count(*) FROM source_documents WHERE kind='moderation_tasks' AND id='task-id'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("moderation task lost: %d %v", count, err)
	}
	if err := a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576,"rateLimit":10}`)); err != nil {
		t.Fatal(err)
	}
	makeUpload := func() int {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, _ := form.CreateFormFile("file", "photo.png")
		file.Write(tinyPNG(t))
		form.Close()
		req := httptest.NewRequest("POST", "/api/upload/public", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if got := makeUpload(); got != 403 {
		t.Fatalf("imported blacklist ineffective: %d", got)
	}
	login := httptest.NewRecorder()
	a.Handler().ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	var auth struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	json.Unmarshal(login.Body.Bytes(), &auth)
	req := httptest.NewRequest("DELETE", "/api/blacklist/blocked-id", nil)
	req.Header.Set("Authorization", "Bearer "+auth.Data.Token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("remove blacklist: %d %s", rec.Code, rec.Body.String())
	}
	if got := makeUpload(); got != 200 {
		t.Fatalf("upload still blocked: %d", got)
	}
}

func TestTrustedProxyUsesForwardedIPForBlacklist(t *testing.T) {
	a, err := New(Config{DataDir: t.TempDir(), AdminUsername: "admin", AdminPassword: "strong-test-password", TrustProxy: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.DB.Exec(`INSERT INTO ip_blacklist(id,ip,reason,created_at) VALUES('id','192.0.2.10','blocked','')`)
	a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576,"rateLimit":10}`))
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "photo.png")
	file.Write(tinyPNG(t))
	form.Close()
	req := httptest.NewRequest("POST", "/api/upload/public", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-Forwarded-For", "192.0.2.10")
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("forwarded blacklisted IP accepted: %d", rec.Code)
	}
}

func TestUpdatingPublicUploadKeepsMigratedSettings(t *testing.T) {
	a := testApp(t)
	a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":false,"maxFileSize":1048576,"allowedFormats":["png"],"contentSafety":{"enabled":true,"provider":"legacy"}}`))
	login := httptest.NewRecorder()
	a.Handler().ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"strong-test-password"}`)))
	var auth struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	json.Unmarshal(login.Body.Bytes(), &auth)
	req := httptest.NewRequest("PUT", "/api/config/public", strings.NewReader(`{"enabled":true,"maxFileSize":2097152,"allowedFormats":["png"],"rateLimit":10}`))
	req.Header.Set("Authorization", "Bearer "+auth.Data.Token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var saved string
	a.DB.QueryRow(`SELECT value FROM settings WHERE key='publicApiConfig'`).Scan(&saved)
	if !strings.Contains(saved, `"provider":"legacy"`) {
		t.Fatalf("migrated config lost: %s", saved)
	}
}
