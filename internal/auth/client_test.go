package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/keman-ai/a2hmarket-cli/internal/config"
)

func newTestClient(serverURL string) *Client {
	cfg := &config.Config{
		BaseURL:     serverURL,
		AuthTimeout: 5,
	}
	return NewClient(cfg)
}

// TestInitLogin_Success 正常返回 code 和 url
func TestInitLogin_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/auth/init-login" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InitLoginResponse{
			Code: "auth_code_12345",
			URL:  "http://localhost/login?code=auth_code_12345",
		})
	}))
	defer ts.Close()

	client := newTestClient(ts.URL)
	resp, err := client.InitLogin(&InitLoginRequest{
		Timestamp:    1700000000,
		MAC:          "aa:bb:cc:dd:ee:ff",
		FeishuUserID: "ou_test123",
	})
	if err != nil {
		t.Fatalf("InitLogin() error = %v", err)
	}
	if resp.Code != "auth_code_12345" {
		t.Errorf("Code = %q, want %q", resp.Code, "auth_code_12345")
	}
	if resp.URL == "" {
		t.Error("URL should not be empty")
	}
}

// TestInitLogin_ErrorInResponse 服务端返回业务错误
func TestInitLogin_ErrorInResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InitLoginResponse{
			Error: "invalid mac address",
		})
	}))
	defer ts.Close()

	client := newTestClient(ts.URL)
	_, err := client.InitLogin(&InitLoginRequest{
		Timestamp: 1700000000,
		MAC:       "",
	})
	if err == nil {
		t.Fatal("InitLogin() should return error when response contains Error field")
	}
}

// TestInitLogin_InvalidJSON 服务端返回非 JSON 响应
func TestInitLogin_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer ts.Close()

	client := newTestClient(ts.URL)
	_, err := client.InitLogin(&InitLoginRequest{Timestamp: 1700000000, MAC: "aa:bb:cc:dd:ee:ff"})
	if err == nil {
		t.Fatal("InitLogin() should return error for invalid JSON response")
	}
}

// TestCheckAuth_Pending pending 状态：code=200，无 data
func TestCheckAuth_Pending(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CheckAuthResponse{Code: "200", Message: "OK"})
	}))
	defer ts.Close()

	client := newTestClient(ts.URL)
	resp, err := client.CheckAuth("some_code")
	if err != nil {
		t.Fatalf("CheckAuth() error = %v", err)
	}
	if !resp.IsSuccess() {
		t.Errorf("IsSuccess() = false, want true")
	}
	if resp.IsAuthorized() {
		t.Error("IsAuthorized() = true, want false for pending state")
	}
}

// TestCheckAuth_Authorized authorized 状态：code=200，data 含完整凭证
func TestCheckAuth_Authorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CheckAuthResponse{
			Code:    "200",
			Message: "OK",
			Data: &Credentials{
				AgentID:  "ag_t6PowP7DhseW8oBl",
				AgentKey: "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K",
				APIURL:   "https://api.a2hmarket.ai",
				MQTTURL:  "mqtts://mqtt.a2hmarket.ai:8883",
				ExpireAt: "2027-12-31T23:59:59Z",
			},
		})
	}))
	defer ts.Close()

	client := newTestClient(ts.URL)
	resp, err := client.CheckAuth("auth_code_12345")
	if err != nil {
		t.Fatalf("CheckAuth() error = %v", err)
	}
	if !resp.IsAuthorized() {
		t.Error("IsAuthorized() = false, want true")
	}
	if resp.Data.AgentID != "ag_t6PowP7DhseW8oBl" {
		t.Errorf("AgentID = %q, want %q", resp.Data.AgentID, "ag_t6PowP7DhseW8oBl")
	}
	if resp.Data.AgentKey != "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K" {
		t.Errorf("AgentKey = %q, want %q", resp.Data.AgentKey, "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K")
	}
	if resp.Data.APIURL != "https://api.a2hmarket.ai" {
		t.Errorf("APIURL = %q, want %q", resp.Data.APIURL, "https://api.a2hmarket.ai")
	}
	if resp.Data.ExpireAt != "2027-12-31T23:59:59Z" {
		t.Errorf("ExpireAt = %q, want %q", resp.Data.ExpireAt, "2027-12-31T23:59:59Z")
	}
}

// TestCheckAuth_ServerError 服务器返回非 200 code
func TestCheckAuth_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CheckAuthResponse{Code: "404", Message: "缺少必需参数: code"})
	}))
	defer ts.Close()

	client := newTestClient(ts.URL)
	resp, err := client.CheckAuth("code")
	if err != nil {
		t.Fatalf("CheckAuth() error = %v", err)
	}
	if resp.IsSuccess() {
		t.Error("IsSuccess() = true, want false for 404 response")
	}
}

// TestCheckAuth_NetworkError 网络错误
func TestCheckAuth_NetworkError(t *testing.T) {
	// 使用不存在的地址
	client := newTestClient("http://127.0.0.1:1")
	_, err := client.CheckAuth("code")
	if err == nil {
		t.Fatal("CheckAuth() should return error for network failure")
	}
}

// TestInitLogin_NetworkError 网络错误
func TestInitLogin_NetworkError(t *testing.T) {
	client := newTestClient("http://127.0.0.1:1")
	_, err := client.InitLogin(&InitLoginRequest{Timestamp: 1700000000, MAC: "aa:bb:cc:dd:ee:ff"})
	if err == nil {
		t.Fatal("InitLogin() should return error for network failure")
	}
}

// TestCheckAuth_InvalidJSON 无效 JSON 响应
func TestCheckAuth_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid json {{"))
	}))
	defer ts.Close()

	client := newTestClient(ts.URL)
	_, err := client.CheckAuth("code")
	if err == nil {
		t.Fatal("CheckAuth() should return error for invalid JSON")
	}
}
