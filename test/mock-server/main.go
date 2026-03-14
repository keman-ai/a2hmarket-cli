package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// 固定凭证
const (
	AgentID  = "ag_t6PowP7DhseW8oBl"
	AgentKey = "GdLTcvnUbwyDbxZlAy6DKHAa5EeVrN5K"
	APIURL   = "https://api.a2hmarket.ai"
	MQTTURL  = "mqtts://mqtt.a2hmarket.ai:8883"
	ExpireAt = "2027-12-31T23:59:59Z"
)

// MockServer Mock 服务器
type MockServer struct {
	mu         sync.Mutex
	authStatus string // "pending" or "authorized"
	requestLog []RequestLog
	delay      time.Duration
	serverIP   string
}

// RequestLog 请求日志
type RequestLog struct {
	Path      string    `json:"path"`
	Method    string    `json:"method"`
	Timestamp string    `json:"timestamp"`
	Params    string    `json:"params,omitempty"`
}

// InitLoginResponse 初始登录响应
type InitLoginResponse struct {
	Code string `json:"code"`
	URL  string `json:"url"`
}

// CheckResponsePending check 接口 pending 状态响应
type CheckResponsePending struct {
	Status string `json:"status"`
}

// CheckResponseAuthorized check 接口 authorized 状态响应
type CheckResponseAuthorized struct {
	Status   string `json:"status"`
	AgentID  string `json:"agent_id"`
	AgentKey string `json:"agent_key"`
	APIURL   string `json:"api_url"`
	MQTTURL  string `json:"mqtt_url"`
	ExpireAt string `json:"expire_at"`
}

// StatusChangeRequest 状态切换请求
type StatusChangeRequest struct {
	Status string `json:"status"`
}

// NewMockServer 创建新的 Mock 服务器
func NewMockServer() *MockServer {
	return &MockServer{
		authStatus: "pending",
		requestLog: make([]RequestLog, 0),
		delay:      0,
	}
}

// logRequest 记录请求
func (s *MockServer) logRequest(r *http.Request, params string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	logEntry := RequestLog{
		Path:      r.URL.Path,
		Method:    r.Method,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if params != "" {
		logEntry.Params = params
	}
	s.requestLog = append(s.requestLog, logEntry)

	log.Printf("[%s] %s %s %s", logEntry.Method, logEntry.Path, logEntry.Timestamp, logEntry.Params)
}

// applyDelay 应用延迟
func (s *MockServer) applyDelay() {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
}

// handleInitLogin 处理 /v1/auth/init-login 请求
func (s *MockServer) handleInitLogin(w http.ResponseWriter, r *http.Request) {
	s.applyDelay()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析表单参数
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	timestamp := r.FormValue("timestamp")
	mac := r.FormValue("mac")
	feishuUserID := r.FormValue("feishu_user_id")

	params := fmt.Sprintf("timestamp=%s, mac=%s, feishu_user_id=%s", timestamp, mac, feishuUserID)
	s.logRequest(r, params)

	// 生成固定 auth code
	authCode := "auth_code_12345"

	// 构建登录 URL
	loginURL := fmt.Sprintf("http://%s/login?code=%s", s.serverIP, authCode)

	response := InitLoginResponse{
		Code: authCode,
		URL:  loginURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCheck 处理 /v1/auth/check 请求
func (s *MockServer) handleCheck(w http.ResponseWriter, r *http.Request) {
	s.applyDelay()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.URL.Query().Get("code")
	s.logRequest(r, fmt.Sprintf("code=%s", code))

	s.mu.Lock()
	status := s.authStatus
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if status == "pending" {
		response := CheckResponsePending{
			Status: "pending",
		}
		json.NewEncoder(w).Encode(response)
	} else {
		response := CheckResponseAuthorized{
			Status:   "authorized",
			AgentID:  AgentID,
			AgentKey: AgentKey,
			APIURL:   APIURL,
			MQTTURL:  MQTTURL,
			ExpireAt: ExpireAt,
		}
		json.NewEncoder(w).Encode(response)
	}
}

// handleLogin 处理 /login 请求 (模拟浏览器登录页面)
func (s *MockServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	s.applyDelay()

	code := r.URL.Query().Get("code")
	s.logRequest(r, fmt.Sprintf("code=%s (login page)", code))

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Mock Login</title></head>
<body>
<h1>Mock Login Page</h1>
<p>Auth Code: %s</p>
<p>This is a mock login page for testing purposes.</p>
</body>
</html>`, code)
}

// handleStatus 处理状态切换请求 PUT /v1/auth/status
func (s *MockServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.applyDelay()

	s.logRequest(r, "")

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		status := s.authStatus
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": status})

	case http.MethodPut:
		var req StatusChangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Failed to decode request: %v", err), http.StatusBadRequest)
			return
		}

		if req.Status != "pending" && req.Status != "authorized" {
			http.Error(w, "Invalid status. Must be 'pending' or 'authorized'", http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		s.authStatus = req.Status
		s.mu.Unlock()

		log.Printf("[STATUS] Changed to: %s", req.Status)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": req.Status})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLogs 处理日志查询请求 GET /v1/logs
func (s *MockServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	s.applyDelay()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.logRequest(r, "")

	s.mu.Lock()
	logs := s.requestLog
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total": len(logs),
		"logs":  logs,
	})
}

// handleHealth 处理健康检查请求
func (s *MockServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.applyDelay()
	s.logRequest(r, "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// verifySignature 验证请求签名
func (s *MockServer) verifySignature(r *http.Request) bool {
	agentID := r.Header.Get("X-Agent-Id")
	timestamp := r.Header.Get("X-Timestamp")
	signature := r.Header.Get("X-Agent-Signature")

	if agentID == "" || timestamp == "" || signature == "" {
		return false
	}

	// 获取请求方法和路径
	method := r.Method
	path := r.URL.Path

	// 构建 payload: METHOD&path&agentId&timestamp
	payload := fmt.Sprintf("%s&%s&%s&%s", method, path, agentID, timestamp)

	// 计算期望签名
	mac := hmac.New(sha256.New, []byte(AgentKey))
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	return signature == expectedSig
}

// handleAPI 处理通用 API 请求（验证签名）
func (s *MockServer) handleAPI(w http.ResponseWriter, r *http.Request) {
	s.applyDelay()

	// 记录请求
	s.logRequest(r, fmt.Sprintf("headers=%v", r.Header))

	// 验证签名
	if !s.verifySignature(r) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    "401",
			"message": "invalid signature",
			"data":    nil,
		})
		return
	}

	// 签名验证通过，返回成功响应
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    "200",
		"message": "success",
		"data": map[string]interface{}{
			"path":      r.URL.Path,
			"method":    r.Method,
			"agent_id":  r.Header.Get("X-Agent-Id"),
			"timestamp": r.Header.Get("X-Timestamp"),
		},
	})
}

// Run 启动服务器
func (s *MockServer) Run(port int, serverIP string) error {
	s.serverIP = serverIP

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/init-login", s.handleInitLogin)
	mux.HandleFunc("/v1/auth/check", s.handleCheck)
	mux.HandleFunc("/v1/auth/status", s.handleStatus)
	mux.HandleFunc("/v1/logs", s.handleLogs)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/health", s.handleHealth)
	// 通用 API 端点 - 验证签名
	mux.HandleFunc("/api/", s.handleAPI)
	mux.HandleFunc("/findu-", s.handleAPI) // 处理 findu-user, findu-match 等前缀

	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start server on %s: %w", addr, err)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Printf("Mock server starting on http://%s", listener.Addr().String())
	log.Printf("Endpoints:")
	log.Printf("  POST   /v1/auth/init-login - Init login")
	log.Printf("  GET    /v1/auth/check      - Check auth status")
	log.Printf("  GET    /v1/auth/status     - Get current status")
	log.Printf("  PUT    /v1/auth/status     - Change auth status")
	log.Printf("  GET    /v1/logs            - Get request logs")
	log.Printf("  GET    /login              - Mock login page")
	log.Printf("  GET    /health             - Health check")
	log.Printf("  ANY    /api/*              - Generic API endpoint (requires signature)")
	log.Printf("  ANY    /findu-*/*          - Generic API endpoint (requires signature)")
	log.Printf("Current auth status: %s", s.authStatus)
	log.Printf("Server IP: %s", s.serverIP)

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// findAvailablePort 查找可用端口
func findAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

func main() {
	var (
		port      int
		serverIP  string
		delay     int
		status    string
	)

	flag.IntVar(&port, "port", 0, "Server port (0 for random available port)")
	flag.StringVar(&serverIP, "ip", "localhost", "Server IP for generating URLs")
	flag.IntVar(&delay, "delay", 0, "Simulated network delay in milliseconds")
	flag.StringVar(&status, "status", "pending", "Initial auth status (pending or authorized)")

	flag.Parse()

	// 如果没有指定端口，自动查找可用端口
	if port == 0 {
		var err error
		port, err = findAvailablePort()
		if err != nil {
			log.Fatalf("Failed to find available port: %v", err)
		}
	}

	server := NewMockServer()
	server.delay = time.Duration(delay) * time.Millisecond
	server.authStatus = status

	if err := server.Run(port, serverIP); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
