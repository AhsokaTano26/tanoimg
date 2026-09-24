package app

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type passkeyUser struct {
	ID, Name    string
	Credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte {
	sum := sha256.Sum256([]byte("tanoimg-user:" + u.ID))
	return sum[:]
}
func (u *passkeyUser) WebAuthnName() string                       { return u.Name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.Name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }
func (a *App) passkeyUser(id string) (*passkeyUser, error) {
	u := &passkeyUser{ID: id}
	if err := a.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&u.Name); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(`SELECT id,credential FROM auth_passkeys WHERE user_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, encrypted string
		if err = rows.Scan(&key, &encrypted); err != nil {
			return nil, err
		}
		raw, err := a.openAuth(encrypted, "passkey:"+key+":"+id)
		if err != nil {
			return nil, err
		}
		var cred webauthn.Credential
		if err = json.Unmarshal([]byte(raw), &cred); err != nil {
			return nil, err
		}
		if base64.RawURLEncoding.EncodeToString(cred.ID) != key {
			return nil, errors.New("credential ID mismatch")
		}
		u.Credentials = append(u.Credentials, cred)
	}
	return u, rows.Err()
}
func (a *App) requirePasskeys(w http.ResponseWriter) bool {
	if a.webAuthn == nil {
		fail(w, 503, "Passkey 未配置，请设置 TANOIMG_PUBLIC_URL 为站点 HTTPS 地址")
		return false
	}
	return true
}
func (a *App) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		authProof
		Name string `json:"name"`
	}
	if !authJSON(w, r, &body) {
		return
	}
	id, valid := a.reauthenticate(w, r, body.authProof)
	if !valid {
		return
	}
	if !a.requirePasskeys(w) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len(body.Name) == 0 || len(body.Name) > 100 {
		fail(w, 400, "请输入 1–100 字节的 Passkey 名称")
		return
	}
	u, err := a.passkeyUser(id)
	if err != nil {
		fail(w, 500, "读取凭证失败")
		return
	}
	if len(u.Credentials) >= 20 {
		fail(w, 400, "最多绑定 20 个 Passkey")
		return
	}
	excluded := []protocol.CredentialDescriptor{}
	for _, c := range u.Credentials {
		excluded = append(excluded, c.Descriptor())
	}
	options, session, err := a.webAuthn.BeginRegistration(u, webauthn.WithExclusions(excluded))
	if err != nil {
		fail(w, 500, "无法开始 Passkey 注册")
		return
	}
	challenge, err := a.newChallenge(id, "passkey-register", tokenHash(bearer(r)), registrationState{Session: *session, Name: body.Name})
	if err != nil {
		fail(w, 500, "无法保存注册请求")
		return
	}
	ok(w, map[string]any{"challenge": challenge, "options": options})
}

type registrationState struct {
	Session webauthn.SessionData
	Name    string
}
type passkeyFinishBody struct {
	Challenge  string          `json:"challenge"`
	Credential json.RawMessage `json:"credential"`
}

func credentialRequest(r *http.Request, raw []byte) *http.Request {
	clone := r.Clone(r.Context())
	clone.Body = io.NopCloser(bytes.NewReader(raw))
	return clone
}
func (a *App) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	id := a.userID(r)
	if id == "" {
		fail(w, 401, "请先登录")
		return
	}
	if !a.requirePasskeys(w) {
		return
	}
	var body passkeyFinishBody
	if !authJSON(w, r, &body) {
		return
	}
	var state registrationState
	user, err := a.readChallenge(body.Challenge, "passkey-register", tokenHash(bearer(r)), &state)
	if err != nil || user != id {
		authError(w, err)
		return
	}
	if err = a.consumeChallenge(body.Challenge); err != nil {
		authError(w, err)
		return
	}
	u, err := a.passkeyUser(id)
	if err != nil {
		fail(w, 500, "读取用户失败")
		return
	}
	cred, err := a.webAuthn.FinishRegistration(u, state.Session, credentialRequest(r, body.Credential))
	if err != nil {
		authError(w, err)
		return
	}
	key := base64.RawURLEncoding.EncodeToString(cred.ID)
	raw, err := json.Marshal(cred)
	if err != nil {
		fail(w, 500, "保存凭证失败")
		return
	}
	sealed, err := a.sealAuth(string(raw), "passkey:"+key+":"+id)
	if err != nil {
		fail(w, 500, "保存凭证失败")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "保存凭证失败")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO auth_passkeys(id,user_id,name,credential,created_at) VALUES(?,?,?,?,?)`, key, id, state.Name, sealed, now()); err != nil {
		fail(w, 409, "该 Passkey 已绑定或无法保存")
		return
	}
	if revokeOtherAuth(tx, id, bearer(r)) != nil || tx.Commit() != nil {
		fail(w, 500, "保存凭证失败")
		return
	}
	ok(w, nil)
}
func (a *App) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !a.requirePasskeys(w) {
		return
	}
	options, session, err := a.webAuthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		fail(w, 500, "无法开始 Passkey 登录")
		return
	}
	binding, err := a.authBinding(w, r)
	if err != nil {
		fail(w, 500, "无法开始登录")
		return
	}
	challenge, err := a.newChallenge("", "passkey-login", binding, session)
	if err != nil {
		fail(w, 500, "无法保存登录请求")
		return
	}
	ok(w, map[string]any{"challenge": challenge, "options": options})
}
func (a *App) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !a.requirePasskeys(w) {
		return
	}
	var body passkeyFinishBody
	if !authJSON(w, r, &body) {
		return
	}
	var session webauthn.SessionData
	_, err := a.readChallenge(body.Challenge, "passkey-login", browserBinding(r), &session)
	if err != nil {
		authError(w, err)
		return
	}
	if err = a.consumeChallenge(body.Challenge); err != nil {
		authError(w, err)
		return
	}
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		var id string
		if err := a.DB.QueryRow(`SELECT user_id FROM auth_passkeys WHERE id=?`, base64.RawURLEncoding.EncodeToString(rawID)).Scan(&id); err != nil {
			return nil, err
		}
		u, err := a.passkeyUser(id)
		if err != nil {
			return nil, err
		}
		if subtle.ConstantTimeCompare(userHandle, u.WebAuthnID()) != 1 {
			return nil, errors.New("user handle mismatch")
		}
		return u, nil
	}
	user, cred, err := a.webAuthn.FinishPasskeyLogin(handler, session, credentialRequest(r, body.Credential))
	if err != nil || cred == nil {
		authError(w, err)
		return
	}
	if cred.Authenticator.CloneWarning {
		fail(w, 401, "Passkey 计数异常，请使用其他登录方式")
		return
	}
	u, valid := user.(*passkeyUser)
	if !valid {
		fail(w, 401, "Passkey 用户无效")
		return
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		fail(w, 500, "更新凭证失败")
		return
	}
	key := base64.RawURLEncoding.EncodeToString(cred.ID)
	sealed, err := a.sealAuth(string(raw), "passkey:"+key+":"+u.ID)
	if err != nil {
		fail(w, 500, "更新凭证失败")
		return
	}
	if _, err = a.DB.Exec(`UPDATE auth_passkeys SET credential=?,last_used_at=? WHERE id=? AND user_id=?`, sealed, now(), key, u.ID); err != nil {
		fail(w, 500, "更新凭证失败")
		return
	}
	a.issueSession(w, r, u.ID)
}
func (a *App) passkeyDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var proof authProof
	if !authJSON(w, r, &proof) {
		return
	}
	id, valid := a.reauthenticate(w, r, proof)
	if !valid {
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "删除失败")
		return
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM auth_passkeys WHERE id=? AND user_id=?`, r.PathValue("id"), id)
	if err != nil {
		fail(w, 500, "删除失败")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		fail(w, 404, "Passkey 不存在")
		return
	}
	if revokeOtherAuth(tx, id, bearer(r)) != nil || tx.Commit() != nil {
		fail(w, 500, "删除失败")
		return
	}
	ok(w, nil)
}
