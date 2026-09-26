package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNotificationDestinationsDeliverAndRetryIndependently(t *testing.T) {
	a := testApp(t)
	webhookCalls := map[string]int{}
	var webhookBody map[string]any
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		webhookCalls[r.URL.Host]++
		if r.URL.Host == "one.example" {
			if err := json.Unmarshal(body, &webhookBody); err != nil {
				t.Fatal(err)
			}
			if webhookCalls[r.URL.Host] == 1 {
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("down")), Header: make(http.Header)}, nil
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"forwarded"}`)), Header: make(http.Header)}, nil
	})}
	c := defaultNotificationConfig()
	c.Enabled = true
	c.Channels = []notificationDestination{
		{ID: "one", Type: "webhook", Enabled: true, Name: "QQ 一群", Webhook: notificationWebhook{URL: "https://one.example/webhook/general", Protocol: "general"}},
		{ID: "two", Type: "webhook", Enabled: true, Name: "QQ 二群", Webhook: notificationWebhook{URL: "https://two.example/webhook/general", Protocol: "general"}},
		{ID: "three", Type: "telegram", Enabled: true, Name: "运维群", Telegram: notificationTelegram{Token: "token", ChatID: "123"}},
	}
	token := adminToken(t, a)
	raw, _ := json.Marshal(c)
	if rec := adminRequest(a, token, http.MethodPut, "/api/notification", string(raw)); rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	a.enqueueNotification("upload", "图片上传", "样图已上传", map[string]any{"id": "image-1", "size": 123, "url": "/i/example.png"})
	var count int
	if err := a.DB.QueryRow(`SELECT count(*) FROM notification_events`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("queued %d: %v", count, err)
	}
	for range 3 {
		if err := a.processOneNotification(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if webhookCalls["one.example"] != 1 || webhookCalls["two.example"] != 1 || webhookCalls["api.telegram.org"] != 1 {
		t.Fatalf("calls: %#v", webhookCalls)
	}
	if _, ok := webhookBody["type"]; ok {
		t.Fatalf("unexpected protocol field: %#v", webhookBody)
	}
	for _, key := range []string{"message", "title", "source", "level", "timestamp", "fields"} {
		if _, ok := webhookBody[key]; !ok {
			t.Fatalf("missing %s: %#v", key, webhookBody)
		}
	}
	if webhookBody["level"] != "success" || webhookBody["source"] != "TanoImg" {
		t.Fatalf("protocol body: %#v", webhookBody)
	}
	if _, ok := webhookBody["url"]; ok {
		t.Fatalf("relative URL was sent: %#v", webhookBody)
	}
	if _, err := a.DB.Exec(`UPDATE notification_events SET next_attempt=0 WHERE destination_id='one'`); err != nil {
		t.Fatal(err)
	}
	if err := a.processOneNotification(context.Background()); err != nil {
		t.Fatal(err)
	}
	if webhookCalls["one.example"] != 2 || webhookCalls["two.example"] != 1 || webhookCalls["api.telegram.org"] != 1 {
		t.Fatalf("duplicate delivery: %#v", webhookCalls)
	}
	if err := a.DB.QueryRow(`SELECT count(*) FROM notification_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("remaining %d: %v", count, err)
	}
}

func TestLegacyNotificationSettingsBecomeEditableDestination(t *testing.T) {
	a := testApp(t)
	if initial := a.notificationSettings(); len(initial.Channels) != 0 {
		t.Fatalf("new site should have no destinations: %#v", initial.Channels)
	}
	c := defaultNotificationConfig()
	c.Enabled = true
	c.Method = "webhook"
	c.Webhook.URL = "https://old.example/hook"
	c.Webhook.BodyTemplate = "{{message}}"
	b, _ := json.Marshal(c)
	if err := a.setSetting("notificationConfig", b); err != nil {
		t.Fatal(err)
	}
	got := a.notificationSettings()
	if len(got.Channels) != 1 || got.Channels[0].ID != "legacy" || got.Channels[0].Webhook.Protocol != "legacy" {
		t.Fatalf("legacy migration: %#v", got.Channels)
	}
	// The new UI saves destination settings without the old singleton fields.
	updated, _ := json.Marshal(map[string]any{"enabled": true, "types": got.Types, "channels": got.Channels})
	if err := a.setSetting("notificationConfig", updated); err != nil {
		t.Fatal(err)
	}
	called := 0
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Host != "old.example" || string(body) != "old event" {
			t.Fatalf("old webhook changed: %s %s", r.URL, body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}
	payload, _ := json.Marshal(notificationPayload{Type: "upload", Message: "old event", Timestamp: now()})
	if _, err := a.DB.Exec(`INSERT INTO notification_events(kind,payload) VALUES('upload',?)`, string(payload)); err != nil {
		t.Fatal(err)
	}
	if err := a.processOneNotification(context.Background()); err != nil || called != 1 {
		t.Fatalf("old pending delivery: %v, calls=%d", err, called)
	}
}

func TestEmptyNotificationDestinationsRemainEmptyAfterSave(t *testing.T) {
	a := testApp(t)
	c := a.notificationSettings()
	raw, _ := json.Marshal(c)
	if err := a.setSetting("notificationConfig", raw); err != nil {
		t.Fatal(err)
	}
	if got := a.notificationSettings(); got.Channels == nil || len(got.Channels) != 0 {
		t.Fatalf("empty destinations became a legacy target: %#v", got.Channels)
	}
}

func TestNotificationTargetTestAndDisabledQueue(t *testing.T) {
	a := testApp(t)
	calls := 0
	a.urlClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}
	c := defaultNotificationConfig()
	c.Enabled = true
	c.Channels = []notificationDestination{
		{ID: "one", Name: "群通知", Type: "webhook", Enabled: true, Webhook: notificationWebhook{URL: "https://one.example/webhook/general"}},
		{ID: "draft", Name: "尚未配置", Type: "email", Enabled: true},
	}
	raw, _ := json.Marshal(c)
	if rec := adminRequest(a, adminToken(t, a), http.MethodPost, "/api/notification/test?channel=one", string(raw)); rec.Code != 200 || calls != 1 {
		t.Fatalf("target test: %d %s calls=%d", rec.Code, rec.Body.String(), calls)
	}
	c.Channels = c.Channels[:1]
	raw, _ = json.Marshal(c)
	if err := a.setSetting("notificationConfig", raw); err != nil {
		t.Fatal(err)
	}
	a.notifyEnabled.Store(true)
	a.enqueueNotification("upload", "图片上传", "完成", nil)
	c.Channels[0].Enabled = false
	raw, _ = json.Marshal(c)
	if err := a.setSetting("notificationConfig", raw); err != nil {
		t.Fatal(err)
	}
	if err := a.processOneNotification(context.Background()); err != nil || calls != 1 {
		t.Fatalf("disabled target was sent: %v calls=%d", err, calls)
	}
	var remaining int
	if err := a.DB.QueryRow(`SELECT count(*) FROM notification_events`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("disabled queue remains: %v count=%d", err, remaining)
	}
}

func TestGeneralWebhookRejectsOverlongRenderedMessage(t *testing.T) {
	a := testApp(t)
	_, err := a.generalWebhookJSON(notificationPayload{Title: "Notice", Message: strings.Repeat("测", 1900), Timestamp: now()}, "TanoImg")
	if err == nil {
		t.Fatal("expected QQ length validation")
	}
	a.publicURL = "https://images.example"
	raw, err := a.generalWebhookJSON(notificationPayload{Type: "upload", Title: "上传成功", Message: "完成", Timestamp: "2026-09-26T12:00:00Z", Data: map[string]any{"url": "/i/a.png", "size": 12, "ok": true}}, "TanoImg")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil || body["url"] != "https://images.example/i/a.png" {
		t.Fatalf("invalid URL: %v %#v", err, body)
	}
	if _, ok := body["type"]; ok {
		t.Fatalf("unexpected top-level field: %#v", body)
	}
	if len(body) != 7 {
		t.Fatalf("unexpected schema: %#v", body)
	}
}
