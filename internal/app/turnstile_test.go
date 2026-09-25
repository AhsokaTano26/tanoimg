package app

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func turnstileAdminToken(t *testing.T, a *App) string {
	t.Helper()
	var userID string
	if err := a.DB.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	token := "turnstile-test-session"
	if _, err := a.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, tokenHash(token), userID, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	return token
}

type turnstileRoundTrip func(*http.Request) (*http.Response, error)

func (f turnstileRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func turnstileRequest(a *App, token, method, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.registerTurnstileRoutes(mux)
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestTurnstileDefaultsOffAndNeverRevealsSecret(t *testing.T) {
	a := testApp(t)
	r := turnstileRequest(a, "", "GET", "/api/turnstile", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"enabled":false`) {
		t.Fatalf("public config: %d %s", r.Code, r.Body.String())
	}
	if !a.verifyPublicTurnstile(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/upload/public", nil)) {
		t.Fatal("disabled Turnstile rejected upload")
	}
	admin := turnstileAdminToken(t, a)
	if r := turnstileRequest(a, "", "GET", "/api/admin/turnstile", ""); r.Code != 401 {
		t.Fatalf("anonymous settings: %d", r.Code)
	}
	r = turnstileRequest(a, admin, "PUT", "/api/admin/turnstile", `{"enabled":true,"siteKey":"site-public","secretKey":"server-secret"}`)
	if r.Code != 200 || strings.Contains(r.Body.String(), "server-secret") {
		t.Fatalf("save leaked secret: %d %s", r.Code, r.Body.String())
	}
	for _, path := range []string{"/api/turnstile", "/api/admin/turnstile"} {
		r = turnstileRequest(a, admin, "GET", path, "")
		if r.Code != 200 || strings.Contains(r.Body.String(), "server-secret") {
			t.Fatalf("GET %s leaked secret: %d %s", path, r.Code, r.Body.String())
		}
	}
	var raw string
	if err := a.DB.QueryRow(`SELECT value FROM settings WHERE key='turnstileConfig'`).Scan(&raw); err != nil || strings.Contains(raw, "server-secret") {
		t.Fatalf("secret was not sealed at rest: %s %v", raw, err)
	}
}

func TestTurnstileVerifiesBothPublicUploadPathsAndFailsClosed(t *testing.T) {
	a := testApp(t)
	admin := turnstileAdminToken(t, a)
	if r := turnstileRequest(a, admin, "PUT", "/api/admin/turnstile", `{"enabled":true,"siteKey":"site-public","secretKey":"server-secret"}`); r.Code != 200 {
		t.Fatalf("config: %d %s", r.Code, r.Body.String())
	}
	calls := 0
	a.urlClient = &http.Client{Transport: turnstileRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" {
			t.Errorf("wrong Siteverify request: %s %s", r.Method, r.URL)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"secret":"server-secret"`) || !strings.Contains(string(body), `"response":"visitor-token"`) {
			t.Errorf("missing proof fields: %s", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"hostname":"example.com"}`)), Header: make(http.Header)}, nil
	})}
	for _, path := range []string{"/api/upload/public", "/api/upload/public/url"} {
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("X-Turnstile-Token", "visitor-token")
		w := httptest.NewRecorder()
		if !a.verifyPublicTurnstile(w, r) || w.Code != 200 {
			t.Fatalf("%s verification: %d %s", path, w.Code, w.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("expected two Siteverify calls, got %d", calls)
	}
	a.urlClient = &http.Client{Transport: turnstileRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":false,"error-codes":["timeout-or-duplicate"]}`)), Header: make(http.Header)}, nil
	})}
	replayed := httptest.NewRequest("POST", "/api/upload/public/url", nil)
	replayed.Header.Set("X-Turnstile-Token", "visitor-token")
	if w := httptest.NewRecorder(); a.verifyPublicTurnstile(w, replayed) || w.Code != 403 {
		t.Fatalf("replayed proof should fail: %d", w.Code)
	}
	if w := httptest.NewRecorder(); a.verifyPublicTurnstile(w, httptest.NewRequest("POST", "/api/upload/public", nil)) || w.Code != 403 {
		t.Fatalf("missing proof should fail: %d", w.Code)
	}
	a.urlClient = &http.Client{Transport: turnstileRoundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("network down") })}
	r := httptest.NewRequest("POST", "/api/upload/public", nil)
	r.Header.Set("X-Turnstile-Token", "visitor-token")
	w := httptest.NewRecorder()
	if a.verifyPublicTurnstile(w, r) || w.Code != 503 {
		t.Fatalf("network failure must fail closed: %d %s", w.Code, w.Body.String())
	}
}
