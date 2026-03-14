package a2a

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/protocol"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// RouterConfig holds per-instance configuration for incoming message routing.
type RouterConfig struct {
	AgentID         string
	MQTTTopicPrefix string // e.g. "P2P_TOPIC" — prefix before /p2p/
	A2ASharedSecret string // empty → skip signature verification
	PushEnabled     bool
	PushTarget      string // default "openclaw"
}

// HandleResult is returned by HandleA2AMessage.
type HandleResult struct {
	Accepted  bool
	Reason    string // non-empty when Accepted=false
	EventID   string
	PeerID    string
	MessageID string
}

// HandleA2AMessage processes a single incoming MQTT message:
//  1. Validate topic is the inbound P2P topic.
//  2. Parse the envelope JSON.
//  3. Verify envelope core fields (protocol, schema_version, required fields).
//  4. Optionally verify HMAC signature.
//  5. Compute deduplication hash.
//  6. Insert into message_event (+ push_outbox if push_enabled).
func HandleA2AMessage(ctx context.Context, es *store.EventStore, cfg RouterConfig, topic, payload string) HandleResult {
	if !isInboundTopic(topic, cfg) {
		return HandleResult{Accepted: false, Reason: "ignored_topic"}
	}

	env, err := protocol.ParseEnvelope(payload)
	if err != nil {
		return HandleResult{Accepted: false, Reason: "invalid_json:" + err.Error()}
	}

	if reason := verifyEnvelopeCore(env); reason != "" {
		return HandleResult{Accepted: false, Reason: "invalid_envelope:" + reason}
	}

	if cfg.A2ASharedSecret != "" {
		if err := protocol.Verify(cfg.A2ASharedSecret, env, 0); err != nil {
			return HandleResult{Accepted: false, Reason: "signature_rejected:" + err.Error()}
		}
	}

	peerID := strings.TrimSpace(env.SenderID)
	if peerID == "" {
		peerID = "unknown"
	}

	messageTs := parseEnvelopeTimestampMs(env.Timestamp)

	preview := extractPreviewFromEnvelope(env)
	hash := ToEventHash(peerID, messageTs, preview, env.MessageID)

	pushTarget := cfg.PushTarget
	if pushTarget == "" {
		pushTarget = "openclaw"
	}

	eventID := newEventID()

	res, err := es.InsertIncomingEvent(ctx, store.InsertEventInput{
		EventID:      eventID,
		PeerID:       peerID,
		MessageID:    env.MessageID,
		MsgTs:        messageTs,
		Hash:         hash,
		UnreadCount:  1,
		Preview:      preview,
		Payload:      env,
		State:        "NEW",
		Source:       "MQTT",
		A2AMessageID: env.MessageID,
		PushEnabled:  cfg.PushEnabled,
		PushTarget:   pushTarget,
	})
	if err != nil {
		return HandleResult{Accepted: false, Reason: "db_error:" + err.Error()}
	}
	if !res.Created {
		return HandleResult{Accepted: false, Reason: "duplicate_message_id"}
	}

	return HandleResult{
		Accepted:  true,
		EventID:   res.EventID,
		PeerID:    peerID,
		MessageID: env.MessageID,
	}
}

// isInboundTopic returns true when the MQTT topic is the agent's inbound P2P topic.
// Topic format: "{MQTTTopicPrefix}/p2p/..."
func isInboundTopic(topic string, cfg RouterConfig) bool {
	prefix := cfg.MQTTTopicPrefix + "/p2p/"
	return strings.HasPrefix(topic, prefix)
}

// verifyEnvelopeCore checks that the required envelope fields are present.
// Returns a non-empty reason string on failure.
func verifyEnvelopeCore(e *protocol.Envelope) string {
	if e.Protocol != protocol.ProtocolName {
		return "protocol_mismatch:" + e.Protocol
	}
	if e.SchemaVersion != protocol.SchemaVersion {
		return "schema_version_mismatch:" + e.SchemaVersion
	}
	if e.MessageID == "" {
		return "missing_message_id"
	}
	if e.SenderID == "" {
		return "missing_sender_id"
	}
	if e.MessageType == "" {
		return "missing_message_type"
	}
	return ""
}

// extractPreviewFromEnvelope computes a human-readable preview from an envelope.
func extractPreviewFromEnvelope(e *protocol.Envelope) string {
	if e == nil {
		return ""
	}
	payload := e.Payload
	if payload == nil {
		payload = make(map[string]interface{})
	}

	// Try direct text fields.
	text, _ := payload["text"].(string)
	if text == "" {
		text, _ = payload["message"].(string)
	}
	if text == "" {
		text, _ = payload["preview"].(string)
	}

	qr := ""
	if v, ok := payload["payment_qr"].(string); ok {
		qr = strings.TrimSpace(v)
	}
	if qr == "" {
		if v, ok := payload["image"].(string); ok {
			qr = strings.TrimSpace(v)
		}
	}

	var att map[string]interface{}
	if a, ok := payload["attachment"].(map[string]interface{}); ok {
		if _, hasURL := a["url"]; hasURL {
			att = a
		}
	}

	attName := ""
	if att != nil {
		attName, _ = att["name"].(string)
		if attName == "" {
			attName = "文件"
		}
	}

	var raw string
	switch {
	case text != "" && qr != "":
		raw = fmt.Sprintf("%s [收款二维码]", text)
	case text != "" && att != nil:
		raw = fmt.Sprintf("%s [附件: %s]", text, attName)
	case text != "":
		raw = text
	case qr != "":
		raw = fmt.Sprintf("[收款二维码] %s", qr)
	case att != nil:
		url, _ := att["url"].(string)
		raw = fmt.Sprintf("[附件: %s] %s", attName, url)
	default:
		// Fallback: JSON of payload.
		raw = fmt.Sprintf("%v", payload)
	}
	return SanitizePreview(raw, 100)
}

// parseEnvelopeTimestampMs parses an envelope timestamp string to Unix milliseconds.
// Returns current time in ms if parsing fails.
func parseEnvelopeTimestampMs(ts string) int64 {
	if ts == "" {
		return time.Now().UnixMilli()
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000+08:00",
		"2006-01-02T15:04:05+08:00",
		time.RFC3339Nano,
	} {
		t, err := time.Parse(layout, ts)
		if err == nil {
			return t.UnixMilli()
		}
	}
	return time.Now().UnixMilli()
}

// newEventID generates a unique event ID in the format expected by the store.
func newEventID() string {
	suffix, err := randomHex6()
	if err != nil {
		suffix = "000000"
	}
	return fmt.Sprintf("a2hmarket_a2a_%d_%s", time.Now().Unix(), suffix)
}
