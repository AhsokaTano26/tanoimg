package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/crypto/bcrypt"
)

func (a *App) initSecurity(publicURL string) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS auth_totp(user_id TEXT PRIMARY KEY,secret TEXT NOT NULL,last_step INTEGER NOT NULL DEFAULT -1)`,
		`CREATE TABLE IF NOT EXISTS auth_recovery(user_id TEXT NOT NULL,code_hash TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS auth_challenges(token_hash TEXT PRIMARY KEY,user_id TEXT NOT NULL,purpose TEXT NOT NULL,binding TEXT NOT NULL,payload TEXT NOT NULL,expires_at INTEGER NOT NULL,attempts INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS auth_challenge_expiry ON auth_challenges(expires_at)`,
		`CREATE TABLE IF NOT EXISTS auth_rates(key TEXT PRIMARY KEY,window_start INTEGER NOT NULL,count INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS auth_passkeys(id TEXT PRIMARY KEY,user_id TEXT NOT NULL,name TEXT NOT NULL,credential TEXT NOT NULL,created_at TEXT NOT NULL,last_used_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS auth_passkey_user ON auth_passkeys(user_id)`,
	} {
		if _, err := a.DB.Exec(q); err != nil {
			return err
		}
	}
	keyPath := filepath.Join(a.DataDir, "auth.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		var count int
		if err = a.DB.QueryRow(`SELECT (SELECT count(*) FROM auth_totp)+(SELECT count(*) FROM auth_passkeys)+(SELECT count(*) FROM auth_challenges)`).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return errors.New("auth.key missing: restore it together with the authentication database")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return err
		}
		f, e := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, err = f.Write(key)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else if err != nil {
		return err
	}
	if len(key) != 32 {
		return errors.New("invalid auth.key length")
	}
	if err = os.Chmod(keyPath, 0600); err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	a.authCipher, err = cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if publicURL == "" {
		return nil
	}
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "localhost")) {
		return errors.New("TANOIMG_PUBLIC_URL must be an HTTPS origin (http://localhost is allowed for development)")
	}
	a.publicURL = u.Scheme + "://" + u.Host
	a.webAuthn, err = webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "TanoImg", RPOrigins: []string{a.publicURL}, AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired}})
	return err
}
func randomAuthToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func (a *App) sealAuth(value, aad string) (string, error) {
	nonce := make([]byte, a.authCipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(a.authCipher.Seal(nonce, nonce, []byte(value), []byte(aad))), nil
}
func (a *App) openAuth(value, aad string) (string, error) {
	b, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(b) < a.authCipher.NonceSize() {
		return "", errors.New("invalid encrypted auth data")
	}
	n := a.authCipher.NonceSize()
	plain, err := a.authCipher.Open(nil, b[:n], b[n:], []byte(aad))
	return string(plain), err
}
func authJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(value); err != nil {
		fail(w, 400, "无效请求")
		return false
	}
	return true
}
func (a *App) secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.HasPrefix(a.publicURL, "https://") || (a.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}
func (a *App) authHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		a.authMu.Lock()
		defer a.authMu.Unlock()
		if r.Method != "GET" && !a.authRate(w, r) {
			return
		}
		handler(w, r)
	}
}
func (a *App) authRate(w http.ResponseWriter, r *http.Request) bool {
	now := time.Now().Unix()
	key := tokenHash(a.clientIP(r))
	_, err := a.DB.Exec(`INSERT INTO auth_rates(key,window_start,count) VALUES(?,?,1) ON CONFLICT(key) DO UPDATE SET count=CASE WHEN window_start<=? THEN 1 ELSE count+1 END,window_start=CASE WHEN window_start<=? THEN excluded.window_start ELSE window_start END`, key, now, now-300, now-300)
	if err != nil {
		fail(w, 503, "认证暂时不可用")
		return false
	}
	var count int
	if err = a.DB.QueryRow(`SELECT count FROM auth_rates WHERE key=?`, key).Scan(&count); err != nil {
		fail(w, 503, "认证暂时不可用")
		return false
	}
	a.DB.Exec(`DELETE FROM auth_rates WHERE window_start<?`, now-86400)
	if count > 60 {
		w.Header().Set("Retry-After", "300")
		fail(w, 429, "认证请求过于频繁，请稍后再试")
		return false
	}
	return true
}
func (a *App) authBinding(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie("tanoimg_auth_binding"); err == nil && len(c.Value) == 64 {
		return tokenHash(c.Value), nil
	}
	value, err := randomAuthToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: "tanoimg_auth_binding", Value: value, Path: "/api/", HttpOnly: true, Secure: a.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: 600})
	return tokenHash(value), nil
}
func browserBinding(r *http.Request) string {
	c, err := r.Cookie("tanoimg_auth_binding")
	if err != nil {
		return ""
	}
	return tokenHash(c.Value)
}
func (a *App) newChallenge(user, purpose, binding string, payload any) (string, error) {
	value, err := randomAuthToken()
	if err != nil {
		return "", err
	}
	hash := tokenHash(value)
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sealed, err := a.sealAuth(string(raw), "challenge:"+hash+":"+user+":"+purpose+":"+binding)
	if err != nil {
		return "", err
	}
	if _, err = a.DB.Exec(`DELETE FROM auth_challenges WHERE expires_at<=? OR (user_id=? AND purpose=? AND binding=?)`, time.Now().Unix(), user, purpose, binding); err != nil {
		return "", err
	}
	_, err = a.DB.Exec(`INSERT INTO auth_challenges(token_hash,user_id,purpose,binding,payload,expires_at) VALUES(?,?,?,?,?,?)`, hash, user, purpose, binding, sealed, time.Now().Add(5*time.Minute).Unix())
	return value, err
}
func (a *App) readChallenge(value, purpose, binding string, payload any) (string, error) {
	if value == "" || binding == "" {
		return "", errors.New("missing challenge")
	}
	hash := tokenHash(value)
	var user, sealed string
	// Attempts are consumed atomically even if the supplied proof is invalid.
	err := a.DB.QueryRow(`UPDATE auth_challenges SET attempts=attempts+1 WHERE token_hash=? AND purpose=? AND binding=? AND expires_at>? AND attempts<5 RETURNING user_id,payload`, hash, purpose, binding, time.Now().Unix()).Scan(&user, &sealed)
	if err != nil {
		return "", err
	}
	raw, err := a.openAuth(sealed, "challenge:"+hash+":"+user+":"+purpose+":"+binding)
	if err != nil {
		return "", err
	}
	err = json.Unmarshal([]byte(raw), payload)
	return user, err
}
func (a *App) consumeChallenge(value string) error {
	result, err := a.DB.Exec(`DELETE FROM auth_challenges WHERE token_hash=?`, tokenHash(value))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("challenge already used")
	}
	return nil
}
func (a *App) totpEnabled(id string) (bool, error) {
	var count int
	err := a.DB.QueryRow(`SELECT count(*) FROM auth_totp WHERE user_id=?`, id).Scan(&count)
	return count > 0, err
}

type authProof struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (a *App) reauthenticate(w http.ResponseWriter, r *http.Request, p authProof) (string, bool) {
	id := a.userID(r)
	if id == "" {
		fail(w, 401, "请先登录")
		return "", false
	}
	var hash string
	if err := a.DB.QueryRow(`SELECT password FROM users WHERE id=?`, id).Scan(&hash); err != nil {
		fail(w, 500, "验证失败")
		return "", false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(p.Password)) != nil {
		fail(w, 403, "当前密码错误")
		return "", false
	}
	enabled, err := a.totpEnabled(id)
	if err != nil {
		fail(w, 500, "验证失败")
		return "", false
	}
	if enabled && !a.useSecondFactor(id, p.Code) {
		fail(w, 403, "动态码或恢复码无效、已使用")
		return "", false
	}
	return id, true
}
func (a *App) issueSession(w http.ResponseWriter, r *http.Request, id string) {
	var username string
	if a.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&username) != nil {
		fail(w, 401, "登录失败")
		return
	}
	token, err := randomAuthToken()
	if err != nil {
		fail(w, 500, "登录失败")
		return
	}
	expires := time.Now().Add(7 * 24 * time.Hour)
	if _, err = a.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, tokenHash(token), id, expires.Unix()); err != nil {
		fail(w, 500, "登录失败")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "tanoimg_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.secureCookie(r), Expires: expires})
	a.enqueueNotification("login", "管理员登录", username+" 已登录", map[string]any{"username": username, "ip": a.clientIP(r)})
	ok(w, map[string]any{"token": token, "user": map[string]string{"username": username}})
}
func revokeOtherAuth(tx *sql.Tx, id, session string) error {
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash!=?`, id, tokenHash(session)); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM auth_challenges WHERE user_id=?`, id)
	return err
}
func (a *App) securityStatus(w http.ResponseWriter, r *http.Request) {
	id := a.userID(r)
	if id == "" {
		fail(w, 401, "请先登录")
		return
	}
	enabled, err := a.totpEnabled(id)
	if err != nil {
		fail(w, 500, "无法读取账户安全设置")
		return
	}
	var count int
	if a.DB.QueryRow(`SELECT count(*) FROM auth_recovery WHERE user_id=?`, id).Scan(&count) != nil {
		fail(w, 500, "读取恢复码状态失败")
		return
	}
	rows, err := a.DB.Query(`SELECT id,name,created_at,last_used_at FROM auth_passkeys WHERE user_id=? ORDER BY created_at`, id)
	if err != nil {
		fail(w, 500, "读取 Passkey 失败")
		return
	}
	defer rows.Close()
	keys := []map[string]string{}
	for rows.Next() {
		var key, name, created, used string
		if err = rows.Scan(&key, &name, &created, &used); err != nil {
			fail(w, 500, "读取 Passkey 失败")
			return
		}
		keys = append(keys, map[string]string{"id": key, "name": name, "createdAt": created, "lastUsedAt": used})
	}
	if rows.Err() != nil {
		fail(w, 500, "读取 Passkey 失败")
		return
	}
	ok(w, map[string]any{"totpEnabled": enabled, "recoveryRemaining": count, "passkeys": keys, "passkeyAvailable": a.webAuthn != nil, "publicURL": a.publicURL})
}
func authError(w http.ResponseWriter, err error) {
	fail(w, 400, "验证失败或已过期，请重新开始")
}
