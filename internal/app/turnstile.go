package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type turnstileSettings struct {
	Enabled      bool   `json:"enabled"`
	SiteKey      string `json:"siteKey"`
	SealedSecret string `json:"sealedSecret"`
}

func (a *App) loadTurnstileSettings() (turnstileSettings, error) {
	var raw string
	err := a.DB.QueryRow(`SELECT value FROM settings WHERE key='turnstileConfig'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return turnstileSettings{}, nil
	}
	if err != nil {
		return turnstileSettings{}, err
	}
	var cfg turnstileSettings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return turnstileSettings{}, err
	}
	return cfg, nil
}

func (a *App) registerTurnstileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/turnstile", a.publicTurnstileConfig)
	mux.HandleFunc("GET /api/admin/turnstile", a.adminTurnstileConfig)
	mux.HandleFunc("PUT /api/admin/turnstile", a.putTurnstileConfig)
}

func (a *App) publicTurnstileConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cfg, err := a.loadTurnstileSettings()
	if err != nil {
		fail(w, 503, "暂时无法读取验证设置")
		return
	}
	ok(w, map[string]any{"enabled": cfg.Enabled, "siteKey": cfg.SiteKey})
}

func (a *App) adminTurnstileConfig(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	cfg, err := a.loadTurnstileSettings()
	if err != nil {
		fail(w, 503, "暂时无法读取验证设置")
		return
	}
	ok(w, map[string]any{"enabled": cfg.Enabled, "siteKey": cfg.SiteKey, "hasSecret": cfg.SealedSecret != ""})
}

func (a *App) putTurnstileConfig(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var body struct {
		Enabled     *bool   `json:"enabled"`
		SiteKey     *string `json:"siteKey"`
		SecretKey   *string `json:"secretKey"`
		ClearSecret bool    `json:"clearSecret"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		fail(w, 400, "验证设置无效")
		return
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		fail(w, 400, "验证设置无效")
		return
	}
	cfg, err := a.loadTurnstileSettings()
	if err != nil {
		fail(w, 503, "暂时无法读取验证设置")
		return
	}
	if body.Enabled != nil {
		cfg.Enabled = *body.Enabled
	}
	if body.SiteKey != nil {
		cfg.SiteKey = strings.TrimSpace(*body.SiteKey)
	}
	if len(cfg.SiteKey) > 256 {
		fail(w, 400, "站点密钥过长")
		return
	}
	if body.ClearSecret {
		cfg.SealedSecret = ""
	}
	if body.SecretKey != nil && strings.TrimSpace(*body.SecretKey) != "" {
		secret := strings.TrimSpace(*body.SecretKey)
		if len(secret) > 512 {
			fail(w, 400, "验证密钥过长")
			return
		}
		cfg.SealedSecret, err = a.sealAuth(secret, "turnstile-secret")
		if err != nil {
			fail(w, 500, "无法保护验证密钥")
			return
		}
	}
	if cfg.Enabled && (cfg.SiteKey == "" || cfg.SealedSecret == "") {
		fail(w, 400, "启用验证需要站点密钥和服务器密钥")
		return
	}
	value, _ := json.Marshal(cfg)
	if err := a.setSetting("turnstileConfig", value); err != nil {
		fail(w, 500, "无法保存验证设置")
		return
	}
	ok(w, map[string]any{"enabled": cfg.Enabled, "siteKey": cfg.SiteKey, "hasSecret": cfg.SealedSecret != ""})
}

// The browser sends the widget token in X-Turnstile-Token for both multipart
// file and JSON URL uploads. Verify after the existing IP guard, before
// reading/uploading the image. A configured verifier always fails closed.
func (a *App) verifyPublicTurnstile(w http.ResponseWriter, r *http.Request) bool {
	cfg, err := a.loadTurnstileSettings()
	if err != nil {
		fail(w, 503, "访客验证暂时不可用")
		return false
	}
	if !cfg.Enabled {
		return true
	}
	if cfg.SiteKey == "" || cfg.SealedSecret == "" {
		fail(w, 503, "访客验证尚未配置完整")
		return false
	}
	token := strings.TrimSpace(r.Header.Get("X-Turnstile-Token"))
	if token == "" || len(token) > 2048 {
		fail(w, 403, "请完成访客验证后重试")
		return false
	}
	secret, err := a.openAuth(cfg.SealedSecret, "turnstile-secret")
	if err != nil || secret == "" {
		fail(w, 503, "访客验证暂时不可用")
		return false
	}
	proof := map[string]string{"secret": secret, "response": token}
	if ip := net.ParseIP(a.clientIP(r)); ip != nil {
		proof["remoteip"] = ip.String()
	}
	body, _ := json.Marshal(proof)
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileSiteverifyURL, bytes.NewReader(body))
	if err != nil {
		fail(w, 503, "访客验证暂时不可用")
		return false
	}
	request.Header.Set("Content-Type", "application/json")
	transport := a.urlClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		fail(w, 503, "访客验证暂时不可用")
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fail(w, 503, "访客验证暂时不可用")
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		fail(w, 503, "访客验证暂时不可用")
		return false
	}
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
	}
	if json.Unmarshal(raw, &result) != nil {
		fail(w, 503, "访客验证暂时不可用")
		return false
	}
	if !result.Success {
		fail(w, 403, "访客验证未通过，请重试")
		return false
	}
	if a.publicURL != "" {
		expected, _ := url.Parse(a.publicURL)
		if !strings.EqualFold(result.Hostname, expected.Hostname()) {
			fail(w, 403, "访客验证站点不匹配")
			return false
		}
	}
	return true
}
