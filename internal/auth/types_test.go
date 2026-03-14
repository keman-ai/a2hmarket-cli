package auth

import (
	"testing"
)

func TestCheckAuthResponse_GetCredentials_WithNestedCredentials(t *testing.T) {
	nested := &Credentials{
		AgentID:  "ag_nested_id",
		AgentKey: "nested_key",
		APIURL:   "https://api.nested.example.com",
		MQTTURL:  "mqtts://mqtt.nested.example.com:8883",
		ExpireAt: "2027-12-31T23:59:59Z",
	}

	resp := &CheckAuthResponse{
		Status:      "authorized",
		Credentials: nested,
		AgentID:     "ag_flat_id",
		AgentKey:    "flat_key",
		APIURL:      "https://api.flat.example.com",
		MQTTURL:     "mqtts://mqtt.flat.example.com:8883",
		ExpireAt:    "2028-01-01T00:00:00Z",
	}

	got := resp.GetCredentials()

	// Should return the nested credentials, not the flat fields
	if got.AgentID != "ag_nested_id" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "ag_nested_id")
	}
	if got.AgentKey != "nested_key" {
		t.Errorf("AgentKey = %q, want %q", got.AgentKey, "nested_key")
	}
	if got.APIURL != "https://api.nested.example.com" {
		t.Errorf("APIURL = %q, want %q", got.APIURL, "https://api.nested.example.com")
	}
	if got.MQTTURL != "mqtts://mqtt.nested.example.com:8883" {
		t.Errorf("MQTTURL = %q, want %q", got.MQTTURL, "mqtts://mqtt.nested.example.com:8883")
	}
	if got.ExpireAt != "2027-12-31T23:59:59Z" {
		t.Errorf("ExpireAt = %q, want %q", got.ExpireAt, "2027-12-31T23:59:59Z")
	}
}

func TestCheckAuthResponse_GetCredentials_WithNilCredentials(t *testing.T) {
	resp := &CheckAuthResponse{
		Status:      "authorized",
		Credentials: nil,
		AgentID:     "ag_t6PowP7DhseW8oBl",
		AgentKey:    "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K",
		APIURL:      "https://api.a2hmarket.ai",
		MQTTURL:     "mqtts://mqtt.a2hmarket.ai:8883",
		ExpireAt:    "2027-12-31T23:59:59Z",
	}

	got := resp.GetCredentials()

	// Should fall back to flat structure fields
	if got.AgentID != "ag_t6PowP7DhseW8oBl" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "ag_t6PowP7DhseW8oBl")
	}
	if got.AgentKey != "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K" {
		t.Errorf("AgentKey = %q, want %q", got.AgentKey, "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K")
	}
	if got.APIURL != "https://api.a2hmarket.ai" {
		t.Errorf("APIURL = %q, want %q", got.APIURL, "https://api.a2hmarket.ai")
	}
	if got.MQTTURL != "mqtts://mqtt.a2hmarket.ai:8883" {
		t.Errorf("MQTTURL = %q, want %q", got.MQTTURL, "mqtts://mqtt.a2hmarket.ai:8883")
	}
	if got.ExpireAt != "2027-12-31T23:59:59Z" {
		t.Errorf("ExpireAt = %q, want %q", got.ExpireAt, "2027-12-31T23:59:59Z")
	}
}

func TestCheckAuthResponse_GetCredentials_EmptyFlat(t *testing.T) {
	resp := &CheckAuthResponse{
		Status:      "pending",
		Credentials: nil,
	}

	got := resp.GetCredentials()

	// Should return a Credentials with all empty fields
	if got == nil {
		t.Fatal("GetCredentials() should never return nil")
	}
	if got.AgentID != "" {
		t.Errorf("AgentID = %q, want empty", got.AgentID)
	}
	if got.AgentKey != "" {
		t.Errorf("AgentKey = %q, want empty", got.AgentKey)
	}
}

func TestAuthStatus_Constants(t *testing.T) {
	tests := []struct {
		status AuthStatus
		want   string
	}{
		{AuthStatusPending, "pending"},
		{AuthStatusAuthorized, "authorized"},
		{AuthStatusExpired, "expired"},
		{AuthStatusUsed, "used"},
		{AuthStatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("AuthStatus = %q, want %q", string(tt.status), tt.want)
			}
		})
	}
}
