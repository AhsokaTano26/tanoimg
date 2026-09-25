package app

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"sort"
	"strings"
)

var emailFieldNames = map[string]string{
	"username": "账户", "ip": "IP 地址", "id": "图片 ID", "imageId": "图片 ID",
	"filename": "文件名", "url": "图片链接", "size": "文件大小（字节）",
	"type": "可见性", "score": "审核分数", "isNsfw": "违规内容", "provider": "审核服务",
}

func emailKind(kind string) string {
	switch kind {
	case "login":
		return "登录提醒"
	case "upload":
		return "上传动态"
	case "nsfw":
		return "审核结果"
	case "test":
		return "测试邮件"
	default:
		return "站点通知"
	}
}

func emailValue(v any) string { return fmt.Sprint(v) }

func emailDetails(payload notificationPayload) ([]string, []string) {
	keys := make([]string, 0, len(payload.Data))
	for key := range payload.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	labels := make([]string, len(keys))
	for i, key := range keys {
		labels[i] = emailFieldNames[key]
		if labels[i] == "" {
			labels[i] = key
		}
	}
	return keys, labels
}

func emailPlain(payload notificationPayload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TanoImg · %s\n\n%s\n%s\n\n时间：%s\n", emailKind(payload.Type), payload.Title, payload.Message, payload.Timestamp)
	keys, labels := emailDetails(payload)
	if len(keys) > 0 {
		b.WriteString("\n详细信息\n")
	}
	for i, key := range keys {
		fmt.Fprintf(&b, "%s：%s\n", labels[i], emailValue(payload.Data[key]))
	}
	b.WriteString("\n此邮件由 TanoImg 自动发送。\n")
	return b.String()
}

func emailHTML(payload notificationPayload) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0;padding:0;background:#f3f6fb;color:#142238;font-family:Arial,'PingFang SC','Microsoft YaHei',sans-serif"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f3f6fb"><tr><td align="center" style="padding:32px 16px"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;background:#ffffff;border:1px solid #e5ebf4;border-radius:16px"><tr><td style="padding:28px 32px 22px;border-bottom:1px solid #e5ebf4"><span style="display:inline-block;width:11px;height:11px;background:#0052ff;border-radius:3px;vertical-align:middle"></span><span style="margin-left:9px;font-size:17px;font-weight:700;letter-spacing:-.02em;color:#11213c">TanoImg</span><span style="float:right;font-size:12px;color:#64748b">图片管理通知</span></td></tr><tr><td style="padding:30px 32px 12px"><div style="display:inline-block;padding:6px 11px;border-radius:6px;background:#eaf1ff;color:#0052ff;font-size:12px;font-weight:700">`)
	b.WriteString(esc(emailKind(payload.Type)))
	b.WriteString(`</div><h1 style="margin:20px 0 12px;font-size:25px;line-height:1.3;color:#10213c">`)
	b.WriteString(esc(payload.Title))
	b.WriteString(`</h1><p style="margin:0;color:#46556d;font-size:15px;line-height:1.8;white-space:pre-line">`)
	b.WriteString(esc(payload.Message))
	b.WriteString(`</p></td></tr>`)
	keys, labels := emailDetails(payload)
	if len(keys) > 0 {
		b.WriteString(`<tr><td style="padding:12px 32px 22px"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border:1px solid #e5ebf4;border-radius:10px;background:#f9fbff">`)
		for i, key := range keys {
			b.WriteString(`<tr><td style="padding:10px 14px;width:110px;border-bottom:1px solid #e5ebf4;color:#64748b;font-size:12px;vertical-align:top">`)
			b.WriteString(esc(labels[i]))
			b.WriteString(`</td><td style="padding:10px 14px;border-bottom:1px solid #e5ebf4;color:#253653;font-size:13px;line-height:1.6;overflow-wrap:anywhere">`)
			b.WriteString(esc(emailValue(payload.Data[key])))
			b.WriteString(`</td></tr>`)
		}
		b.WriteString(`</table></td></tr>`)
	}
	b.WriteString(`<tr><td style="padding:12px 32px 28px;color:#758399;font-size:12px;line-height:1.6">发生时间：`)
	b.WriteString(esc(payload.Timestamp))
	b.WriteString(`</td></tr></table><p style="margin:18px 0 0;color:#8a97aa;font-size:11px">此邮件由 TanoImg 自动发送。</p></td></tr></table></body></html>`)
	return b.String()
}

func buildNotificationEmail(from, to string, payload notificationPayload) (string, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", from, to, mime.BEncoding.Encode("UTF-8", payload.Title))
	writer := multipart.NewWriter(&b)
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", writer.Boundary())
	parts := []struct{ typ, content string }{{"text/plain", emailPlain(payload)}, {"text/html", emailHTML(payload)}}
	for _, p := range parts {
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", p.typ+"; charset=UTF-8")
		header.Set("Content-Transfer-Encoding", "quoted-printable")
		part, err := writer.CreatePart(header)
		if err != nil {
			return "", err
		}
		qp := quotedprintable.NewWriter(part)
		if _, err := io.WriteString(qp, p.content); err != nil {
			return "", err
		}
		if err := qp.Close(); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}
