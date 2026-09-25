package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func apiKeyAdminToken(t *testing.T, a *App) string {
	t.Helper()
	var userID string
	if err := a.DB.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	token := "api-key-security-test-session"
	if _, err := a.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, tokenHash(token), userID, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	return token
}

func keySecurityRequest(a *App, token, method, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.registerAPIKeySecurityRoutes(mux)
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func apiKeyRequest(raw, scope string, a *App) (apiKeyPrincipal, bool) {
	r := httptest.NewRequest("POST", "/api/upload/private", nil)
	r.Header.Set("X-API-Key", raw)
	return a.resolveAPIKey(r, scope)
}

func TestAPIKeyLegacyLazyHashPreservesRights(t *testing.T) {
	a := testApp(t)
	if err := a.initAPIKeySecurity(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO apikeys(id,key,name,created_at) VALUES('legacy','sk-old-secret','Old Key','')`); err != nil {
		t.Fatal(err)
	}
	p, valid := apiKeyRequest("sk-old-secret", "upload:url", a)
	if !valid || p.ID != "legacy" || !p.Allows("upload:file") || !p.Allows("upload:public") || p.DailyCount != 0 || p.DailyBytes != 0 {
		t.Fatalf("legacy rights: %+v %t", p, valid)
	}
	var stored, hash, used string
	if err := a.DB.QueryRow(`SELECT key,key_hash,last_used_at FROM apikeys WHERE id='legacy'`).Scan(&stored, &hash, &used); err != nil {
		t.Fatal(err)
	}
	if stored == "sk-old-secret" || hash != tokenHash("sk-old-secret") || used == "" {
		t.Fatalf("legacy key not lazily hashed: key=%q hash=%q used=%q", stored, hash, used)
	}
	if _, valid := apiKeyRequest("sk-old-secret", "upload:file", a); !valid {
		t.Fatal("legacy raw key stopped working after migration")
	}
	r := httptest.NewRequest("POST", "/api/upload/private?apiKey=sk-old-secret", nil)
	if _, valid := a.resolveAPIKey(r, "upload:file"); !valid {
		t.Fatal("query API key compatibility lost")
	}
}

func TestAPIKeyNewSecretIsRevealedOnlyOnceAndScopeExpiryEnforced(t *testing.T) {
	a := testApp(t)
	if err := a.initAPIKeySecurity(); err != nil {
		t.Fatal(err)
	}
	token := apiKeyAdminToken(t, a)
	r := keySecurityRequest(a, token, "POST", "/api/apikeys", `{"name":"Scoped","scopes":["upload:file"],"policy":"private-only","dailyCount":2,"dailyBytes":10}`)
	if r.Code != 200 {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	var created struct {
		Data struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &created); err != nil || !strings.HasPrefix(created.Data.Key, "sk-") {
		t.Fatalf("new secret: %s %v", r.Body.String(), err)
	}
	var stored, hash string
	if err := a.DB.QueryRow(`SELECT key,key_hash FROM apikeys WHERE id=?`, created.Data.ID).Scan(&stored, &hash); err != nil || stored == created.Data.Key || hash != tokenHash(created.Data.Key) {
		t.Fatalf("new key must be hash-only: key=%q hash=%q err=%v", stored, hash, err)
	}
	list := keySecurityRequest(a, token, "GET", "/api/apikeys", "")
	if list.Code != 200 || strings.Contains(list.Body.String(), created.Data.Key) {
		t.Fatalf("list leaked secret: %d %s", list.Code, list.Body.String())
	}
	p, valid := apiKeyRequest(created.Data.Key, "upload:file", a)
	if !valid || p.DailyCount != 2 || p.DailyBytes != 10 || p.Allows("upload:url") || p.Allows("upload:public") {
		t.Fatalf("scope or policy: %+v %t", p, valid)
	}
	if _, valid := apiKeyRequest(created.Data.Key, "upload:url", a); valid {
		t.Fatal("URL scope was not enforced")
	}
	if got := keySecurityRequest(a, token, "PUT", "/api/apikeys/"+created.Data.ID, `{"expiresAt":"2000-01-01T00:00:00Z"}`); got.Code != 200 {
		t.Fatalf("expire: %d %s", got.Code, got.Body.String())
	}
	if _, valid := apiKeyRequest(created.Data.Key, "upload:file", a); valid {
		t.Fatal("expired key still authenticates")
	}
	updated := keySecurityRequest(a, token, "PUT", "/api/apikeys/"+created.Data.ID, `{"expiresAt":"","regenerate":true}`)
	if updated.Code != 200 {
		t.Fatalf("rotate: %d %s", updated.Code, updated.Body.String())
	}
	var rotated struct {
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &rotated); err != nil || rotated.Data.Key == "" || rotated.Data.Key == created.Data.Key {
		t.Fatalf("rotation secret: %s %v", updated.Body.String(), err)
	}
	if _, valid := apiKeyRequest(created.Data.Key, "upload:file", a); valid {
		t.Fatal("rotated old key still authenticates")
	}
	if _, valid := apiKeyRequest(rotated.Data.Key, "upload:file", a); !valid {
		t.Fatal("rotated key unusable")
	}
}

func TestAPIKeyDailyQuotaReservationRaceAndRollback(t *testing.T) {
	a := testApp(t)
	if err := a.initAPIKeySecurity(); err != nil {
		t.Fatal(err)
	}
	p := apiKeyPrincipal{ID: "key1", DailyCount: 1, DailyBytes: 8}
	if err := a.setSetting("uploadIPDailyQuota", json.RawMessage(`{"dailyCount":0,"dailyBytes":0}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.reserveUploadQuota(context.Background(), p, "192.0.2.1", "too-large", 9); !errors.Is(err, errDailyQuotaExceeded) {
		t.Fatalf("initial byte quota ignored: %v", err)
	}
	const racers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := make([]string, 0, 1)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := a.reserveUploadQuota(context.Background(), p, "192.0.2.1", "image-"+string(rune('a'+i)), 5)
			if err == nil {
				mu.Lock()
				winners = append(winners, id)
				mu.Unlock()
			} else if !errors.Is(err, errDailyQuotaExceeded) {
				t.Errorf("unexpected quota error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if len(winners) != 1 {
		t.Fatalf("atomic count quota accepted %d uploads", len(winners))
	}
	if err := a.finishUploadQuota(context.Background(), winners[0], false); err != nil {
		t.Fatal(err)
	}
	id, err := a.reserveUploadQuota(context.Background(), p, "192.0.2.1", "next-image", 8)
	if err != nil {
		t.Fatalf("rollback did not restore quota: %v", err)
	}
	if err := a.finishUploadQuota(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.reserveUploadQuota(context.Background(), p, "192.0.2.1", "one-more", 1); !errors.Is(err, errDailyQuotaExceeded) {
		t.Fatalf("committed count quota ignored: %v", err)
	}
	if err := a.setSetting("uploadIPDailyQuota", json.RawMessage(`{"dailyCount":1,"dailyBytes":8}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.reserveUploadQuota(context.Background(), apiKeyPrincipal{ID: "key2"}, "192.0.2.1", "other-key", 1); !errors.Is(err, errDailyQuotaExceeded) {
		t.Fatalf("IP quota ignored across keys: %v", err)
	}
	if _, err := a.reserveUploadQuota(context.Background(), apiKeyPrincipal{ID: "key2"}, "192.0.2.2", "oversize-ip", 9); !errors.Is(err, errDailyQuotaExceeded) {
		t.Fatalf("initial IP byte quota ignored: %v", err)
	}
}

func TestAPIKeyStaleReservationRecoveryKeepsCompletedUpload(t *testing.T) {
	a := testApp(t)
	if err := a.initAPIKeySecurity(); err != nil {
		t.Fatal(err)
	}
	p := apiKeyPrincipal{ID: "key-recovery", DailyCount: 1, DailyBytes: 10}
	id, err := a.reserveUploadQuota(context.Background(), p, "192.0.2.10", "abandoned-image", 6)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`UPDATE upload_quota_reservations SET created_at=? WHERE id=?`, time.Now().Add(-2*time.Hour).Unix(), id); err != nil {
		t.Fatal(err)
	}
	if err := a.recoverStaleUploadQuotaReservations(); err != nil {
		t.Fatal(err)
	}
	completed, err := a.reserveUploadQuota(context.Background(), p, "192.0.2.10", "completed-image", 7)
	if err != nil {
		t.Fatalf("abandoned claim was not released: %v", err)
	}
	imageID, _ := newID()
	if err := a.saveImage(Image{ID: "completed-image", UUID: imageID, Filename: imageID + ".png", Format: "png", Size: 7, UploadedAt: now(), UpdatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`UPDATE upload_quota_reservations SET created_at=? WHERE id=?`, time.Now().Add(-2*time.Hour).Unix(), completed); err != nil {
		t.Fatal(err)
	}
	if err := a.recoverStaleUploadQuotaReservations(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.reserveUploadQuota(context.Background(), p, "192.0.2.10", "third-image", 1); !errors.Is(err, errDailyQuotaExceeded) {
		t.Fatalf("completed upload lost its quota usage: %v", err)
	}
}
