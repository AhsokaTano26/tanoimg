package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func uploadFixture(t *testing.T, a *App, target, token, ip string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	part, _ := form.CreateFormFile("file", "example.png")
	part.Write(tinyPNG(t))
	form.Close()
	req := httptest.NewRequest(http.MethodPost, target, &b)
	req.RemoteAddr = ip + ":1234"
	req.Header.Set("Content-Type", form.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAuthenticatedUploadVisibility(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	for _, visibility := range []string{"private", "public"} {
		got := uploadFixture(t, a, "/api/upload/private?visibility="+visibility, token, "192.0.2.1")
		if got.Code != 200 || !strings.Contains(got.Body.String(), `"uploadedByType":"`+visibility+`"`) || !strings.Contains(got.Body.String(), `"uploadedBy":"admin"`) {
			t.Fatalf("%s: %d %s", visibility, got.Code, got.Body.String())
		}
	}
	if got := uploadFixture(t, a, "/api/upload/private?visibility=public", "", "192.0.2.1"); got.Code != 401 {
		t.Fatalf("anonymous visibility bypass: %d", got.Code)
	}
	if got := uploadFixture(t, a, "/api/upload/private?visibility=invalid", token, "192.0.2.1"); got.Code != 400 {
		t.Fatalf("invalid visibility: %d", got.Code)
	}
	got := adminRequest(a, "", "GET", "/api/images", "")
	if !strings.Contains(got.Body.String(), `"total":1`) {
		t.Fatalf("public gallery: %s", got.Body.String())
	}
}

func TestAutoBanCountsRejectedRequestsAndUnblockResets(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576,"rateLimit":1,"autoBan":{"enabled":true,"windowMinutes":10,"maxAttempts":2}}`))
	for i, want := range []int{200, 429, 403, 403} {
		got := uploadFixture(t, a, "/api/upload/public", "", "192.0.2.2")
		if got.Code != want {
			t.Fatalf("attempt %d: %d want %d: %s", i, got.Code, want, got.Body.String())
		}
	}
	var id, reason string
	if err := a.DB.QueryRow("SELECT id,reason FROM ip_blacklist WHERE ip='192.0.2.2'").Scan(&id, &reason); err != nil || !strings.Contains(reason, "自动封禁") {
		t.Fatalf("ban: %s %v", reason, err)
	}
	if got := adminRequest(a, token, "DELETE", "/api/blacklist/"+id, ""); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	if got := uploadFixture(t, a, "/api/upload/public", "", "192.0.2.2"); got.Code != 200 {
		t.Fatalf("unblock: %d %s", got.Code, got.Body.String())
	}
	if got := uploadFixture(t, a, "/api/upload/private", token, "192.0.2.2"); got.Code != 200 {
		t.Fatalf("private exempt: %d", got.Code)
	}
}

func TestAutoBanWindowConcurrencyAndValidation(t *testing.T) {
	a := testApp(t)
	c := autoBanConfig{Enabled: true, WindowMinutes: 1, MaxAttempts: 4}
	a.DB.Exec("INSERT INTO public_upload_attempts VALUES(?,?,?)", "192.0.2.3", time.Now().Unix()-61, 99)
	banned, err := a.recordPublicAttempt(context.Background(), "192.0.2.3", c)
	if err != nil || banned {
		t.Fatalf("expired window: %v %v", banned, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.recordPublicAttempt(context.Background(), "192.0.2.4", c); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count int
	a.DB.QueryRow("SELECT count(*) FROM ip_blacklist WHERE ip='192.0.2.4'").Scan(&count)
	if count != 1 {
		t.Fatalf("concurrent bans: %d", count)
	}
	token := adminToken(t, a)
	for _, body := range []string{`{"autoBan":{"enabled":true,"windowMinutes":0,"maxAttempts":1}}`, `{"autoBan":{"enabled":true,"windowMinutes":5,"maxAttempts":0}}`, `{"allowedFormats":["exe"]}`} {
		if got := adminRequest(a, token, "PUT", "/api/config/public", body); got.Code != 400 {
			t.Fatalf("invalid config: %d %s", got.Code, got.Body.String())
		}
	}
}

func TestPublicURLUsesPublicPolicy(t *testing.T) {
	a := testApp(t)
	a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":true,"allowedFormats":["png"],"maxFileSize":1048576,"rateLimit":100,"autoBan":{"enabled":false}}`))
	calls := 0
	a.urlClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(tinyPNG(t)))}, nil
	})
	got := adminRequest(a, "", "POST", "/api/upload/public/url", `{"url":"https://images.example/photo.png"}`)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"uploadedByType":"public"`) {
		t.Fatalf("URL: %d %s", got.Code, got.Body.String())
	}
	got = adminRequest(a, "", "POST", "/api/upload/public/url", `{"url":"http://127.0.0.1/private"}`)
	if got.Code != 400 || calls != 1 {
		t.Fatalf("SSRF: %d calls=%d", got.Code, calls)
	}
	a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":true,"allowedFormats":["jpg"],"maxFileSize":1048576,"rateLimit":100}`))
	got = adminRequest(a, "", "POST", "/api/upload/public/url", `{"url":"https://images.example/photo.png"}`)
	if got.Code != 400 {
		t.Fatalf("format bypass: %d", got.Code)
	}
	a.setSetting("publicApiConfig", json.RawMessage(`{"enabled":false}`))
	got = adminRequest(a, "", "POST", "/api/upload/public/url", `{"url":"https://images.example/photo.png"}`)
	if got.Code != 403 || calls != 2 {
		t.Fatalf("disabled: %d calls=%d", got.Code, calls)
	}
}
