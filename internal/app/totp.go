package app

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"image/png"
	"net/http"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func normalizeRecovery(code string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}
func recoveryHash(id, code string) string {
	return tokenHash("recovery:" + id + ":" + normalizeRecovery(code))
}
func totpStep(secret, code string) int64 {
	now := time.Now()
	step := now.Unix() / 30
	for _, offset := range []int64{0, -1, 1} {
		candidate := step + offset
		want, err := totp.GenerateCode(secret, time.Unix(candidate*30, 0))
		if err == nil && subtle.ConstantTimeCompare([]byte(code), []byte(want)) == 1 {
			return candidate
		}
	}
	return -1
}
func (a *App) useSecondFactor(id, code string) bool {
	code = strings.TrimSpace(code)
	if len(normalizeRecovery(code)) == 24 {
		result, err := a.DB.Exec(`DELETE FROM auth_recovery WHERE user_id=? AND code_hash=?`, id, recoveryHash(id, code))
		if err != nil {
			return false
		}
		n, _ := result.RowsAffected()
		return n == 1
	}
	if len(code) != 6 {
		return false
	}
	var encrypted string
	var previous int64
	if a.DB.QueryRow(`SELECT secret,last_step FROM auth_totp WHERE user_id=?`, id).Scan(&encrypted, &previous) != nil {
		return false
	}
	secret, err := a.openAuth(encrypted, "totp:"+id)
	if err != nil {
		return false
	}
	step := totpStep(secret, code)
	if step < 0 || step <= previous {
		return false
	}
	result, err := a.DB.Exec(`UPDATE auth_totp SET last_step=? WHERE user_id=? AND last_step<?`, step, id, step)
	if err != nil {
		return false
	}
	n, _ := result.RowsAffected()
	return n == 1
}
func newRecoveryCodes() ([]string, error) {
	codes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		v := hex.EncodeToString(b)
		codes[i] = v[:8] + "-" + v[8:16] + "-" + v[16:]
	}
	return codes, nil
}
func (a *App) totpSetup(w http.ResponseWriter, r *http.Request) {
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
	enabled, err := a.totpEnabled(id)
	if err != nil {
		fail(w, 500, "读取二次验证失败")
		return
	}
	if enabled {
		fail(w, 409, "TOTP 已启用，请先关闭后重新绑定")
		return
	}
	var username string
	if a.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&username) != nil {
		fail(w, 500, "账户读取失败")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "TanoImg", AccountName: username, SecretSize: 20, Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		fail(w, 500, "生成密钥失败")
		return
	}
	challenge, err := a.newChallenge(id, "totp-setup", tokenHash(bearer(r)), key.Secret())
	if err != nil {
		fail(w, 500, "生成验证请求失败")
		return
	}
	im, err := key.Image(256, 256)
	if err != nil {
		fail(w, 500, "生成二维码失败")
		return
	}
	var buf bytes.Buffer
	if png.Encode(&buf, im) != nil {
		fail(w, 500, "生成二维码失败")
		return
	}
	ok(w, map[string]any{"challenge": challenge, "secret": key.Secret(), "uri": key.URL(), "qr": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())})
}
func (a *App) totpEnable(w http.ResponseWriter, r *http.Request) {
	id := a.userID(r)
	if id == "" {
		fail(w, 401, "请先登录")
		return
	}
	var body struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !authJSON(w, r, &body) {
		return
	}
	var secret string
	user, err := a.readChallenge(body.Challenge, "totp-setup", tokenHash(bearer(r)), &secret)
	if err != nil || user != id {
		authError(w, err)
		return
	}
	step := totpStep(secret, strings.TrimSpace(body.Code))
	if step < 0 {
		fail(w, 400, "动态码错误，请检查设备时间")
		return
	}
	encrypted, err := a.sealAuth(secret, "totp:"+id)
	if err != nil {
		fail(w, 500, "保存二次验证失败")
		return
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		fail(w, 500, "生成恢复码失败")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "保存二次验证失败")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO auth_totp(user_id,secret,last_step) VALUES(?,?,?)`, id, encrypted, step); err != nil {
		fail(w, 409, "TOTP 已启用")
		return
	}
	for _, code := range codes {
		if _, err = tx.Exec(`INSERT INTO auth_recovery(user_id,code_hash) VALUES(?,?)`, id, recoveryHash(id, code)); err != nil {
			fail(w, 500, "保存恢复码失败")
			return
		}
	}
	if err = revokeOtherAuth(tx, id, bearer(r)); err != nil {
		fail(w, 500, "撤销旧会话失败")
		return
	}
	if tx.Commit() != nil {
		fail(w, 500, "保存二次验证失败")
		return
	}
	ok(w, map[string]any{"recoveryCodes": codes})
}
func (a *App) totpLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !authJSON(w, r, &body) {
		return
	}
	var payload any
	id, err := a.readChallenge(body.Challenge, "totp-login", browserBinding(r), &payload)
	if err != nil {
		authError(w, err)
		return
	}
	if !a.useSecondFactor(id, body.Code) {
		fail(w, 401, "动态码或恢复码无效、已使用")
		return
	}
	if err = a.consumeChallenge(body.Challenge); err != nil {
		authError(w, err)
		return
	}
	a.issueSession(w, r, id)
}
func (a *App) totpDisable(w http.ResponseWriter, r *http.Request) {
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
		fail(w, 500, "关闭失败")
		return
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM auth_totp WHERE user_id=?`, `DELETE FROM auth_recovery WHERE user_id=?`} {
		if _, err = tx.Exec(q, id); err != nil {
			fail(w, 500, "关闭失败")
			return
		}
	}
	if revokeOtherAuth(tx, id, bearer(r)) != nil || tx.Commit() != nil {
		fail(w, 500, "关闭失败")
		return
	}
	ok(w, nil)
}
func (a *App) rotateRecovery(w http.ResponseWriter, r *http.Request) {
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
	enabled, err := a.totpEnabled(id)
	if err != nil || !enabled {
		fail(w, 400, "请先启用 TOTP")
		return
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		fail(w, 500, "生成恢复码失败")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "保存恢复码失败")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM auth_recovery WHERE user_id=?`, id); err != nil {
		fail(w, 500, "保存恢复码失败")
		return
	}
	for _, code := range codes {
		if _, err = tx.Exec(`INSERT INTO auth_recovery(user_id,code_hash) VALUES(?,?)`, id, recoveryHash(id, code)); err != nil {
			fail(w, 500, "保存恢复码失败")
			return
		}
	}
	if revokeOtherAuth(tx, id, bearer(r)) != nil || tx.Commit() != nil {
		fail(w, 500, "保存恢复码失败")
		return
	}
	ok(w, map[string]any{"recoveryCodes": codes})
}
