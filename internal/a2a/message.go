// Package a2a implements A2A protocol message utilities and routing.
// It mirrors JS runtime/js/src/listener/message-utils.js.
package a2a

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SanitizePreview compresses whitespace and truncates to maxChars (default 80).
func SanitizePreview(text string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 80
	}
	// Normalize whitespace (mirrors JS .replace(/\r/g," ").replace(/\n/g," ").replace(/\s+/g," ").trim())
	compact := strings.TrimSpace(normalizeWS(text))
	if utf8.RuneCountInString(compact) <= maxChars {
		return compact
	}
	limit := maxChars - 3
	if limit < 0 {
		limit = 0
	}
	runes := []rune(compact)
	return string(runes[:limit]) + "..."
}

func normalizeWS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inWS := false
	for _, r := range s {
		if r == '\r' || r == '\n' || r == '\t' {
			r = ' '
		}
		if r == ' ' {
			if !inWS {
				b.WriteRune(r)
			}
			inWS = true
		} else {
			inWS = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ToEventHash produces a deterministic SHA-256 hash for deduplication.
// If messageID is non-empty it hashes "id:<messageID>"; otherwise it hashes
// "<peerID>|<messageTs>|<messageText>".
func ToEventHash(peerID string, messageTs int64, messageText, messageID string) string {
	var raw string
	if messageID != "" {
		raw = "id:" + messageID
	} else {
		raw = fmt.Sprintf("%s|%d|%s", peerID, messageTs, messageText)
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ExtractPreview derives the human-readable preview from a payload map.
// Mirrors JS extractFullText() → sanitizePreview().
func ExtractPreview(payload map[string]interface{}, maxChars int) string {
	if payload == nil {
		return ""
	}
	full := extractFullText(payload)
	if full == "" {
		return ""
	}
	return SanitizePreview(full, maxChars)
}

func extractFullText(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}

	text := ""
	if v, ok := payload["text"]; ok {
		text = strings.TrimSpace(fmt.Sprintf("%v", v))
	} else if v, ok := payload["message"]; ok {
		text = strings.TrimSpace(fmt.Sprintf("%v", v))
	}

	qr := extractPaymentQr(payload)
	att := extractAttachment(payload)

	var lines []string
	if text != "" {
		lines = append(lines, text)
	}
	if qr != "" {
		lines = append(lines, "[收款二维码]: "+qr)
	}
	if att != nil {
		label := formatAttachmentLabel(att)
		url, _ := att["url"].(string)
		lines = append(lines, label+": "+url)
	}
	return strings.Join(lines, "\n")
}

func extractPaymentQr(payload map[string]interface{}) string {
	if v, ok := payload["payment_qr"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := payload["image"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

func extractAttachment(payload map[string]interface{}) map[string]interface{} {
	att, ok := payload["attachment"].(map[string]interface{})
	if !ok {
		return nil
	}
	if _, hasURL := att["url"]; !hasURL {
		return nil
	}
	return att
}

func formatAttachmentLabel(att map[string]interface{}) string {
	name, _ := att["name"].(string)
	if name == "" {
		name = "文件"
	}
	mime, _ := att["mime_type"].(string)
	if strings.HasPrefix(mime, "image/") {
		return "[图片: " + name + "]"
	}
	_, hasExpiry := att["expires_at"]
	if hasExpiry {
		return "[附件: " + name + "（24h有效）]"
	}
	return "[附件: " + name + "]"
}

// PushImage represents a media URL extracted from a payload for push notifications.
type PushImage struct {
	URL  string
	Type string // "payment_qr" or "image"
}

// ExtractPushImage extracts the first media URL suitable for push notification from payload.
func ExtractPushImage(payload map[string]interface{}) *PushImage {
	if payload == nil {
		return nil
	}
	qr := extractPaymentQr(payload)
	if qr != "" {
		return &PushImage{URL: qr, Type: "payment_qr"}
	}
	att := extractAttachment(payload)
	if att != nil {
		mime, _ := att["mime_type"].(string)
		url, _ := att["url"].(string)
		if strings.HasPrefix(mime, "image/") && url != "" {
			return &PushImage{URL: strings.TrimSpace(url), Type: "image"}
		}
	}
	return nil
}

// ExtractPayloadFromEnvelope resolves a payload map from an event's payload_json field.
// It handles both full envelope JSON (with a "payload" key) and bare payload JSON.
func ExtractPayloadFromEnvelope(payloadJSON string) map[string]interface{} {
	if payloadJSON == "" {
		return nil
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &envelope); err != nil {
		return nil
	}
	if sub, ok := envelope["payload"].(map[string]interface{}); ok {
		return sub
	}
	return envelope
}

// FormatSystemEventText formats an event as a system notification message (for Feishu cards, etc.).
func FormatSystemEventText(peerID, eventID, preview, payloadJSON string) string {
	payload := ExtractPayloadFromEnvelope(payloadJSON)
	fullText := extractFullText(payload)
	if fullText == "" {
		fullText = SanitizePreview(preview, 200)
	}
	pushImage := ExtractPushImage(payload)

	header := fmt.Sprintf("[A2H Market | from:%s | event:%s]", peerID, eventID)
	lines := []string{header, "", fullText}

	if pushImage != nil && !strings.Contains(fullText, pushImage.URL) {
		label := "[收款二维码]"
		if pushImage.Type == "image" {
			label = "[图片]"
		}
		lines = append(lines, label+": "+pushImage.URL)
	}

	lines = append(lines, "", "event_id: "+eventID, "inbox get --event-id "+eventID)
	return strings.Join(lines, "\n")
}

// deliverableKinds are the session key kinds that can be used for direct push delivery.
var deliverableKinds = map[string]bool{
	"direct":  true,
	"dm":      true,
	"group":   true,
	"channel": true,
}

// DeliveryHints is the parsed channel and recipient target from a session key.
type DeliveryHints struct {
	Channel string
	To      string
}

// ParseDeliveryHintsFromSessionKey parses a session key of the form:
//
//	[agent:]<channel>:<kind>:<target>
//
// e.g. "agent:feishu:channel:oc_abc123" → {Channel:"feishu", To:"oc_abc123"}
// Returns nil when the key does not encode a deliverable target.
func ParseDeliveryHintsFromSessionKey(sessionKey string) *DeliveryHints {
	raw := strings.TrimSpace(sessionKey)
	if raw == "" {
		return nil
	}
	parts := splitNonEmpty(raw, ":")
	// Strip leading "agent" prefix.
	if len(parts) >= 3 && parts[0] == "agent" {
		parts = parts[1:]
	}
	if len(parts) < 3 {
		return nil
	}
	channelRaw, kind := parts[0], parts[1]
	to := strings.Join(parts[2:], ":")
	if to == "" {
		return nil
	}
	if !deliverableKinds[kind] {
		return nil
	}
	channel := strings.ToLower(channelRaw)
	if channel == "main" || channel == "subagent" {
		return nil
	}
	return &DeliveryHints{Channel: channel, To: to}
}

func splitNonEmpty(s, sep string) []string {
	raw := strings.Split(s, sep)
	var out []string
	for _, p := range raw {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FormatDirectPushText formats an event as a direct push notification.
func FormatDirectPushText(peerID, eventID, preview, payloadJSON string) string {
	payload := ExtractPayloadFromEnvelope(payloadJSON)
	fullText := extractFullText(payload)
	if fullText == "" {
		fullText = SanitizePreview(preview, 200)
	}
	pushImage := ExtractPushImage(payload)

	lines := []string{"📩 [A2H Market] 来自 " + peerID + ":", "", fullText}

	if pushImage != nil && !strings.Contains(fullText, pushImage.URL) {
		label := "[收款二维码]"
		if pushImage.Type == "image" {
			label = "[图片]"
		}
		lines = append(lines, label+": "+pushImage.URL)
	}

	lines = append(lines, "", "📌 event: "+eventID)
	return strings.Join(lines, "\n")
}
