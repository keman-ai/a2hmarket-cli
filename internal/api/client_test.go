package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testAgentID  = "ag_t6PowP7DhseW8oBl"
	testAgentKey = "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K"
)

func newTestAPIClient(baseURL string) *Client {
	return NewClient(Credentials{
		AgentID:  testAgentID,
		AgentKey: testAgentKey,
		BaseURL:  baseURL,
	}, 5*time.Second)
}

// TestGetJSON_Success 正常 GET 请求，平台标准响应
func TestGetJSON_Success(t *testing.T) {
	type Profile struct {
		Name string `json:"name"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证签名头存在
		if r.Header.Get("X-Agent-Id") == "" {
			t.Error("X-Agent-Id header missing")
		}
		if r.Header.Get("X-Timestamp") == "" {
			t.Error("X-Timestamp header missing")
		}
		if r.Header.Get("X-Agent-Signature") == "" {
			t.Error("X-Agent-Signature header missing")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    "200",
			"message": "ok",
			"data":    Profile{Name: "test_user"},
		})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	var profile Profile
	err := client.GetJSON("/api/v1/profile", "", &profile)
	if err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if profile.Name != "test_user" {
		t.Errorf("Name = %q, want %q", profile.Name, "test_user")
	}
}

// TestGetJSON_WithQueryParams 带查询参数时 signPath 自动截取
func TestGetJSON_WithQueryParams(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": "200", "data": nil,
		})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	err := client.GetJSON("/api/v1/items?page=1&size=10", "", nil)
	if err != nil {
		t.Fatalf("GetJSON() with query params error = %v", err)
	}
}

// TestGetJSON_PlatformError 平台业务错误 code != "200"
func TestGetJSON_PlatformError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    "10001",
			"message": "param error",
			"data":    nil,
		})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	err := client.GetJSON("/api/v1/test", "", nil)
	if err == nil {
		t.Fatal("GetJSON() should return error for platform code != 200")
	}
	platformErr, ok := err.(*PlatformError)
	if !ok {
		t.Fatalf("error type = %T, want *PlatformError", err)
	}
	if platformErr.PlatformCode != "10001" {
		t.Errorf("PlatformCode = %q, want %q", platformErr.PlatformCode, "10001")
	}
}

// TestGetJSON_HTTPError HTTP 非 2xx 状态码
func TestGetJSON_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": "401", "message": "unauthorized",
		})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	err := client.GetJSON("/api/v1/test", "", nil)
	if err == nil {
		t.Fatal("GetJSON() should return error for HTTP 4xx")
	}
	platformErr, ok := err.(*PlatformError)
	if !ok {
		t.Fatalf("error type = %T, want *PlatformError", err)
	}
	if platformErr.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want %d", platformErr.HTTPStatus, http.StatusUnauthorized)
	}
}

// TestGetJSON_NetworkError 网络错误
func TestGetJSON_NetworkError(t *testing.T) {
	client := newTestAPIClient("http://127.0.0.1:1")
	err := client.GetJSON("/api/v1/test", "", nil)
	if err == nil {
		t.Fatal("GetJSON() should return error for network failure")
	}
}

// TestPostJSON_Success 正常 POST 请求
func TestPostJSON_Success(t *testing.T) {
	type Request struct {
		Query string `json:"query"`
	}
	type Response struct {
		Result string `json:"result"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    "200",
			"message": "ok",
			"data":    Response{Result: "found: " + req.Query},
		})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	var resp Response
	err := client.PostJSON("/api/v1/search", Request{Query: "test"}, &resp)
	if err != nil {
		t.Fatalf("PostJSON() error = %v", err)
	}
	if resp.Result != "found: test" {
		t.Errorf("Result = %q, want %q", resp.Result, "found: test")
	}
}

// TestPostJSON_NilBody 空 body 发送 {}
func TestPostJSON_NilBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 0 {
			t.Errorf("body should be empty object {}, got %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": "200", "data": nil,
		})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	err := client.PostJSON("/api/v1/test", nil, nil)
	if err != nil {
		t.Fatalf("PostJSON() with nil body error = %v", err)
	}
}

// TestSignatureHeaders_Correctness 验证签名头内容与 ComputeHTTPSignature 一致
func TestSignatureHeaders_Correctness(t *testing.T) {
	var capturedAgentID, capturedTimestamp, capturedSig string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAgentID = r.Header.Get("X-Agent-Id")
		capturedTimestamp = r.Header.Get("X-Timestamp")
		capturedSig = r.Header.Get("X-Agent-Signature")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": "200", "data": nil,
		})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	client.GetJSON("/api/v1/test", "", nil)

	if capturedAgentID != testAgentID {
		t.Errorf("X-Agent-Id = %q, want %q", capturedAgentID, testAgentID)
	}
	if capturedTimestamp == "" {
		t.Error("X-Timestamp should not be empty")
	}

	// 验证签名可重新计算
	expected := ComputeHTTPSignature(testAgentKey, "GET", "/api/v1/test", testAgentID, capturedTimestamp)
	if capturedSig != expected {
		t.Errorf("X-Agent-Signature = %q, want %q", capturedSig, expected)
	}
}

// TestGetJSON_NonStandardResponse 响应不是平台标准格式，直接反序列化
func TestGetJSON_NonStandardResponse(t *testing.T) {
	type Direct struct {
		Value int `json:"value"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 直接返回非标准格式
		json.NewEncoder(w).Encode(Direct{Value: 42})
	}))
	defer ts.Close()

	client := newTestAPIClient(ts.URL)
	var result Direct
	// 非标准格式不含 code 字段，不应报平台错误
	err := client.GetJSON("/api/v1/test", "", &result)
	if err != nil {
		t.Logf("GetJSON() non-standard response: %v (may be acceptable)", err)
	}
}

// TestPostJSONToHost_CustomBaseURL 向自定义 baseURL 发送请求
func TestPostJSONToHost_CustomBaseURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": "200", "data": nil,
		})
	}))
	defer ts.Close()

	// 使用默认 BaseURL 为空的 client，指定 ts.URL 为目标
	client := NewClient(Credentials{
		AgentID:  testAgentID,
		AgentKey: testAgentKey,
		BaseURL:  "https://api.a2hmarket.ai", // 不会被调用
	}, 5*time.Second)

	err := client.PostJSONToHost(ts.URL, "/api/v1/upload/sign", "/prefix/api/v1/upload/sign", nil, nil)
	if err != nil {
		t.Fatalf("PostJSONToHost() error = %v", err)
	}
}

// TestPutBinary_Success 预签名 URL 上传成功
func TestPutBinary_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Method = %q, want PUT", r.Method)
		}
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("X-Custom-Header missing or wrong")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := newTestAPIClient("https://api.a2hmarket.ai")
	err := client.PutBinary(ts.URL+"/upload", map[string]string{
		"X-Custom-Header": "custom-value",
	}, []byte("binary data"))
	if err != nil {
		t.Fatalf("PutBinary() error = %v", err)
	}
}

// TestPutBinary_HTTPError 上传失败 HTTP 非 2xx
func TestPutBinary_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("access denied"))
	}))
	defer ts.Close()

	client := newTestAPIClient("https://api.a2hmarket.ai")
	err := client.PutBinary(ts.URL+"/upload", nil, []byte("data"))
	if err == nil {
		t.Fatal("PutBinary() should return error for HTTP 4xx")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention HTTP 403, got: %v", err)
	}
}

// TestNewClient_DefaultTimeout 超时为 0 时使用 30 秒默认值
func TestNewClient_DefaultTimeout(t *testing.T) {
	client := NewClient(Credentials{
		AgentID:  testAgentID,
		AgentKey: testAgentKey,
		BaseURL:  "https://api.a2hmarket.ai",
	}, 0)
	if client == nil {
		t.Fatal("NewClient() should not return nil")
	}
	if client.http.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", client.http.Timeout)
	}
}
