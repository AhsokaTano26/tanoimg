package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type notificationWebhook struct {
	URL          string            `json:"url"`
	Protocol     string            `json:"protocol,omitempty"`
	Method       string            `json:"method,omitempty"`
	ContentType  string            `json:"contentType,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyTemplate string            `json:"bodyTemplate,omitempty"`
}

type notificationTelegram struct {
	Token  string `json:"token"`
	ChatID string `json:"chatId"`
}
type notificationEmail struct {
	Service string `json:"service"`
	User    string `json:"user"`
	Pass    string `json:"pass"`
	To      string `json:"to"`
}
type notificationServerChan struct {
	SendKey string `json:"sendKey"`
}

type notificationDestination struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Enabled    bool                   `json:"enabled"`
	Webhook    notificationWebhook    `json:"webhook"`
	Telegram   notificationTelegram   `json:"telegram"`
	Email      notificationEmail      `json:"email"`
	ServerChan notificationServerChan `json:"serverchan"`
}

func normalizedNotificationDestinations(c notificationConfig) []notificationDestination {
	if c.Channels != nil {
		return c.Channels
	}
	channel := notificationDestination{ID: "legacy", Name: "原有通知目标", Type: c.Method, Enabled: true}
	channel.Webhook = notificationWebhook{URL: c.Webhook.URL, Protocol: "legacy", Method: c.Webhook.Method, ContentType: c.Webhook.ContentType, Headers: c.Webhook.Headers, BodyTemplate: c.Webhook.BodyTemplate}
	channel.Telegram = notificationTelegram{Token: c.Telegram.Token, ChatID: c.Telegram.ChatID}
	channel.Email = notificationEmail{Service: c.Email.Service, User: c.Email.User, Pass: c.Email.Pass, To: c.Email.To}
	channel.ServerChan = notificationServerChan{SendKey: c.ServerChan.SendKey}
	return []notificationDestination{channel}
}

func validateNotificationDestinations(channels []notificationDestination) error {
	if len(channels) > 32 {
		return errors.New("最多配置 32 个通知目标")
	}
	seen := make(map[string]bool, len(channels))
	for _, channel := range channels {
		if len(channel.ID) == 0 || len(channel.ID) > 80 || strings.Trim(channel.ID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" || seen[channel.ID] {
			return errors.New("通知目标 ID 无效或重复")
		}
		seen[channel.ID] = true
		if utf8.RuneCountInString(strings.TrimSpace(channel.Name)) == 0 || utf8.RuneCountInString(channel.Name) > 100 {
			return errors.New("通知目标名称无效")
		}
		switch channel.Type {
		case "webhook":
			if channel.Webhook.URL != "" {
				if _, err := parseRemoteURL(channel.Webhook.URL); err != nil {
					return errors.New("Webhook 地址无效")
				}
			} else if channel.Enabled {
				return errors.New("Webhook URL 未配置")
			}
			if channel.Webhook.Protocol != "" && channel.Webhook.Protocol != "general" && channel.Webhook.Protocol != "legacy" {
				return errors.New("Webhook 协议无效")
			}
			if channel.Webhook.Protocol == "legacy" {
				if channel.Webhook.Method != "" && channel.Webhook.Method != "POST" && channel.Webhook.Method != "PUT" {
					return errors.New("Webhook 请求方法无效")
				}
				if len(channel.Webhook.BodyTemplate) > 16384 || len(channel.Webhook.Headers) > 20 {
					return errors.New("Webhook 配置过长")
				}
				for key, value := range channel.Webhook.Headers {
					if !httpgutsHeaderName(key) || strings.ContainsAny(value, "\r\n") {
						return errors.New("Webhook 请求头无效")
					}
				}
			}
		case "email":
			if channel.Enabled && (channel.Email.Service == "" || channel.Email.User == "" || channel.Email.Pass == "") {
				return errors.New("邮件账户配置不完整")
			}
			if channel.Email.Service != "" && channel.Email.Service != "gmail" && channel.Email.Service != "qq" && channel.Email.Service != "163" && channel.Email.Service != "outlook" {
				return errors.New("不支持的邮件服务")
			}
			if strings.ContainsAny(channel.Email.User+channel.Email.To, "\r\n") {
				return errors.New("邮箱地址无效")
			}
		case "telegram":
			if channel.Enabled && (channel.Telegram.Token == "" || channel.Telegram.ChatID == "") {
				return errors.New("Telegram Token 或 Chat ID 未配置")
			}
		case "serverchan":
			if channel.Enabled && channel.ServerChan.SendKey == "" {
				return errors.New("Server酱 SendKey 未配置")
			}
		default:
			return errors.New("不支持的通知方式")
		}
	}
	return nil
}

func ensureNotificationDestinationColumn(a *sql.DB) error {
	rows, err := a.Query(`PRAGMA table_info(notification_events)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "destination_id" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || found {
		return err
	}
	_, err = a.Exec(`ALTER TABLE notification_events ADD COLUMN destination_id TEXT NOT NULL DEFAULT ''`)
	return err
}

func (a *App) sendNotificationDestination(ctx context.Context, channel notificationDestination, payload notificationPayload) error {
	c := defaultNotificationConfig()
	switch channel.Type {
	case "webhook":
		if channel.Webhook.Protocol == "legacy" {
			c.Webhook.URL = channel.Webhook.URL
			c.Webhook.Method = channel.Webhook.Method
			c.Webhook.ContentType = channel.Webhook.ContentType
			c.Webhook.Headers = channel.Webhook.Headers
			c.Webhook.BodyTemplate = channel.Webhook.BodyTemplate
			return a.sendWebhook(ctx, c, payload)
		}
		body, err := a.generalWebhookJSON(payload, "TanoImg")
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.Webhook.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		return a.sendNotificationHTTP(req)
	case "telegram":
		c.Telegram.Token = channel.Telegram.Token
		c.Telegram.ChatID = channel.Telegram.ChatID
		return a.sendTelegram(ctx, c, payload)
	case "email":
		c.Email.Service = channel.Email.Service
		c.Email.User = channel.Email.User
		c.Email.Pass = channel.Email.Pass
		c.Email.To = channel.Email.To
		return sendEmail(ctx, c, payload)
	case "serverchan":
		c.ServerChan.SendKey = channel.ServerChan.SendKey
		return a.sendServerChan(ctx, c, payload)
	}
	return errors.New("不支持的通知方式")
}

type generalWebhookRequest struct {
	Message   string         `json:"message"`
	Title     string         `json:"title,omitempty"`
	Source    string         `json:"source,omitempty"`
	Level     string         `json:"level,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
	URL       string         `json:"url,omitempty"`
}

func (a *App) generalWebhookJSON(payload notificationPayload, source string) ([]byte, error) {
	level := map[string]string{"login": "info", "upload": "success", "nsfw": "warning", "test": "info"}[payload.Type]
	message := strings.TrimSpace(payload.Message)
	if message == "" || utf8.RuneCountInString(message) > 1900 {
		return nil, errors.New("Webhook 正文无效或超过 1900 字符")
	}
	request := generalWebhookRequest{Message: message, Title: strings.TrimSpace(payload.Title), Source: strings.TrimSpace(source), Level: level}
	if payload.Timestamp != "" {
		if _, err := time.Parse(time.RFC3339, payload.Timestamp); err != nil {
			return nil, errors.New("Webhook 时间格式无效")
		}
		request.Timestamp = payload.Timestamp
	}
	request.Fields = make(map[string]any)
	for key, value := range payload.Data {
		if key == "url" || strings.TrimSpace(key) == "" {
			continue
		}
		switch value.(type) {
		case string, float64, float32, int, int64, bool, json.Number:
			request.Fields[key] = value
		}
	}
	if len(request.Fields) == 0 {
		request.Fields = nil
	}
	if value, ok := payload.Data["url"].(string); ok && value != "" {
		if strings.HasPrefix(value, "/") && a.publicURL != "" {
			value = a.publicURL + value
		}
		if target, err := url.Parse(value); err == nil && (target.Scheme == "http" || target.Scheme == "https") && target.Hostname() != "" {
			request.URL = value
		}
	}
	if utf8.RuneCountInString(renderGeneralWebhookMessage(request)) > 1900 {
		return nil, errors.New("Webhook 排版后超过 1900 字符")
	}
	return json.Marshal(request)
}

func renderGeneralWebhookMessage(request generalWebhookRequest) string {
	var lines []string
	if request.Title != "" || request.Level != "" {
		prefix := map[string]string{"info": "🔵", "success": "🟢", "warning": "🟡", "error": "🔴"}[request.Level]
		title := request.Title
		if title == "" {
			title = map[string]string{"info": "通知", "success": "成功", "warning": "警告", "error": "错误"}[request.Level]
		}
		if prefix != "" {
			title = prefix + " " + title
		}
		lines = append(lines, title)
	}
	if request.Source != "" {
		lines = append(lines, "来源："+request.Source)
	}
	if request.Timestamp != "" {
		parsed, _ := time.Parse(time.RFC3339, request.Timestamp)
		lines = append(lines, "时间："+parsed.In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02 15:04:05-07:00"))
	}
	keys := make([]string, 0, len(request.Fields))
	for key := range request.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+"："+fmt.Sprint(request.Fields[key]))
	}
	if request.Source != "" || request.Timestamp != "" || len(keys) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, request.Message)
	if request.URL != "" {
		lines = append(lines, "链接："+request.URL)
	}
	return strings.Join(lines, "\n")
}
