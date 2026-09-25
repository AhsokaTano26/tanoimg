package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHashedAPIKeyUploadIdempotencyQuotaAndScope(t *testing.T) {
	a := testApp(t)
	admin := apiKeyAdminToken(t, a)
	created := keySecurityRequest(a, admin, http.MethodPost, "/api/apikeys", `{"name":"limited","scopes":["upload:file"],"policy":"private-only","dailyCount":1}`)
	if created.Code != 200 {
		t.Fatalf("create key: %d %s", created.Code, created.Body.String())
	}
	var key struct {
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &key); err != nil || key.Data.Key == "" {
		t.Fatalf("decode key: %v %s", err, created.Body.String())
	}
	upload := func(path, idem string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, _ := form.CreateFormFile("file", "test.png")
		_, _ = file.Write(tinyPNG(t))
		_ = form.Close()
		req := httptest.NewRequest(http.MethodPost, path, &body)
		req.RemoteAddr = "192.0.2.41:12345"
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("X-API-Key", key.Data.Key)
		if idem != "" {
			req.Header.Set("Idempotency-Key", idem)
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}
	first := upload("/api/upload/private?visibility=private", "test-upload-unique-001")
	if first.Code != 200 {
		t.Fatalf("first upload: %d %s", first.Code, first.Body.String())
	}
	replay := upload("/api/upload/private?visibility=private", "test-upload-unique-001")
	if replay.Code != 200 {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	var firstImage, replayImage struct {
		Data Image `json:"data"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &firstImage)
	_ = json.Unmarshal(replay.Body.Bytes(), &replayImage)
	if firstImage.Data.ID == "" || firstImage.Data.ID != replayImage.Data.ID {
		t.Fatalf("idempotency made another image: %s %s", first.Body.String(), replay.Body.String())
	}
	if next := upload("/api/upload/private?visibility=private", "test-upload-unique-002"); next.Code != 429 || next.Header().Get("Retry-After") == "" {
		t.Fatalf("daily quota: %d %s", next.Code, next.Body.String())
	}
	if public := upload("/api/upload/private?visibility=public", ""); public.Code != 403 {
		t.Fatalf("public scope: %d %s", public.Code, public.Body.String())
	}
	urlReq := httptest.NewRequest(http.MethodPost, "/api/upload/url", strings.NewReader(`{"url":"https://example.com/a.png"}`))
	urlReq.Header.Set("X-API-Key", key.Data.Key)
	urlRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(urlRec, urlReq)
	if urlRec.Code != 401 {
		t.Fatalf("URL scope: %d %s", urlRec.Code, urlRec.Body.String())
	}
	var count int
	if err := a.DB.QueryRow(`SELECT count(*) FROM images`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("persisted images: %d %v", count, err)
	}
}
