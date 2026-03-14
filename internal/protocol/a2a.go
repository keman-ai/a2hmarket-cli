// Package protocol implements the A2A (Agent-to-Agent) envelope format.
//
// Envelope signature algorithm mirrors runtime/js/src/protocol/a2a-protocol.js:
//
//  1. Canonicalize payload (JSON keys sorted recursively).
//  2. Compute SHA-256 of canonicalized payload → payload_hash.
//  3. Remove signature field, canonicalize entire envelope object → signing payload.
//  4. signature = HMAC-SHA256(agentKey, signingPayload).hex()
package protocol

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	ProtocolName  = "a2hmarket-a2a"
	SchemaVersion = "1.0.0"
)

// Envelope is an A2A message envelope.
type Envelope struct {
	Protocol      string                 `json:"protocol"`
	SchemaVersion string                 `json:"schema_version"`
	MessageType   string                 `json:"message_type"`
	MessageID     string                 `json:"message_id"`
	TraceID       string                 `json:"trace_id"`
	SenderID      string                 `json:"sender_id"`
	TargetID      string                 `json:"target_id"`
	Timestamp     string                 `json:"timestamp"`
	Nonce         string                 `json:"nonce"`
	Payload       map[string]interface{} `json:"payload"`
	PayloadHash   string                 `json:"payload_hash"`
	Signature     string                 `json:"signature"`
}

// BuildEnvelope creates a new unsigned envelope.
func BuildEnvelope(senderID, targetID, messageType string, payload map[string]interface{}) (*Envelope, error) {
	if payload == nil {
		payload = make(map[string]interface{})
	}

	msgID, err := randomID("msg")
	if err != nil {
		return nil, fmt.Errorf("a2a: random msgID: %w", err)
	}
	traceID, err := randomID("trace")
	if err != nil {
		return nil, fmt.Errorf("a2a: random traceID: %w", err)
	}
	nonce, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("a2a: random nonce: %w", err)
	}

	payloadHash := sha256HexOf(canonicalize(payload))

	return &Envelope{
		Protocol:      ProtocolName,
		SchemaVersion: SchemaVersion,
		MessageType:   messageType,
		MessageID:     msgID,
		TraceID:       traceID,
		SenderID:      senderID,
		TargetID:      targetID,
		Timestamp:     beijingTimeISO(),
		Nonce:         nonce,
		Payload:       payload,
		PayloadHash:   payloadHash,
		Signature:     "",
	}, nil
}

// Sign returns a signed copy of the envelope.
// The original envelope is not modified.
func Sign(agentKey string, e *Envelope) *Envelope {
	unsigned := *e
	unsigned.Signature = ""
	sig := envelopeHMAC(agentKey, &unsigned)
	signed := unsigned
	signed.Signature = sig
	return &signed
}

// VerifyError is returned when envelope verification fails.
type VerifyError struct {
	Reason string
}

func (e *VerifyError) Error() string {
	return "a2a verify: " + e.Reason
}

// Verify checks the envelope's protocol fields, timestamp, payload_hash and signature.
// toleranceMs is the allowed clock skew in milliseconds (0 defaults to 5 minutes).
func Verify(agentKey string, e *Envelope, toleranceMs int64) error {
	if e.Protocol != ProtocolName {
		return &VerifyError{Reason: "protocol mismatch: " + e.Protocol}
	}
	if e.SchemaVersion != SchemaVersion {
		return &VerifyError{Reason: "schema_version mismatch: " + e.SchemaVersion}
	}
	if e.MessageID == "" || e.SenderID == "" || e.MessageType == "" {
		return &VerifyError{Reason: "missing required fields"}
	}

	tol := toleranceMs
	if tol <= 0 {
		tol = 5 * 60 * 1000
	}

	ts, err := parseEnvelopeTime(e.Timestamp)
	if err != nil {
		return &VerifyError{Reason: "invalid timestamp: " + e.Timestamp}
	}
	diffMs := math.Abs(float64(time.Now().UnixMilli() - ts.UnixMilli()))
	if diffMs > float64(tol) {
		return &VerifyError{Reason: "timestamp out of tolerance"}
	}

	expectedHash := sha256HexOf(canonicalize(e.Payload))
	if expectedHash != e.PayloadHash {
		return &VerifyError{Reason: "payload_hash mismatch"}
	}

	unsigned := *e
	unsigned.Signature = ""
	expected := envelopeHMAC(agentKey, &unsigned)
	if expected != e.Signature {
		return &VerifyError{Reason: "signature mismatch"}
	}

	return nil
}

// ParseEnvelope parses a raw JSON payload into an Envelope.
func ParseEnvelope(raw string) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return nil, fmt.Errorf("a2a: parse envelope: %w", err)
	}
	return &e, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// canonicalize produces a deterministic JSON-like string from any value,
// matching the JS implementation in a2a-protocol.js.
func canonicalize(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		raw, _ := json.Marshal(val)
		return string(raw)
	case string:
		raw, _ := json.Marshal(val)
		return string(raw)
	case []interface{}:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonicalize(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, len(keys))
		for i, k := range keys {
			keyJSON, _ := json.Marshal(k)
			pairs[i] = string(keyJSON) + ":" + canonicalize(val[k])
		}
		return "{" + strings.Join(pairs, ",") + "}"
	default:
		raw, _ := json.Marshal(val)
		return string(raw)
	}
}

// envelopeAsMap converts an Envelope to map[string]interface{} for canonicalization.
// This replicates the JS behaviour of spreading the envelope object minus the signature.
func envelopeAsMap(e *Envelope) map[string]interface{} {
	// Use JSON round-trip to convert struct → generic map
	data, _ := json.Marshal(e)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	delete(m, "signature")
	return m
}

func envelopeHMAC(secret string, e *Envelope) string {
	m := envelopeAsMap(e)
	payload := canonicalize(m)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha256HexOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomID(prefix string) (string, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), suffix), nil
}

// beijingTimeISO formats the current time as UTC+8 ISO8601, matching the JS implementation.
func beijingTimeISO() string {
	now := time.Now().UTC()
	beijing := now.Add(8 * time.Hour)
	return beijing.Format("2006-01-02T15:04:05.000+08:00")
}

// parseEnvelopeTime parses the non-standard "+08:00" timestamp used in envelopes.
func parseEnvelopeTime(ts string) (time.Time, error) {
	// Try RFC3339 first (handles +08:00 suffix correctly on Go 1.20+)
	t, err := time.Parse(time.RFC3339, ts)
	if err == nil {
		return t, nil
	}
	// Fallback: strip the offset and parse as UTC, then adjust
	t, err = time.Parse("2006-01-02T15:04:05.000", strings.TrimSuffix(ts, "+08:00"))
	if err == nil {
		return t.Add(-8 * time.Hour), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", ts)
}
