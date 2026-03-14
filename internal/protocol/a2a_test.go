package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCanonicalize verifies the deterministic JSON serialization matches the JS implementation.
func TestCanonicalize(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{
			name:  "null",
			input: nil,
			want:  "null",
		},
		{
			name:  "bool true",
			input: true,
			want:  "true",
		},
		{
			name:  "string",
			input: "hello",
			want:  `"hello"`,
		},
		{
			name:  "number",
			input: float64(42),
			want:  "42",
		},
		{
			name:  "empty object",
			input: map[string]interface{}{},
			want:  "{}",
		},
		{
			name:  "sorted keys",
			input: map[string]interface{}{"z": 1.0, "a": 2.0},
			want:  `{"a":2,"z":1}`,
		},
		{
			name: "nested object keys sorted",
			input: map[string]interface{}{
				"text": "hi",
				"meta": map[string]interface{}{"z": "last", "a": "first"},
			},
			want: `{"meta":{"a":"first","z":"last"},"text":"hi"}`,
		},
		{
			name:  "array",
			input: []interface{}{"b", "a"},
			want:  `["b","a"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalize(tt.input)
			if got != tt.want {
				t.Errorf("canonicalize(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestBuildAndSign verifies envelope construction and signing round-trip.
func TestBuildAndSign(t *testing.T) {
	env, err := BuildEnvelope("ag_sender", "ag_target", "chat.request",
		map[string]interface{}{"text": "hello"})
	if err != nil {
		t.Fatalf("BuildEnvelope: %v", err)
	}

	if env.Protocol != ProtocolName {
		t.Errorf("Protocol = %q, want %q", env.Protocol, ProtocolName)
	}
	if env.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q", env.SchemaVersion)
	}
	if env.SenderID != "ag_sender" || env.TargetID != "ag_target" {
		t.Errorf("SenderID/TargetID mismatch")
	}
	if env.Signature != "" {
		t.Errorf("unsigned envelope should have empty signature")
	}
	if env.PayloadHash == "" {
		t.Errorf("payload_hash must not be empty")
	}

	secret := "test-secret-key"
	signed := Sign(secret, env)
	if signed.Signature == "" {
		t.Error("signed envelope must have non-empty signature")
	}

	// Verify should pass
	if err := Verify(secret, signed, 0); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

// TestVerify_WrongSecret ensures signature mismatch is detected.
func TestVerify_WrongSecret(t *testing.T) {
	env, _ := BuildEnvelope("ag_a", "ag_b", "chat.request",
		map[string]interface{}{"text": "hi"})
	signed := Sign("correct-secret", env)

	err := Verify("wrong-secret", signed, 0)
	if err == nil {
		t.Error("expected verification failure with wrong secret")
	}
	if !strings.Contains(err.Error(), "signature mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestVerify_TamperedPayload ensures payload tampering is detected via payload_hash.
func TestVerify_TamperedPayload(t *testing.T) {
	env, _ := BuildEnvelope("ag_a", "ag_b", "chat.request",
		map[string]interface{}{"text": "original"})
	signed := Sign("secret", env)

	// Tamper with payload after signing
	tampered := *signed
	tampered.Payload = map[string]interface{}{"text": "hacked"}

	err := Verify("secret", &tampered, 0)
	if err == nil {
		t.Error("expected verification failure for tampered payload")
	}
}

// TestSign_IsImmutable ensures Sign does not modify the input envelope.
func TestSign_IsImmutable(t *testing.T) {
	env, _ := BuildEnvelope("ag_a", "ag_b", "chat.request",
		map[string]interface{}{"text": "hi"})

	_ = Sign("secret", env)

	if env.Signature != "" {
		t.Error("original envelope signature was mutated by Sign")
	}
}

// TestParseEnvelope verifies round-trip JSON parsing.
func TestParseEnvelope(t *testing.T) {
	original, _ := BuildEnvelope("ag_sender", "ag_target", "chat.request",
		map[string]interface{}{"text": "round-trip"})
	signed := Sign("key", original)

	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	parsed, err := ParseEnvelope(string(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}

	if parsed.MessageID != signed.MessageID {
		t.Errorf("MessageID mismatch")
	}
	if parsed.Signature != signed.Signature {
		t.Errorf("Signature mismatch")
	}

	// Verify the parsed envelope
	if err := Verify("key", parsed, 0); err != nil {
		t.Errorf("Verify after parse: %v", err)
	}
}
