package openclaw

import (
	"fmt"
	"strings"

	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// FormatPushText builds the notification text sent to OpenClaw for a push_outbox row.
// Mirrors JS formatSystemEventText from listener/message-utils.js.
func FormatPushText(row store.PushOutboxRow) string {
	body := extractBody(row)

	header := fmt.Sprintf("[A2H Market | from:%s | event:%s]", row.PeerID, row.EventID)
	lines := []string{header, "", body}

	// Append payment QR / image URL if not already in body
	qr := extractQR(row.Payload)
	if qr != "" && !strings.Contains(body, qr) {
		lines = append(lines, fmt.Sprintf("[收款二维码]: %s", qr))
	} else {
		att := extractAttachmentURL(row.Payload)
		if att != "" && !strings.Contains(body, att) {
			lines = append(lines, fmt.Sprintf("[附件]: %s", att))
		}
	}

	lines = append(lines, "", fmt.Sprintf("event_id: %s", row.EventID))
	lines = append(lines, fmt.Sprintf("inbox get --event-id %s", row.EventID))
	return strings.Join(lines, "\n")
}

// extractBody computes the main body text from payload, falling back to preview.
func extractBody(row store.PushOutboxRow) string {
	p := row.Payload

	text, _ := p["text"].(string)
	if text == "" {
		text, _ = p["message"].(string)
	}

	qr := extractQR(p)
	att := extractAttachmentURL(p)
	attName := extractAttachmentName(p)

	var parts []string
	if text != "" {
		parts = append(parts, text)
	}
	if qr != "" {
		parts = append(parts, fmt.Sprintf("[收款二维码]: %s", qr))
	}
	if att != "" && qr == "" {
		parts = append(parts, fmt.Sprintf("[附件: %s]: %s", attName, att))
	}

	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	// Fallback to preview
	return row.Preview
}

func extractQR(p map[string]interface{}) string {
	if v, ok := p["payment_qr"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := p["image"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

func extractAttachmentURL(p map[string]interface{}) string {
	att, ok := p["attachment"].(map[string]interface{})
	if !ok {
		return ""
	}
	u, _ := att["url"].(string)
	return strings.TrimSpace(u)
}

func extractAttachmentName(p map[string]interface{}) string {
	att, ok := p["attachment"].(map[string]interface{})
	if !ok {
		return "文件"
	}
	name, _ := att["name"].(string)
	if name == "" {
		return "文件"
	}
	return name
}
