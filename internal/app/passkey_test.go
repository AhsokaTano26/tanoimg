package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func clientData(kind, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return b
}
func authenticatorBytes(flags byte, counter uint32) []byte {
	sum := sha256.Sum256([]byte("img.example"))
	out := append([]byte{}, sum[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, counter)
	return out
}
func registerTestKey(t *testing.T, a *App, token string) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	d := securityData(t, securityCall(a, "POST", "/api/admin/passkeys/register/begin", token, map[string]string{"password": "strong-test-password", "name": "Test device"}))
	opts := d["options"].(map[string]any)["publicKey"].(map[string]any)
	if opts["authenticatorSelection"].(map[string]any)["userVerification"] != "required" {
		t.Fatal("UV not required")
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	rand.Read(id)
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: private.X.FillBytes(make([]byte, 32)), -3: private.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	auth := authenticatorBytes(0x45, 0)
	auth = append(auth, make([]byte, 16)...)
	auth = binary.BigEndian.AppendUint16(auth, uint16(len(id)))
	auth = append(auth, id...)
	auth = append(auth, cose...)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "authData": auth, "attStmt": map[string]any{}})
	cd := clientData("webauthn.create", opts["challenge"].(string), "https://img.example")
	cred := map[string]any{"id": b64(id), "rawId": b64(id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(cd), "attestationObject": b64(att), "transports": []string{"internal"}}, "clientExtensionResults": map[string]any{}}
	w := securityCall(a, "POST", "/api/admin/passkeys/register/finish", token, map[string]any{"challenge": d["challenge"], "credential": cred})
	if w.Code != 200 {
		t.Fatalf("registration: %d %s", w.Code, w.Body.String())
	}
	return private, id
}
func testPasskeyApp(t *testing.T) *App {
	t.Helper()
	a, err := New(Config{DataDir: t.TempDir(), AdminUsername: "admin", AdminPassword: "strong-test-password", PublicURL: "https://img.example"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}
func passkeyAssertion(t *testing.T, a *App, key *ecdsa.PrivateKey, id []byte, challenge, origin string, flags byte, counter uint32) map[string]any {
	t.Helper()
	var userID string
	a.DB.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID)
	u := &passkeyUser{ID: userID}
	cd := clientData("webauthn.get", challenge, origin)
	auth := authenticatorBytes(flags, counter)
	h := sha256.Sum256(cd)
	signed := append(append([]byte{}, auth...), h[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": b64(id), "rawId": b64(id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(cd), "authenticatorData": b64(auth), "signature": b64(sig), "userHandle": b64(u.WebAuthnID())}, "clientExtensionResults": map[string]any{}}
}
func TestPasskeyRegisterLoginAndRejection(t *testing.T) {
	a := testPasskeyApp(t)
	token := adminToken(t, a)
	key, id := registerTestKey(t, a, token)
	// A verified Passkey remains an independent login method even with TOTP enabled.
	enableTestTOTP(t, a, token)
	for _, tc := range []struct {
		name, origin string
		flags        byte
		want         int
	}{{"valid", "https://img.example", 5, 200}, {"wrong origin", "https://evil.example", 5, 400}, {"no UV", "https://img.example", 1, 400}} {
		t.Run(tc.name, func(t *testing.T) {
			start := securityCall(a, "POST", "/api/auth/passkey/begin", "", map[string]any{})
			d := securityData(t, start)
			opts := d["options"].(map[string]any)["publicKey"].(map[string]any)
			cred := passkeyAssertion(t, a, key, id, opts["challenge"].(string), tc.origin, tc.flags, 1)
			body := map[string]any{"challenge": d["challenge"], "credential": cred}
			if w := securityCall(a, "POST", "/api/auth/passkey/finish", "", body); w.Code == 200 {
				t.Fatal("missing binding accepted")
			}
			w := securityCall(a, "POST", "/api/auth/passkey/finish", "", body, start.Result().Cookies()...)
			if w.Code != tc.want {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if tc.want == 200 && securityData(t, w)["token"] == nil {
				t.Fatal("no login session")
			}
			if w = securityCall(a, "POST", "/api/auth/passkey/finish", "", body, start.Result().Cookies()...); w.Code == 200 {
				t.Fatal("replayed assertion accepted")
			}
		})
	}
}
func TestPasskeyConfigAndDeletion(t *testing.T) {
	if a, err := New(Config{DataDir: t.TempDir(), PublicURL: "http://img.example"}); err == nil {
		a.Close()
		t.Fatal("insecure public origin accepted")
	}
	a := testPasskeyApp(t)
	token := adminToken(t, a)
	_, id := registerTestKey(t, a, token)
	if w := securityCall(a, "DELETE", "/api/admin/passkeys/"+b64(id), token, map[string]string{"password": "wrong"}); w.Code != http.StatusForbidden {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w := securityCall(a, "DELETE", "/api/admin/passkeys/"+b64(id), token, map[string]string{"password": "strong-test-password"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	status := securityData(t, securityCall(a, "GET", "/api/admin/security", token, nil))
	if len(status["passkeys"].([]any)) != 0 {
		t.Fatal("credential not removed")
	}
}
