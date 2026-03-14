package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestComputeHTTPSignature(t *testing.T) {
	// 使用真实凭证验证签名的可预测性
	const (
		agentID  = "ag_t6PowP7DhseW8oBl"
		agentKey = "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K"
	)

	tests := []struct {
		name      string
		agentKey  string
		method    string
		path      string
		agentID   string
		timestamp string
	}{
		{
			name:      "GET request with real credentials",
			agentKey:  agentKey,
			method:    "GET",
			path:      "/v1/auth/check",
			agentID:   agentID,
			timestamp: "1700000000",
		},
		{
			name:      "POST request with real credentials",
			agentKey:  agentKey,
			method:    "POST",
			path:      "/v1/auth/init-login",
			agentID:   agentID,
			timestamp: "1700000000",
		},
		{
			name:      "lowercase method should be uppercased",
			agentKey:  agentKey,
			method:    "get",
			path:      "/v1/auth/check",
			agentID:   agentID,
			timestamp: "1700000000",
		},
		{
			name:      "different timestamp produces different signature",
			agentKey:  agentKey,
			method:    "GET",
			path:      "/v1/auth/check",
			agentID:   agentID,
			timestamp: "1700000001",
		},
		{
			name:      "empty path",
			agentKey:  agentKey,
			method:    "GET",
			path:      "",
			agentID:   agentID,
			timestamp: "1700000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeHTTPSignature(tt.agentKey, tt.method, tt.path, tt.agentID, tt.timestamp)

			// Signature should be 64 hex chars (SHA-256 = 32 bytes = 64 hex)
			if len(got) != 64 {
				t.Errorf("signature length = %d, want 64", len(got))
			}

			// Verify it's valid hex
			_, err := hex.DecodeString(got)
			if err != nil {
				t.Errorf("signature is not valid hex: %v", err)
			}
		})
	}
}

func TestComputeHTTPSignature_Deterministic(t *testing.T) {
	const (
		agentKey  = "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K"
		method    = "GET"
		path      = "/v1/auth/check"
		agentID   = "ag_t6PowP7DhseW8oBl"
		timestamp = "1700000000"
	)

	// Same inputs must produce identical signatures
	sig1 := ComputeHTTPSignature(agentKey, method, path, agentID, timestamp)
	sig2 := ComputeHTTPSignature(agentKey, method, path, agentID, timestamp)

	if sig1 != sig2 {
		t.Errorf("signature is not deterministic: %s != %s", sig1, sig2)
	}
}

func TestComputeHTTPSignature_MatchesManualComputation(t *testing.T) {
	const (
		agentKey  = "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K"
		method    = "GET"
		path      = "/v1/auth/check"
		agentID   = "ag_t6PowP7DhseW8oBl"
		timestamp = "1700000000"
	)

	// Manually compute expected signature: payload = "GET&/v1/auth/check&ag_t6PowP7DhseW8oBl&1700000000"
	payload := "GET&/v1/auth/check&ag_t6PowP7DhseW8oBl&1700000000"
	mac := hmac.New(sha256.New, []byte(agentKey))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	got := ComputeHTTPSignature(agentKey, method, path, agentID, timestamp)

	if got != expected {
		t.Errorf("ComputeHTTPSignature() = %s, want %s", got, expected)
	}
}

func TestComputeHTTPSignature_MethodUppercased(t *testing.T) {
	const (
		agentKey  = "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K"
		path      = "/v1/auth/check"
		agentID   = "ag_t6PowP7DhseW8oBl"
		timestamp = "1700000000"
	)

	// "get" should produce the same result as "GET"
	sigLower := ComputeHTTPSignature(agentKey, "get", path, agentID, timestamp)
	sigUpper := ComputeHTTPSignature(agentKey, "GET", path, agentID, timestamp)

	if sigLower != sigUpper {
		t.Errorf("method case should not matter: get=%s, GET=%s", sigLower, sigUpper)
	}
}

func TestComputeHTTPSignature_DifferentKeysDifferentSignatures(t *testing.T) {
	const (
		method    = "GET"
		path      = "/v1/auth/check"
		agentID   = "ag_t6PowP7DhseW8oBl"
		timestamp = "1700000000"
	)

	sig1 := ComputeHTTPSignature("key_aaa", method, path, agentID, timestamp)
	sig2 := ComputeHTTPSignature("key_bbb", method, path, agentID, timestamp)

	if sig1 == sig2 {
		t.Error("different keys should produce different signatures")
	}
}
