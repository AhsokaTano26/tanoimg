package app

import (
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
)

func TestNotificationEmailHasSafeHTMLAndPlainFallback(t *testing.T) {
	payload := notificationPayload{Type: "upload", Title: "图片上传", Message: "<script>alert(1)</script> 已上传", Timestamp: "2026-09-25T12:00:00Z", Data: map[string]any{"filename": "<private>.png", "url": "https://example.com/i/a.png", "size": 2048}}
	raw, err := buildNotificationEmail("sender@example.com", "recipient@example.com", payload)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	typ, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || typ != "multipart/alternative" {
		t.Fatalf("type: %q %v", typ, err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	var bodies []string
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(quotedprintable.NewReader(part))
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(decoded))
	}
	if len(bodies) != 2 {
		t.Fatalf("parts: %d", len(bodies))
	}
	if !strings.Contains(bodies[0], payload.Message) || !strings.Contains(bodies[0], "<private>.png") {
		t.Fatal("plain text missing original content")
	}
	if strings.Contains(bodies[1], "<script>") || strings.Contains(bodies[1], "<private>.png") {
		t.Fatal("HTML injection")
	}
	for _, want := range []string{"&lt;script&gt;", "&lt;private&gt;.png", "2026-09-25", "图片上传", "https://example.com/i/a.png"} {
		if !strings.Contains(bodies[1], want) {
			t.Fatalf("HTML missing %q", want)
		}
	}
}
