package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func securityCall(a *App, method, path, token string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(b)))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func securityData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data
}
func enableTestTOTP(t *testing.T, a *App, token string) (string, []any) {
	t.Helper()
	d := securityData(t, securityCall(a, "POST", "/api/admin/totp/setup", token, map[string]string{"password": "strong-test-password"}))
	secret := d["secret"].(string)
	code, _ := totp.GenerateCode(secret, time.Now())
	enabled := securityData(t, securityCall(a, "POST", "/api/admin/totp/enable", token, map[string]string{"challenge": d["challenge"].(string), "code": code}))
	return secret, enabled["recoveryCodes"].([]any)
}
func TestTOTPLoginAndRecovery(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	secret, codes := enableTestTOTP(t, a, token)
	var stored string
	a.DB.QueryRow(`SELECT secret FROM auth_totp`).Scan(&stored)
	if strings.Contains(stored, secret) {
		t.Fatal("TOTP secret stored in plaintext")
	}
	w := securityCall(a, "POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": "strong-test-password"})
	d := securityData(t, w)
	if d["requiresTOTP"] != true || d["token"] != nil {
		t.Fatalf("password bypassed MFA: %v", d)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "tanoimg_session" {
			t.Fatal("session issued before MFA")
		}
	}
	body := map[string]string{"challenge": d["challenge"].(string), "code": codes[0].(string)}
	if r := securityCall(a, "POST", "/api/auth/totp", "", body); r.Code == 200 {
		t.Fatal("missing browser binding accepted")
	}
	verified := securityData(t, securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...))
	if verified["token"] == nil {
		t.Fatal("no session after MFA")
	}
	if r := securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...); r.Code == 200 {
		t.Fatal("challenge replay accepted")
	}
	w = securityCall(a, "POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": "strong-test-password"})
	d = securityData(t, w)
	body["challenge"] = d["challenge"].(string)
	if r := securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...); r.Code == 200 {
		t.Fatal("recovery code reused")
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	body["code"] = code
	if r := securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...); r.Code == 200 {
		t.Fatal("enrollment OTP reused")
	}
}
func TestSecurityEndpointsRequireReauthentication(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	for _, path := range []string{"/api/admin/totp/setup", "/api/admin/recovery-codes", "/api/admin/passkeys/register/begin"} {
		if w := securityCall(a, "POST", path, "", map[string]string{}); w.Code != 401 {
			t.Fatalf("guest %s: %d", path, w.Code)
		}
		if w := securityCall(a, "POST", path, token, map[string]string{"password": "wrong"}); w.Code == 200 {
			t.Fatalf("wrong password %s accepted", path)
		}
	}
}

func TestTOTPExpiryAttemptLimitAndPasswordChange(t *testing.T) {
	a := testApp(t)
	token := adminToken(t, a)
	_, codes := enableTestTOTP(t, a, token)
	login := func() (*httptest.ResponseRecorder, map[string]any) {
		w := securityCall(a, "POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": "strong-test-password"})
		return w, securityData(t, w)
	}
	w, d := login()
	body := map[string]string{"challenge": d["challenge"].(string), "code": "wrong"}
	for i := 0; i < 5; i++ {
		if r := securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...); r.Code == 200 {
			t.Fatal("bad code accepted")
		}
	}
	body["code"] = codes[0].(string)
	if r := securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...); r.Code == 200 {
		t.Fatal("attempt limit bypassed")
	}
	w, d = login()
	body["challenge"] = d["challenge"].(string)
	a.DB.Exec(`UPDATE auth_challenges SET expires_at=0`)
	if r := securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...); r.Code == 200 {
		t.Fatal("expired challenge accepted")
	}
	w, d = login()
	body["challenge"] = d["challenge"].(string)
	if r := securityCall(a, "PUT", "/api/admin/password", token, map[string]string{"oldPassword": "strong-test-password", "newPassword": "another-strong-password"}); r.Code != 403 {
		t.Fatal("password change bypassed TOTP", r.Code)
	}
	if r := securityCall(a, "PUT", "/api/admin/password", token, map[string]string{"oldPassword": "strong-test-password", "newPassword": "another-strong-password", "code": codes[1].(string)}); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := securityCall(a, "POST", "/api/auth/totp", "", body, w.Result().Cookies()...); r.Code == 200 {
		t.Fatal("old password proof survived password change")
	}
}
func TestTOTPRotateDisableAndPersistentKey(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Config{DataDir: dir, AdminUsername: "admin", AdminPassword: "strong-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	token := adminToken(t, a)
	_, codes := enableTestTOTP(t, a, token)
	a.Close()
	a, err = New(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	fresh := securityData(t, securityCall(a, "POST", "/api/admin/recovery-codes", token, map[string]string{"password": "strong-test-password", "code": codes[0].(string)}))["recoveryCodes"].([]any)
	if r := securityCall(a, "DELETE", "/api/admin/totp", token, map[string]string{"password": "strong-test-password", "code": codes[1].(string)}); r.Code == 200 {
		t.Fatal("old recovery codes survived rotation")
	}
	if r := securityCall(a, "DELETE", "/api/admin/totp", token, map[string]string{"password": "strong-test-password", "code": fresh[0].(string)}); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	d := securityData(t, securityCall(a, "POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": "strong-test-password"}))
	if d["token"] == nil {
		t.Fatal("password login not restored")
	}
}
