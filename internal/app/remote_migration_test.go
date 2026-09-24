package app

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func migrationRequest(a *App, token, method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func migrationLogin(t *testing.T, a *App) string {
	t.Helper()
	r := migrationRequest(a, "", "POST", "/api/auth/login", []byte(`{"username":"admin","password":"strong-test-password"}`))
	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || result.Data.Token == "" {
		t.Fatal("login failed")
	}
	return result.Data.Token
}
func migrationArchive(t *testing.T, badPath string) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	files := map[string][]byte{"db/images.db": []byte(`{"_id":"old-image","uuid":"11111111-1111-1111-1111-111111111111","filename":"11111111-1111-1111-1111-111111111111.png","size":1,"isDeleted":true}` + "\n"), "db/users.db": []byte(`{"_id":"old-user","username":"legacy","password":"preserved-hash"}` + "\n"), "uploads/11111111-1111-1111-1111-111111111111.png": {1}}
	if badPath != "" {
		files[badPath] = []byte("forbidden")
	}
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(data)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func beginMigration(t *testing.T, a *App, token string, archive []byte) string {
	t.Helper()
	sum := sha256.Sum256(archive)
	id := hex.EncodeToString(sum[:])
	body, _ := json.Marshal(map[string]any{"sha256": id, "size": len(archive)})
	r := migrationRequest(a, token, "POST", "/api/admin/migrations", body)
	if r.Code != 200 {
		t.Fatalf("begin: %d %s", r.Code, r.Body.String())
	}
	return id
}
func waitMigration(t *testing.T, a *App, token, id string) map[string]any {
	t.Helper()
	for range 500 {
		r := migrationRequest(a, token, "GET", "/api/admin/migrations/"+id, nil)
		var response struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal(r.Body.Bytes(), &response)
		if response.Data["phase"] == "ready" || response.Data["phase"] == "failed" {
			return response.Data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("migration did not finish")
	return nil
}
func TestRemoteMigrationResumeAndIsolation(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Config{DataDir: dir, AdminPassword: "strong-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	token := migrationLogin(t, a)
	archive := migrationArchive(t, "")
	id := beginMigration(t, a, token, archive)
	path := "/api/admin/migrations/" + id
	if r := migrationRequest(a, "", "PUT", path+"/archive?offset=0", archive); r.Code != 401 {
		t.Fatalf("unauthenticated write: %d", r.Code)
	}
	a.DB.Exec(`INSERT INTO apikeys(id,key,name,created_at) VALUES('k','sk-test','test','')`)
	r := httptest.NewRequest("PUT", path+"/archive?offset=0", bytes.NewReader(archive))
	r.Header.Set("X-API-Key", "sk-test")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("API key must not authorize migration: %d", w.Code)
	}
	half := len(archive) / 2
	if r := migrationRequest(a, token, "PUT", path+"/archive?offset=0", archive[:half]); r.Code != 200 {
		t.Fatalf("chunk: %s", r.Body.String())
	}
	if r := migrationRequest(a, token, "PUT", path+"/archive?offset=0", archive[:half]); r.Code != 409 {
		t.Fatal("duplicate offset must conflict")
	}
	if r := migrationRequest(a, token, "POST", path+"/finalize", nil); r.Code != 409 {
		t.Fatal("incomplete archive accepted")
	}
	a.Close()
	a, err = New(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if r := migrationRequest(a, token, "PUT", path+"/archive?offset="+strconv.Itoa(half), archive[half:]); r.Code != 200 {
		t.Fatalf("resume: %s", r.Body.String())
	}
	if r := migrationRequest(a, token, "POST", path+"/finalize", nil); r.Code != 202 {
		t.Fatalf("finalize: %d %s", r.Code, r.Body.String())
	}
	result := waitMigration(t, a, token, id)
	if result["phase"] != "ready" {
		t.Fatalf("prepare: %v", result)
	}
	ready := result["dataDir"].(string)
	var count int
	a.DB.QueryRow(`SELECT count(*) FROM images`).Scan(&count)
	if count != 0 {
		t.Fatal("live images modified")
	}
	var username string
	a.DB.QueryRow(`SELECT username FROM users`).Scan(&username)
	if username != "admin" {
		t.Fatal("live account replaced")
	}
	restored, err := New(Config{DataDir: ready})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var hash string
	restored.DB.QueryRow(`SELECT password FROM users WHERE id='old-user'`).Scan(&hash)
	if hash != "preserved-hash" {
		t.Fatal("old account not preserved")
	}
	var deleted bool
	restored.DB.QueryRow(`SELECT is_deleted FROM images WHERE id='old-image'`).Scan(&deleted)
	if !deleted {
		t.Fatal("recycle state lost")
	}
	b, err := os.ReadFile(filepath.Join(ready, "uploads", "11111111-1111-1111-1111-111111111111.png"))
	if err != nil || !bytes.Equal(b, []byte{1}) {
		t.Fatal("image missing")
	}
	if r := migrationRequest(a, token, "POST", path+"/finalize", nil); r.Code != 200 {
		t.Fatal("ready finalize must be idempotent")
	}
	if r := migrationRequest(a, token, "DELETE", path, nil); r.Code != 409 {
		t.Fatal("ready dataset must not be deleted")
	}
}
func TestRemoteMigrationRejectsUnsafeArchiveAndDigest(t *testing.T) {
	for _, bad := range []string{"../escape", "db/../../escape", "uploads/unknown.txt", "digest"} {
		t.Run(bad, func(t *testing.T) {
			a := testApp(t)
			token := migrationLogin(t, a)
			name := bad
			if bad == "digest" {
				name = ""
			}
			archive := migrationArchive(t, name)
			id := beginMigration(t, a, token, archive)
			path := "/api/admin/migrations/" + id
			if bad == "digest" {
				archive[0] ^= 1
			}
			if r := migrationRequest(a, token, "PUT", path+"/archive?offset=0", archive); r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			migrationRequest(a, token, "POST", path+"/finalize", nil)
			if state := waitMigration(t, a, token, id); state["phase"] != "failed" {
				t.Fatalf("unsafe archive accepted: %v", state)
			}
		})
	}
}

func TestRemoteMigrationCapabilitiesOwnerAndChunkRollback(t *testing.T) {
	a := testApp(t)
	token := migrationLogin(t, a)
	if r := migrationRequest(a, token, "GET", "/api/admin/migrations", nil); r.Code != 200 {
		t.Fatalf("capabilities: %d", r.Code)
	}
	archive := migrationArchive(t, "")
	id := beginMigration(t, a, token, archive)
	path := "/api/admin/migrations/" + id
	a.DB.Exec(`INSERT INTO users(id,username,password) VALUES('other','other','unused')`)
	a.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,'other',?)`, tokenHash("other-session"), time.Now().Add(time.Hour).Unix())
	for _, method := range []string{"GET", "DELETE"} {
		if r := migrationRequest(a, "other-session", method, path, nil); r.Code != 403 {
			t.Fatalf("other owner %s: %d", method, r.Code)
		}
	}
	r := httptest.NewRequest("PUT", path+"/archive?offset=0", bytes.NewReader(archive[:100]))
	r.ContentLength = 200
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("truncated chunk: %d", w.Code)
	}
	if st, err := os.Stat(filepath.Join(a.DataDir, "imports", id, "archive.tar")); err != nil || st.Size() != 0 {
		t.Fatal("partial chunk was not rolled back")
	}
	if r := migrationRequest(a, token, "PUT", path+"/archive?offset=0", append(archive, 1)); r.Code != 400 {
		t.Fatal("oversize archive accepted")
	}
	if r := migrationRequest(a, token, "DELETE", path, nil); r.Code != 200 {
		t.Fatalf("reset: %d", r.Code)
	}
	if r := migrationRequest(a, token, "GET", path, nil); r.Code != 404 {
		t.Fatal("reset did not remove incomplete import")
	}
}

func TestMigrationArchiveRejectsLinks(t *testing.T) {
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink} {
		var data bytes.Buffer
		w := tar.NewWriter(&data)
		w.WriteHeader(&tar.Header{Name: "uploads/11111111-1111-1111-1111-111111111111.png", Typeflag: kind, Linkname: "/etc/passwd"})
		w.Close()
		archive := filepath.Join(t.TempDir(), "archive.tar")
		os.WriteFile(archive, data.Bytes(), 0600)
		if err := extractMigrationArchive(archive, t.TempDir()); err == nil {
			t.Fatal("archive link accepted")
		}
	}
}
