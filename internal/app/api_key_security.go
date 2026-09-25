package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultAPIKeyScopes = `["upload:file","upload:url","upload:public"]`

type apiKeyPrincipal struct {
	ID         string
	Name       string
	Scopes     []string
	Policy     string
	ExpiresAt  int64
	DailyCount int64
	DailyBytes int64
}

func (p apiKeyPrincipal) Allows(scope string) bool {
	if scope == "" {
		return true
	}
	allowed := false
	for _, existing := range p.Scopes {
		if existing == scope {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}
	switch p.Policy {
	case "", "standard":
		return true
	case "private-only", "small-private":
		return scope != "upload:public"
	case "file-only":
		return scope != "upload:url"
	default:
		return false
	}
}

// The site-wide upload policy remains an upper bound for all key presets.
func (p apiKeyPrincipal) MaxFileSize(siteLimit int64) int64 {
	if p.Policy == "small-private" && siteLimit > 10<<20 {
		return 10 << 20
	}
	return siteLimit
}

func (p apiKeyPrincipal) AllowsFormat(format string) bool {
	if p.Policy != "small-private" {
		return true
	}
	return formatAllowed([]string{"jpg", "jpeg", "png", "webp"}, format)
}

func validAPIKeyPolicy(policy string) bool {
	switch policy {
	case "standard", "private-only", "file-only", "small-private":
		return true
	}
	return false
}

func normalizeAPIKeyScopes(scopes []string) ([]string, bool) {
	if len(scopes) == 0 || len(scopes) > 3 {
		return nil, false
	}
	seen := make(map[string]bool, len(scopes))
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope != "upload:file" && scope != "upload:url" && scope != "upload:public" {
			return nil, false
		}
		if !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}
	return result, true
}

func (a *App) initAPIKeySecurity() error {
	rows, err := a.DB.Query(`PRAGMA table_info(apikeys)`)
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primary int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primary); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, column := range []string{
		"key_hash TEXT NOT NULL DEFAULT ''",
		"key_hint TEXT NOT NULL DEFAULT ''",
		"scopes TEXT NOT NULL DEFAULT '" + defaultAPIKeyScopes + "'",
		"policy TEXT NOT NULL DEFAULT 'standard'",
		"expires_at INTEGER NOT NULL DEFAULT 0",
		"last_used_at TEXT NOT NULL DEFAULT ''",
		"quota_daily_count INTEGER NOT NULL DEFAULT 0",
		"quota_daily_bytes INTEGER NOT NULL DEFAULT 0",
	} {
		name := strings.SplitN(column, " ", 2)[0]
		if !existing[name] {
			if _, err := a.DB.Exec(`ALTER TABLE apikeys ADD COLUMN ` + column); err != nil {
				return err
			}
		}
	}
	for _, q := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS apikeys_key_hash ON apikeys(key_hash) WHERE key_hash!=''`,
		`CREATE TABLE IF NOT EXISTS upload_daily_usage(subject_type TEXT NOT NULL,subject_id TEXT NOT NULL,day TEXT NOT NULL,upload_count INTEGER NOT NULL DEFAULT 0,upload_bytes INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(subject_type,subject_id,day))`,
		`CREATE TABLE IF NOT EXISTS upload_quota_reservations(id TEXT PRIMARY KEY,image_id TEXT NOT NULL,key_id TEXT NOT NULL,ip_hash TEXT NOT NULL,day TEXT NOT NULL,upload_bytes INTEGER NOT NULL,created_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS upload_quota_reservations_age ON upload_quota_reservations(created_at)`,
	} {
		if _, err := a.DB.Exec(q); err != nil {
			return err
		}
	}
	return a.recoverStaleUploadQuotaReservations()
}

func apiKeyRaw(r *http.Request) string {
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = r.URL.Query().Get("apiKey") // Kept for EasyImg client compatibility.
	}
	return key
}

func keyHint(raw string) string {
	if len(raw) <= 4 {
		return "••••"
	}
	return "••••" + raw[len(raw)-4:]
}

func (a *App) resolveAPIKey(r *http.Request, scope string) (apiKeyPrincipal, bool) {
	raw := apiKeyRaw(r)
	if raw == "" {
		return apiKeyPrincipal{}, false
	}
	hash := tokenHash(raw)
	var p apiKeyPrincipal
	var enabled bool
	var storedHash, scopesJSON, lastUsed string
	err := a.DB.QueryRow(`SELECT id,name,enabled,key_hash,scopes,policy,expires_at,quota_daily_count,quota_daily_bytes,last_used_at FROM apikeys WHERE key_hash=? OR (key_hash='' AND key=?) LIMIT 1`, hash, raw).
		Scan(&p.ID, &p.Name, &enabled, &storedHash, &scopesJSON, &p.Policy, &p.ExpiresAt, &p.DailyCount, &p.DailyBytes, &lastUsed)
	if err != nil || !enabled || p.ExpiresAt != 0 && p.ExpiresAt <= time.Now().Unix() {
		return apiKeyPrincipal{}, false
	}
	if json.Unmarshal([]byte(scopesJSON), &p.Scopes) != nil || !validAPIKeyPolicy(p.Policy) || !p.Allows(scope) {
		return apiKeyPrincipal{}, false
	}
	if storedHash == "" {
		// A successful use migrates an EasyImg plaintext key in place. Its raw value
		// remains valid through key_hash, while GET responses reveal only a hint.
		result, updateErr := a.DB.Exec(`UPDATE apikeys SET key_hash=?,key=?,key_hint=? WHERE id=? AND key_hash='' AND key=?`, hash, "__hashed__:"+p.ID, keyHint(raw), p.ID, raw)
		err = updateErr
		if err != nil {
			return apiKeyPrincipal{}, false
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return apiKeyPrincipal{}, false
		}
	}
	if used, err := time.Parse(time.RFC3339Nano, lastUsed); err != nil || time.Since(used) >= time.Minute {
		_, _ = a.DB.Exec(`UPDATE apikeys SET last_used_at=? WHERE id=?`, now(), p.ID)
	}
	return p, true
}

func newAPIKeySecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(value), nil
}

func parseAPIKeyExpiry(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0, err
	}
	return value.Unix(), nil
}

func apiKeyExpiry(value int64) string {
	if value == 0 {
		return ""
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

func validKeyQuota(count, bytes int64) bool {
	return count >= 0 && count <= 1_000_000_000 && bytes >= 0 && bytes <= 1<<40
}

func decodeKeyBody(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		fail(w, 400, "无效密钥设置")
		return false
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		fail(w, 400, "无效密钥设置")
		return false
	}
	return true
}

func (a *App) registerAPIKeySecurityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/apikeys", a.listSecureAPIKeys)
	mux.HandleFunc("POST /api/apikeys", a.createSecureAPIKey)
	mux.HandleFunc("PUT /api/apikeys/{id}", a.updateSecureAPIKey)
	mux.HandleFunc("DELETE /api/apikeys/{id}", a.deleteSecureAPIKey)
	mux.HandleFunc("GET /api/admin/upload-quota", a.getIPDailyQuota)
	mux.HandleFunc("PUT /api/admin/upload-quota", a.putIPDailyQuota)
}

func (a *App) listSecureAPIKeys(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	rows, err := a.DB.Query(`SELECT a.id,a.key,a.key_hint,a.name,a.enabled,a.is_default,a.created_at,a.scopes,a.policy,a.expires_at,a.last_used_at,a.quota_daily_count,a.quota_daily_bytes,coalesce(u.upload_count,0),coalesce(u.upload_bytes,0)
		FROM apikeys a LEFT JOIN upload_daily_usage u ON u.subject_type='key' AND u.subject_id=a.id AND u.day=? ORDER BY a.created_at,a.id`, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		fail(w, 500, "查询密钥失败")
		return
	}
	defer rows.Close()
	list := make([]map[string]any, 0)
	for rows.Next() {
		var id, stored, hint, name, created, scopesJSON, policy, used string
		var enabled, defaultKey bool
		var expiry, dailyCount, dailyBytes, usedTodayCount, usedTodayBytes int64
		if err := rows.Scan(&id, &stored, &hint, &name, &enabled, &defaultKey, &created, &scopesJSON, &policy, &expiry, &used, &dailyCount, &dailyBytes, &usedTodayCount, &usedTodayBytes); err != nil {
			fail(w, 500, "查询密钥失败")
			return
		}
		if hint == "" {
			hint = keyHint(stored)
		}
		var scopes []string
		if json.Unmarshal([]byte(scopesJSON), &scopes) != nil {
			scopes = nil
		}
		list = append(list, map[string]any{"id": id, "name": name, "enabled": enabled, "isDefault": defaultKey, "createdAt": created, "keyHint": hint, "scopes": scopes, "policy": policy, "expiresAt": apiKeyExpiry(expiry), "lastUsedAt": used, "dailyCount": dailyCount, "dailyBytes": dailyBytes, "usedTodayCount": usedTodayCount, "usedTodayBytes": usedTodayBytes})
	}
	if rows.Err() != nil {
		fail(w, 500, "查询密钥失败")
		return
	}
	ok(w, list)
}

func (a *App) createSecureAPIKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var body struct {
		Name       string   `json:"name"`
		Scopes     []string `json:"scopes"`
		Policy     string   `json:"policy"`
		ExpiresAt  string   `json:"expiresAt"`
		DailyCount int64    `json:"dailyCount"`
		DailyBytes int64    `json:"dailyBytes"`
	}
	if !decodeKeyBody(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len(body.Name) == 0 || len(body.Name) > 100 || !validKeyQuota(body.DailyCount, body.DailyBytes) {
		fail(w, 400, "密钥名称或配额无效")
		return
	}
	if body.Policy == "" {
		body.Policy = "standard"
	}
	if !validAPIKeyPolicy(body.Policy) {
		fail(w, 400, "未知密钥策略")
		return
	}
	if body.Scopes == nil {
		json.Unmarshal([]byte(defaultAPIKeyScopes), &body.Scopes)
	}
	scopes, valid := normalizeAPIKeyScopes(body.Scopes)
	if !valid {
		fail(w, 400, "无效密钥权限")
		return
	}
	expiry, err := parseAPIKeyExpiry(body.ExpiresAt)
	if err != nil {
		fail(w, 400, "到期时间无效")
		return
	}
	id, err := newID()
	if err != nil {
		fail(w, 500, "创建密钥失败")
		return
	}
	raw, err := newAPIKeySecret()
	if err != nil {
		fail(w, 500, "创建密钥失败")
		return
	}
	scopesJSON, _ := json.Marshal(scopes)
	_, err = a.DB.Exec(`INSERT INTO apikeys(id,key,key_hash,key_hint,name,enabled,is_default,created_at,scopes,policy,expires_at,quota_daily_count,quota_daily_bytes) VALUES(?,?,?,?,?,1,0,?,?,?,?,?,?)`, id, "__hashed__:"+id, tokenHash(raw), keyHint(raw), body.Name, now(), string(scopesJSON), body.Policy, expiry, body.DailyCount, body.DailyBytes)
	if err != nil {
		fail(w, 500, "创建密钥失败")
		return
	}
	ok(w, map[string]any{"id": id, "key": raw, "keyHint": keyHint(raw), "name": body.Name, "enabled": true, "scopes": scopes, "policy": body.Policy, "expiresAt": apiKeyExpiry(expiry), "dailyCount": body.DailyCount, "dailyBytes": body.DailyBytes})
}

func (a *App) updateSecureAPIKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var body struct {
		Name       *string   `json:"name"`
		Enabled    *bool     `json:"enabled"`
		Regenerate bool      `json:"regenerate"`
		Scopes     *[]string `json:"scopes"`
		Policy     *string   `json:"policy"`
		ExpiresAt  *string   `json:"expiresAt"`
		DailyCount *int64    `json:"dailyCount"`
		DailyBytes *int64    `json:"dailyBytes"`
	}
	if !decodeKeyBody(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	var name, scopesJSON, policy, created, hint, hash, stored string
	var enabled, defaultKey bool
	var expiry, dailyCount, dailyBytes int64
	err := a.DB.QueryRow(`SELECT name,enabled,is_default,created_at,scopes,policy,expires_at,quota_daily_count,quota_daily_bytes,key_hint,key_hash,key FROM apikeys WHERE id=?`, id).
		Scan(&name, &enabled, &defaultKey, &created, &scopesJSON, &policy, &expiry, &dailyCount, &dailyBytes, &hint, &hash, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "密钥不存在")
		return
	}
	if err != nil {
		fail(w, 500, "读取密钥失败")
		return
	}
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	if len(name) == 0 || len(name) > 100 {
		fail(w, 400, "密钥名称无效")
		return
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.Scopes != nil {
		scopes, valid := normalizeAPIKeyScopes(*body.Scopes)
		if !valid {
			fail(w, 400, "无效密钥权限")
			return
		}
		encoded, _ := json.Marshal(scopes)
		scopesJSON = string(encoded)
	}
	if body.Policy != nil {
		policy = *body.Policy
		if !validAPIKeyPolicy(policy) {
			fail(w, 400, "未知密钥策略")
			return
		}
	}
	if body.ExpiresAt != nil {
		expiry, err = parseAPIKeyExpiry(*body.ExpiresAt)
		if err != nil {
			fail(w, 400, "到期时间无效")
			return
		}
	}
	if body.DailyCount != nil {
		dailyCount = *body.DailyCount
	}
	if body.DailyBytes != nil {
		dailyBytes = *body.DailyBytes
	}
	if !validKeyQuota(dailyCount, dailyBytes) {
		fail(w, 400, "配额无效")
		return
	}
	var raw string
	if body.Regenerate {
		raw, err = newAPIKeySecret()
		if err != nil {
			fail(w, 500, "重置密钥失败")
			return
		}
		hash, hint, stored = tokenHash(raw), keyHint(raw), "__hashed__:"+id
	} else if hint == "" {
		hint = keyHint(stored)
	}
	_, err = a.DB.Exec(`UPDATE apikeys SET key=?,key_hash=?,key_hint=?,name=?,enabled=?,scopes=?,policy=?,expires_at=?,quota_daily_count=?,quota_daily_bytes=? WHERE id=?`, stored, hash, hint, name, enabled, scopesJSON, policy, expiry, dailyCount, dailyBytes, id)
	if err != nil {
		fail(w, 500, "更新密钥失败")
		return
	}
	var scopes []string
	json.Unmarshal([]byte(scopesJSON), &scopes)
	response := map[string]any{"id": id, "keyHint": hint, "name": name, "enabled": enabled, "isDefault": defaultKey, "createdAt": created, "scopes": scopes, "policy": policy, "expiresAt": apiKeyExpiry(expiry), "dailyCount": dailyCount, "dailyBytes": dailyBytes}
	if raw != "" {
		response["key"] = raw
	}
	ok(w, response)
}

func (a *App) deleteSecureAPIKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	result, err := a.DB.Exec(`DELETE FROM apikeys WHERE id=?`, r.PathValue("id"))
	if err != nil {
		fail(w, 500, "删除密钥失败")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "密钥不存在")
		return
	}
	ok(w, nil)
}
