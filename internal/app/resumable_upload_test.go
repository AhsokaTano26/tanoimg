package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type resumableEnvelope struct {
	Data struct {
		ID     string `json:"id"`
		Offset int64  `json:"offset"`
		Image  Image  `json:"image"`
	} `json:"data"`
}

func resumableRequest(a *App, token, method, path string, body []byte) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.registerResumableUploadRoutes(mux)
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func resumableKeyRequest(a *App, key, method, path string, body []byte) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.registerResumableUploadRoutes(mux)
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func resumableData(t *testing.T, r *httptest.ResponseRecorder) resumableEnvelope {
	t.Helper()
	var value resumableEnvelope
	if err := json.Unmarshal(r.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", r.Body.String(), err)
	}
	return value
}

func TestResumableUploadPersistsOffsetsAndFinalizesNormalImage(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	png := tinyPNG(t)
	hash := sha256.Sum256(png)
	createBody := fmt.Sprintf(`{"filename":"sample.png","size":%d,"sha256":"%s","visibility":"private","alt":"resume"}`, len(png), hex.EncodeToString(hash[:]))
	if got := resumableRequest(a, "", "POST", "/api/upload/resumable", []byte(createBody)); got.Code != 401 {
		t.Fatalf("anonymous create: %d %s", got.Code, got.Body.String())
	}
	created := resumableRequest(a, token, "POST", "/api/upload/resumable", []byte(createBody))
	if created.Code != 200 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := resumableData(t, created).Data.ID
	if id == "" {
		t.Fatal("missing session ID")
	}
	first := resumableRequest(a, token, "PUT", "/api/upload/resumable/"+id+"/chunk?offset=0", png[:10])
	if first.Code != 200 || resumableData(t, first).Data.Offset != 10 {
		t.Fatalf("first chunk: %d %s", first.Code, first.Body.String())
	}
	if got := resumableRequest(a, token, "PUT", "/api/upload/resumable/"+id+"/chunk?offset=0", png[10:]); got.Code != 409 {
		t.Fatalf("stale offset: %d %s", got.Code, got.Body.String())
	}
	status := resumableRequest(a, token, "GET", "/api/upload/resumable/"+id, nil)
	if status.Code != 200 || resumableData(t, status).Data.Offset != 10 {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}
	second := resumableRequest(a, token, "PUT", "/api/upload/resumable/"+id+"/chunk?offset=10", png[10:])
	if second.Code != 200 || resumableData(t, second).Data.Offset != int64(len(png)) {
		t.Fatalf("second chunk: %d %s", second.Code, second.Body.String())
	}
	finished := resumableRequest(a, token, "POST", "/api/upload/resumable/"+id+"/finalize", nil)
	if finished.Code != 200 {
		t.Fatalf("finalize: %d %s", finished.Code, finished.Body.String())
	}
	im := resumableData(t, finished).Data.Image
	if im.ID == "" || im.Visibility != "private" || im.Alt != "resume" || im.Size != int64(len(png)) || im.Format != "png" {
		t.Fatalf("wrong image: %+v", im)
	}
	if data, err := os.ReadFile(filepath.Join(a.DataDir, "uploads", im.Filename)); err != nil || !bytes.Equal(data, png) {
		t.Fatalf("published file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.DataDir, "resumable", id, "part")); !os.IsNotExist(err) {
		t.Fatalf("completed session retains chunk file: %v", err)
	}
	if again := resumableRequest(a, token, "POST", "/api/upload/resumable/"+id+"/finalize", nil); again.Code != 200 || resumableData(t, again).Data.Image.ID != im.ID {
		t.Fatalf("idempotent finalize: %d %s", again.Code, again.Body.String())
	}
}

func TestResumableRejectsBadChecksumAndOtherOwner(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	png := tinyPNG(t)
	create := fmt.Sprintf(`{"filename":"x.png","size":%d,"sha256":"%s"}`, len(png), strings.Repeat("0", 64))
	r := resumableRequest(a, token, "POST", "/api/upload/resumable", []byte(create))
	if r.Code != 200 {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	id := resumableData(t, r).Data.ID
	if got := resumableRequest(a, "", "GET", "/api/upload/resumable/"+id, nil); got.Code != 401 {
		t.Fatalf("anonymous status: %d", got.Code)
	}
	if got := resumableRequest(a, token, "PUT", "/api/upload/resumable/"+id+"/chunk?offset=0", append(png, 1)); got.Code != 413 {
		t.Fatalf("oversized chunk: %d %s", got.Code, got.Body.String())
	}
	if got := resumableRequest(a, token, "GET", "/api/upload/resumable/"+id, nil); resumableData(t, got).Data.Offset != 0 {
		t.Fatalf("oversized chunk advanced offset: %s", got.Body.String())
	}
	if got := resumableRequest(a, token, "PUT", "/api/upload/resumable/"+id+"/chunk?offset=0", png); got.Code != 200 {
		t.Fatalf("chunk: %d %s", got.Code, got.Body.String())
	}
	if got := resumableRequest(a, token, "POST", "/api/upload/resumable/"+id+"/finalize", nil); got.Code != 422 {
		t.Fatalf("checksum: %d %s", got.Code, got.Body.String())
	}
	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("checksum published image: %d %v", count, err)
	}
}

func TestResumableCleanupExpiresDiskSession(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	body := fmt.Sprintf(`{"filename":"x.png","size":1,"sha256":"%s"}`, strings.Repeat("0", 64))
	r := resumableRequest(a, token, "POST", "/api/upload/resumable", []byte(body))
	if r.Code != 200 {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	id := resumableData(t, r).Data.ID
	path := filepath.Join(a.DataDir, "resumable", id)
	state, err := loadResumableState(path)
	if err != nil {
		t.Fatal(err)
	}
	state.UpdatedAt = time.Now().Add(-25 * time.Hour).Unix()
	if err := saveResumableState(path, state); err != nil {
		t.Fatal(err)
	}
	if err := a.cleanupResumableSessions(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired session remains: %v", err)
	}
}

func TestResumableAPIKeyOwnerAndRestart(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Config{DataDir: dir, AdminUsername: "admin", AdminPassword: "strong-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO apikeys(id,key,name,created_at) VALUES('key-one','legacy-one','One',''),('key-two','legacy-two','Two','')`); err != nil {
		t.Fatal(err)
	}
	png := tinyPNG(t)
	hash := sha256.Sum256(png)
	body := fmt.Sprintf(`{"filename":"key.png","size":%d,"sha256":"%s","visibility":"unlisted"}`, len(png), hex.EncodeToString(hash[:]))
	created := resumableKeyRequest(a, "legacy-one", "POST", "/api/upload/resumable", []byte(body))
	if created.Code != 200 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := resumableData(t, created).Data.ID
	if other := resumableKeyRequest(a, "legacy-two", "GET", "/api/upload/resumable/"+id, nil); other.Code != 404 {
		t.Fatalf("another key sees session: %d %s", other.Code, other.Body.String())
	}
	first := resumableKeyRequest(a, "legacy-one", "PUT", "/api/upload/resumable/"+id+"/chunk?offset=0", png[:8])
	if first.Code != 200 {
		t.Fatalf("first chunk: %d %s", first.Code, first.Body.String())
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := New(Config{DataDir: dir, AdminUsername: "admin", AdminPassword: "strong-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	status := resumableKeyRequest(b, "legacy-one", "GET", "/api/upload/resumable/"+id, nil)
	if status.Code != 200 || resumableData(t, status).Data.Offset != 8 {
		t.Fatalf("restart status: %d %s", status.Code, status.Body.String())
	}
	second := resumableKeyRequest(b, "legacy-one", "PUT", "/api/upload/resumable/"+id+"/chunk?offset=8", png[8:])
	if second.Code != 200 {
		t.Fatalf("second chunk: %d %s", second.Code, second.Body.String())
	}
	finished := resumableKeyRequest(b, "legacy-one", "POST", "/api/upload/resumable/"+id+"/finalize", nil)
	if finished.Code != 200 {
		t.Fatalf("finalize: %d %s", finished.Code, finished.Body.String())
	}
	im := resumableData(t, finished).Data.Image
	if im.APIKeyID != "" || im.ID == "" { // APIKeyID is intentionally hidden in JSON.
		t.Fatalf("unexpected image: %+v", im)
	}
	var keyID string
	if err := b.DB.QueryRow(`SELECT api_key_id FROM images WHERE id=?`, im.ID).Scan(&keyID); err != nil || keyID != "key-one" {
		t.Fatalf("key ownership: %q %v", keyID, err)
	}
}
