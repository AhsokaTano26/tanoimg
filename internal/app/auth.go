package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func bearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	if c, err := r.Cookie("tanoimg_session"); err == nil {
		return c.Value
	}
	return ""
}

func (a *App) userID(r *http.Request) string {
	token := bearer(r)
	if token == "" {
		return ""
	}
	var id string
	if err := a.DB.QueryRow(`SELECT user_id FROM sessions WHERE token_hash=? AND expires_at>?`, tokenHash(token), time.Now().Unix()).Scan(&id); err != nil {
		return ""
	}
	return id
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if a.userID(r) != "" {
		return true
	}
	fail(w, http.StatusUnauthorized, "请先登录")
	return false
}

func (a *App) hasAPIKey(r *http.Request) bool {
	_, ok := a.resolveAPIKey(r, "")
	return ok
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		fail(w, 400, "无效请求")
		return
	}
	var id, hash string
	err := a.DB.QueryRow(`SELECT id,password FROM users WHERE username=?`, body.Username).Scan(&id, &hash)
	if err == sql.ErrNoRows || bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)) != nil {
		fail(w, 401, "用户名或密码错误")
		return
	}
	if err != nil {
		fail(w, 500, "登录失败")
		return
	}
	enabled, err := a.totpEnabled(id)
	if err != nil {
		fail(w, 500, "登录失败")
		return
	}
	if enabled {
		binding, err := a.authBinding(w, r)
		if err != nil {
			fail(w, 500, "登录失败")
			return
		}
		challenge, err := a.newChallenge(id, "totp-login", binding, nil)
		if err != nil {
			fail(w, 500, "登录失败")
			return
		}
		ok(w, map[string]any{"requiresTOTP": true, "challenge": challenge})
		return
	}
	a.issueSession(w, r, id)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if token := bearer(r); token != "" {
		a.DB.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash(token))
	}
	http.SetCookie(w, &http.Cookie{Name: "tanoimg_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	ok(w, nil)
}

func (a *App) verify(w http.ResponseWriter, r *http.Request) {
	id := a.userID(r)
	if id == "" {
		fail(w, 401, "请先登录")
		return
	}
	var username string
	if a.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&username) != nil {
		fail(w, 401, "请先登录")
		return
	}
	ok(w, map[string]any{"user": map[string]string{"username": username}})
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	id := a.userID(r)
	if id == "" {
		fail(w, http.StatusUnauthorized, "请先登录")
		return
	}
	var body struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
		Code        string `json:"code"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil || len(body.NewPassword) < 8 {
		fail(w, 400, "新密码至少需要 8 位")
		return
	}
	var oldHash string
	if err := a.DB.QueryRow(`SELECT password FROM users WHERE id=?`, id).Scan(&oldHash); err != nil {
		fail(w, 404, "用户不存在")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(body.OldPassword)) != nil {
		fail(w, 400, "旧密码错误")
		return
	}
	enabled, err := a.totpEnabled(id)
	if err != nil {
		fail(w, 500, "验证失败")
		return
	}
	if enabled && !a.useSecondFactor(id, body.Code) {
		fail(w, 403, "请输入有效且未使用的动态码或恢复码")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		fail(w, 500, "修改密码失败")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		fail(w, 500, "修改密码失败")
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password=? WHERE id=?`, string(newHash), id); err != nil {
		fail(w, 500, "修改密码失败")
		return
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		fail(w, 500, "修改密码失败")
		return
	}
	if _, err := tx.Exec(`DELETE FROM auth_challenges WHERE user_id=?`, id); err != nil {
		fail(w, 500, "修改密码失败")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "修改密码失败")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "tanoimg_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	ok(w, nil)
}

func (a *App) changeUsername(w http.ResponseWriter, r *http.Request) {
	id := a.userID(r)
	if id == "" {
		fail(w, 401, "请先登录")
		return
	}
	var body struct {
		Username string `json:"username"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		fail(w, 400, "无效请求")
		return
	}
	username := strings.TrimSpace(body.Username)
	if len(username) < 3 || len(username) > 64 {
		fail(w, 400, "用户名需要 3–64 位")
		return
	}
	if _, err := a.DB.Exec(`UPDATE users SET username=? WHERE id=?`, username, id); err != nil {
		fail(w, 400, "用户名已存在或无效")
		return
	}
	ok(w, map[string]string{"username": username, "token": bearer(r)})
}
