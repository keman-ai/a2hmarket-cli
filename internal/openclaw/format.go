package openclaw

import (
	"fmt"
	"strings"

	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// AttachmentInfo holds extracted attachment metadata from a push outbox row.
type AttachmentInfo struct {
	URL      string // OSS or external URL
	Name     string // display filename
	MimeType string // e.g. "image/png"
	IsImage  bool   // determined by extension or MIME
}

// innerPayload extracts the nested "payload" map from the envelope.
// The database stores the full Envelope JSON whose structure is:
//
//	{ "payload": { "text": "...", "attachment": {...} }, "message_id": "...", ... }
//
// This function returns the inner payload map, or the root map as fallback.
func innerPayload(row store.PushOutboxRow) map[string]interface{} {
	if inner, ok := row.Payload["payload"].(map[string]interface{}); ok {
		return inner
	}
	return row.Payload
}

// ExtractAttachment returns attachment info from a PushOutboxRow, or nil if none.
func ExtractAttachment(row store.PushOutboxRow) *AttachmentInfo {
	p := innerPayload(row)

	if qr := extractQR(p); qr != "" {
		return &AttachmentInfo{URL: qr, Name: "payment_qr.png", IsImage: true}
	}

	att, ok := p["attachment"].(map[string]interface{})
	if !ok {
		return nil
	}
	url, _ := att["url"].(string)
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}

	name, _ := att["name"].(string)
	if name == "" {
		name = filenameFromURL(url)
	}

	mime, _ := att["mime_type"].(string)
	isImage := IsImageFile(name) || IsImageMIME(mime)

	return &AttachmentInfo{URL: url, Name: name, MimeType: mime, IsImage: isImage}
}

// FormatPushText builds the notification text sent to OpenClaw for a push_outbox row.
// URLs are formatted as markdown links for clickability.
func FormatPushText(row store.PushOutboxRow) string {
	body := extractBody(row)

	header := fmt.Sprintf("[A2H Market | from:%s | event:%s]", row.PeerID, row.EventID)
	lines := []string{header, "", body}

	lines = append(lines, "", fmt.Sprintf("event_id: %s", row.EventID))
	lines = append(lines, fmt.Sprintf("inbox get --event-id %s", row.EventID))
	return strings.Join(lines, "\n")
}

// FormatPushTextForMedia builds a shorter text to accompany a media message.
func FormatPushTextForMedia(row store.PushOutboxRow) string {
	p := innerPayload(row)
	text, _ := p["text"].(string)
	if text == "" {
		text, _ = p["message"].(string)
	}

	parts := []string{
		fmt.Sprintf("📨 A2H Market 消息 (from: %s)", row.PeerID),
	}
	if text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, fmt.Sprintf("event_id: %s", row.EventID))
	return strings.Join(parts, "\n")
}

// FormatExternalURLText builds text with a clickable markdown link for URLs
// that are too large to download.
func FormatExternalURLText(row store.PushOutboxRow, att *AttachmentInfo) string {
	p := innerPayload(row)
	text, _ := p["text"].(string)
	if text == "" {
		text, _ = p["message"].(string)
	}

	lines := []string{
		fmt.Sprintf("[A2H Market | from:%s | event:%s]", row.PeerID, row.EventID),
		"",
	}
	if text != "" {
		lines = append(lines, text, "")
	}
	lines = append(lines, fmt.Sprintf("📎 附件: [%s](%s)", att.Name, att.URL))
	lines = append(lines, "", fmt.Sprintf("event_id: %s", row.EventID))
	return strings.Join(lines, "\n")
}

func extractBody(row store.PushOutboxRow) string {
	p := innerPayload(row)

	text, _ := p["text"].(string)
	if text == "" {
		text, _ = p["message"].(string)
	}

	att := ExtractAttachment(row)

	var parts []string
	if text != "" {
		parts = append(parts, text)
	}
	if att != nil {
		if att.IsImage {
			parts = append(parts, fmt.Sprintf("🖼️ [%s](%s)", att.Name, att.URL))
		} else {
			parts = append(parts, fmt.Sprintf("📎 [%s](%s)", att.Name, att.URL))
		}
	}

	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return row.Preview
}

func extractQR(p map[string]interface{}) string {
	if v, ok := p["payment_qr"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}
