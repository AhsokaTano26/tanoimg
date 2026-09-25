package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"
)

type notificationConfig struct {
	Enabled bool   `json:"enabled"`
	Method  string `json:"method"`
	Types   struct {
		Login  bool `json:"login"`
		Upload bool `json:"upload"`
		NSFW   bool `json:"nsfw"`
	} `json:"types"`
	Webhook struct {
		URL          string            `json:"url"`
		Method       string            `json:"method"`
		ContentType  string            `json:"contentType"`
		Headers      map[string]string `json:"headers"`
		BodyTemplate string            `json:"bodyTemplate"`
	} `json:"webhook"`
	Telegram struct {
		Token  string `json:"token"`
		ChatID string `json:"chatId"`
	} `json:"telegram"`
	Email struct {
		Service string `json:"service"`
		User    string `json:"user"`
		Pass    string `json:"pass"`
		To      string `json:"to"`
	} `json:"email"`
	ServerChan struct {
		SendKey string `json:"sendKey"`
	} `json:"serverchan"`
}

type notificationPayload struct {
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	Timestamp string         `json:"timestamp"`
	Data      map[string]any `json:"data"`
}

func defaultNotificationConfig() notificationConfig {
	var c notificationConfig
	c.Method = "telegram"
	c.Types.Login, c.Types.Upload, c.Types.NSFW = true, true, true
	c.Webhook.Method = "POST"
	c.Webhook.ContentType = "application/json"
	c.Webhook.Headers = map[string]string{}
	c.Webhook.BodyTemplate = `{"type":"{{type}}","title":"{{title}}","message":"{{message}}","timestamp":"{{timestamp}}","data":"{{data}}"}`
	return c
}

func (a *App) notificationSettings() notificationConfig {
	c := defaultNotificationConfig()
	json.Unmarshal(a.setting("notificationConfig", c), &c)
	return c
}

func validateNotification(c notificationConfig) error {
	switch c.Method {
	case "webhook", "telegram", "email", "serverchan":
	default:
		return errors.New("不支持的通知方式")
	}
	if c.Webhook.URL != "" {
		if _, err := parseRemoteURL(c.Webhook.URL); err != nil {
			return errors.New("Webhook 地址无效")
		}
	}
	if c.Webhook.Method != "" && c.Webhook.Method != "POST" && c.Webhook.Method != "PUT" {
		return errors.New("Webhook 请求方法无效")
	}
	if len(c.Webhook.BodyTemplate) > 16384 || len(c.Webhook.Headers) > 20 {
		return errors.New("Webhook 配置过长")
	}
	for key, value := range c.Webhook.Headers {
		if !httpgutsHeaderName(key) || strings.ContainsAny(value, "\r\n") {
			return errors.New("Webhook 请求头无效")
		}
	}
	return nil
}

func httpgutsHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

func (a *App) getNotification(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	ok(w, a.notificationSettings())
}

func (a *App) putNotification(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	c := defaultNotificationConfig()
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&c) != nil {
		fail(w, 400, "通知配置无效")
		return
	}
	if err := validateNotification(c); err != nil {
		fail(w, 400, err.Error())
		return
	}
	b, _ := json.Marshal(c)
	if err := a.setSetting("notificationConfig", b); err != nil {
		fail(w, 500, "保存通知配置失败")
		return
	}
	a.notifyEnabled.Store(c.Enabled)
	ok(w, c)
}

func (a *App) testNotification(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	c := defaultNotificationConfig()
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&c) != nil {
		fail(w, 400, "通知配置无效")
		return
	}
	if err := validateNotification(c); err != nil {
		fail(w, 400, err.Error())
		return
	}
	payload := notificationPayload{Type: "test", Title: "TanoImg 测试通知", Message: "测试通知发送成功", Timestamp: now(), Data: map[string]any{}}
	if err := a.sendNotification(r.Context(), c, payload); err != nil {
		fail(w, 502, err.Error())
		return
	}
	ok(w, map[string]bool{"sent": true})
}

func (a *App) enqueueNotification(kind, title, message string, data map[string]any) {
	if !a.notifyEnabled.Load() {
		return
	}
	c := a.notificationSettings()
	if !c.Enabled || !(kind == "login" && c.Types.Login || kind == "upload" && c.Types.Upload || kind == "nsfw" && c.Types.NSFW) {
		return
	}
	payload, err := json.Marshal(notificationPayload{Type: kind, Title: title, Message: message, Timestamp: now(), Data: data})
	if err != nil {
		return
	}
	if _, err := a.DB.Exec(`INSERT INTO notification_events(kind,payload) VALUES(?,?)`, kind, string(payload)); err == nil {
		select {
		case a.notifyWake <- struct{}{}:
		default:
		}
	}
}

func (a *App) StartNotifications() {
	a.notifyOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		a.notifyCancel = cancel
		a.notifyDone = make(chan struct{})
		go func() {
			defer close(a.notifyDone)
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				for {
					err := a.processOneNotification(ctx)
					if errors.Is(err, sql.ErrNoRows) || ctx.Err() != nil {
						break
					}
					if err != nil {
						break
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				case <-a.notifyWake:
				}
			}
		}()
	})
}

func (a *App) processOneNotification(ctx context.Context) error {
	var id int64
	var kind, raw string
	var retry int
	if err := a.DB.QueryRow(`SELECT id,kind,payload,retry_count FROM notification_events WHERE status='pending' AND next_attempt<=? ORDER BY id LIMIT 1`, time.Now().Unix()).Scan(&id, &kind, &raw, &retry); err != nil {
		return err
	}
	var payload notificationPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return err
	}
	c := a.notificationSettings()
	if !c.Enabled || !(kind == "login" && c.Types.Login || kind == "upload" && c.Types.Upload || kind == "nsfw" && c.Types.NSFW) {
		_, err := a.DB.Exec(`DELETE FROM notification_events WHERE id=?`, id)
		return err
	}
	err := a.sendNotification(ctx, c, payload)
	if err == nil {
		_, err = a.DB.Exec(`DELETE FROM notification_events WHERE id=?`, id)
		return err
	}
	retry++
	status := "pending"
	if retry >= 3 {
		status = "error"
	}
	_, updateErr := a.DB.Exec(`UPDATE notification_events SET status=?,retry_count=?,next_attempt=?,error=? WHERE id=?`, status, retry, time.Now().Add(time.Duration(retry)*time.Minute).Unix(), err.Error(), id)
	return updateErr
}

func (a *App) sendNotification(ctx context.Context, c notificationConfig, payload notificationPayload) error {
	switch c.Method {
	case "webhook":
		return a.sendWebhook(ctx, c, payload)
	case "telegram":
		return a.sendTelegram(ctx, c, payload)
	case "serverchan":
		return a.sendServerChan(ctx, c, payload)
	case "email":
		return sendEmail(ctx, c, payload)
	}
	return errors.New("不支持的通知方式")
}

func (a *App) sendWebhook(ctx context.Context, c notificationConfig, payload notificationPayload) error {
	if c.Webhook.URL == "" {
		return errors.New("Webhook URL 未配置")
	}
	data, _ := json.Marshal(payload.Data)
	body := c.Webhook.BodyTemplate
	if body == "" {
		body = defaultNotificationConfig().Webhook.BodyTemplate
	}
	body = strings.NewReplacer("{{type}}", payload.Type, "{{title}}", payload.Title, "{{message}}", payload.Message, "{{timestamp}}", payload.Timestamp, "{{data}}", string(data)).Replace(body)
	method := c.Webhook.Method
	if method == "" {
		method = "POST"
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Webhook.URL, strings.NewReader(body))
	if err != nil {
		return err
	}
	contentType := c.Webhook.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range c.Webhook.Headers {
		req.Header.Set(k, v)
	}
	return a.sendNotificationHTTP(req)
}

func (a *App) sendNotificationHTTP(req *http.Request) error {
	response, err := a.urlClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("通知服务返回 HTTP %d", response.StatusCode)
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return nil
}

func (a *App) sendTelegram(ctx context.Context, c notificationConfig, payload notificationPayload) error {
	if c.Telegram.Token == "" || c.Telegram.ChatID == "" {
		return errors.New("Telegram Token 或 Chat ID 未配置")
	}
	text := payload.Title + "\n" + payload.Message
	if imageURL, ok := payload.Data["url"].(string); ok && imageURL != "" {
		text += "\n" + imageURL
	}
	base := os.Getenv("TELEGRAM_API_URL")
	if base == "" {
		base = "https://api.telegram.org"
	}
	if _, err := parseRemoteURL(base); err != nil {
		return err
	}
	endpoint := strings.TrimRight(base, "/") + "/bot" + url.PathEscape(c.Telegram.Token) + "/sendMessage"
	b, _ := json.Marshal(map[string]any{"chat_id": c.Telegram.ChatID, "text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return a.sendNotificationHTTP(req)
}

func (a *App) sendServerChan(ctx context.Context, c notificationConfig, payload notificationPayload) error {
	if c.ServerChan.SendKey == "" {
		return errors.New("Server酱 SendKey 未配置")
	}
	endpoint := "https://sctapi.ftqq.com/" + url.PathEscape(c.ServerChan.SendKey) + ".send"
	form := url.Values{"title": {payload.Title}, "desp": {payload.Message}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return a.sendNotificationHTTP(req)
}

func sendEmail(ctx context.Context, c notificationConfig, payload notificationPayload) error {
	hosts := map[string]string{"gmail": "smtp.gmail.com", "qq": "smtp.qq.com", "163": "smtp.163.com", "outlook": "smtp.office365.com"}
	host := hosts[c.Email.Service]
	if host == "" {
		return errors.New("不支持的邮件服务")
	}
	if c.Email.User == "" || c.Email.Pass == "" {
		return errors.New("邮件账户配置不完整")
	}
	to := c.Email.To
	if to == "" {
		to = c.Email.User
	}
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(c.Email.User, "\r\n") {
		return errors.New("邮箱地址无效")
	}
	port := "465"
	if c.Email.Service == "outlook" {
		port = "587"
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	var protocol *textproto.Conn
	if port == "465" {
		secure := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		protocol = textproto.NewConn(secure)
	} else {
		protocol = textproto.NewConn(conn)
	}
	if _, _, err := protocol.ReadResponse(220); err != nil {
		return err
	}
	command := func(expected int, line string) error {
		if err := protocol.PrintfLine("%s", line); err != nil {
			return err
		}
		_, _, err := protocol.ReadResponse(expected)
		return err
	}
	if err := command(250, "EHLO tanoimg"); err != nil {
		return err
	}
	if port == "587" {
		if err := command(220, "STARTTLS"); err != nil {
			return err
		}
		secure := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		protocol = textproto.NewConn(secure)
		if err := command(250, "EHLO tanoimg"); err != nil {
			return err
		}
	}
	auth := base64.StdEncoding.EncodeToString([]byte("\x00" + c.Email.User + "\x00" + c.Email.Pass))
	if err := command(235, "AUTH PLAIN "+auth); err != nil {
		return err
	}
	if err := command(250, "MAIL FROM:<"+c.Email.User+">"); err != nil {
		return err
	}
	if err := command(250, "RCPT TO:<"+to+">"); err != nil {
		return err
	}
	if err := command(354, "DATA"); err != nil {
		return err
	}
	writer := protocol.DotWriter()
	message, err := buildNotificationEmail(c.Email.User, to, payload)
	if err != nil {
		writer.Close()
		return err
	}
	if _, err := io.WriteString(writer, message); err != nil {
		writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if _, _, err := protocol.ReadResponse(250); err != nil {
		return err
	}
	_ = protocol.PrintfLine("QUIT")
	return nil
}
