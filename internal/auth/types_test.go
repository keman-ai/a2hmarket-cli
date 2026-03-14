package auth

import (
	"testing"
)

func TestCheckAuthResponse_IsSuccess(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{"200 is success", "200", true},
		{"404 is not success", "404", false},
		{"500 is not success", "500", false},
		{"empty code is not success", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &CheckAuthResponse{Code: tt.code}
			if got := resp.IsSuccess(); got != tt.want {
				t.Errorf("IsSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckAuthResponse_IsAuthorized(t *testing.T) {
	t.Run("authorized with data", func(t *testing.T) {
		resp := &CheckAuthResponse{
			Code:    "200",
			Message: "OK",
			Data: &Credentials{
				AgentID:  "ag_xxx",  // mapped from JSON "agentId"
				AgentKey: "key_xxx", // mapped from JSON "secret"
			},
		}
		if !resp.IsAuthorized() {
			t.Error("IsAuthorized() = false, want true")
		}
	})

	t.Run("pending: code=200 but no data", func(t *testing.T) {
		resp := &CheckAuthResponse{Code: "200", Message: "OK"}
		if resp.IsAuthorized() {
			t.Error("IsAuthorized() = true, want false for pending")
		}
	})

	t.Run("data present but agent_id empty", func(t *testing.T) {
		resp := &CheckAuthResponse{
			Code: "200",
			Data: &Credentials{AgentID: ""},
		}
		if resp.IsAuthorized() {
			t.Error("IsAuthorized() = true, want false when AgentID is empty")
		}
	})

	t.Run("server error", func(t *testing.T) {
		resp := &CheckAuthResponse{Code: "404", Message: "缺少必需参数: code"}
		if resp.IsAuthorized() {
			t.Error("IsAuthorized() = true, want false for error response")
		}
	})
}
