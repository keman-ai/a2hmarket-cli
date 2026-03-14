// Package api 提供对 api.a2hmarket.ai 各业务接口的带签名 HTTP 客户端。
//
// # 鉴权方式
//
// 每个请求在 Header 中携带三个字段：
//
//	X-Agent-Id:        agentID
//	X-Timestamp:       Unix 秒级时间戳（字符串）
//	X-Agent-Signature: HMAC-SHA256(agentKey, "METHOD&path&agentId&timestamp").hex()
//
// 签名路径（signPath）使用不含查询参数的纯路径，当 apiPath 含 query string 时需显式传入。
//
// # 平台响应格式
//
// 平台统一响应体：{ "code": "200", "message": "...", "data": <any> }
// code 非 "200" 视为业务错误，返回 *PlatformError。
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Credentials 是调用平台 API 所需的凭证，由鉴权流程获得后传入。
type Credentials struct {
	AgentID   string
	AgentKey  string
	BaseURL   string // 例如 "https://api.a2hmarket.ai"（不含末尾斜杠）
}

// Client 是平台 HTTP API 客户端，持有凭证和共享 http.Client。
type Client struct {
	creds  Credentials
	http   *http.Client
}

// NewClient 创建 API 客户端。timeout 为 0 时默认 30 秒。
func NewClient(creds Credentials, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		creds: creds,
		http:  &http.Client{Timeout: timeout},
	}
}

// GetJSON 发起带签名的 GET 请求，将平台 data 字段反序列化到 dest。
//
// apiPath 可含查询参数（如 "/findu-user/api/v1/user/works/public?type=3&page=1"）。
// signPath 为签名用的纯路径（不含 query），传空字符串时自动截取 apiPath 中 '?' 之前的部分。
func (c *Client) GetJSON(apiPath, signPath string, dest interface{}) error {
	return c.doRequest("GET", c.creds.BaseURL, apiPath, signPath, nil, dest)
}

// PostJSON 发起带签名的 POST 请求（Content-Type: application/json）。
// body 为任意可 JSON 序列化的结构体或 map，传 nil 时发送空对象 {}。
func (c *Client) PostJSON(apiPath string, body interface{}, dest interface{}) error {
	return c.doRequest("POST", c.creds.BaseURL, apiPath, "", body, dest)
}

// PostJSONToHost 向指定 baseURL（不同于凭证中的 BaseURL）发起带签名的 POST 请求。
//
// 用于访问独立域名的服务（如 OSS 签名服务），签名路径需带服务前缀以匹配目标服务的约定：
//
//	baseURL  = "https://api.a2hmarket.ai/findu-oss"
//	apiPath  = "/api/v1/oss_signurl/upload/sign"
//	signPath = "/findu-oss/api/v1/oss_signurl/upload/sign"
func (c *Client) PostJSONToHost(baseURL, apiPath, signPath string, body interface{}, dest interface{}) error {
	return c.doRequest("POST", strings.TrimRight(baseURL, "/"), apiPath, signPath, body, dest)
}

// DeleteJSON 发起带签名的 DELETE 请求，将平台 data 字段反序列化到 dest。
// dest 为 nil 时忽略响应体。
func (c *Client) DeleteJSON(apiPath string, dest interface{}) error {
	return c.doRequest("DELETE", c.creds.BaseURL, apiPath, "", nil, dest)
}

// PutBinary 向预签名 URL 直传二进制数据（不走业务签名，直接使用服务端返回的 signedHeaders）。
//
// 典型用途：OSS 预签名直传。uploadURL 为完整预签名地址，signedHeaders 为服务端返回的额外请求头。
func (c *Client) PutBinary(uploadURL string, signedHeaders map[string]string, data []byte) error {
	req, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("api: build PUT request: %w", err)
	}
	for k, v := range signedHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("api: PUT binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(body))
		return fmt.Errorf("api: PUT binary HTTP %d%s", resp.StatusCode,
			func() string {
				if msg != "" {
					return ": " + msg
				}
				return ""
			}())
	}
	return nil
}

// ---------------------------------------------------------------------------
// 内部实现
// ---------------------------------------------------------------------------

func (c *Client) doRequest(method, baseURL, apiPath, signPath string, body interface{}, dest interface{}) error {
	// 确定签名路径（不含 query string）
	effectiveSignPath := signPath
	if effectiveSignPath == "" {
		if idx := strings.Index(apiPath, "?"); idx >= 0 {
			effectiveSignPath = apiPath[:idx]
		} else {
			effectiveSignPath = apiPath
		}
	}

	// 时间戳
	timestampSec := strconv.FormatInt(time.Now().Unix(), 10)

	// 签名
	signature := ComputeHTTPSignature(c.creds.AgentKey, method, effectiveSignPath, c.creds.AgentID, timestampSec)

	// 构建请求体
	var bodyReader io.Reader
	if method != "GET" && method != "HEAD" {
		payload := body
		if payload == nil {
			payload = struct{}{}
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("api: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	url := baseURL + apiPath
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("api: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Id", c.creds.AgentID)
	req.Header.Set("X-Timestamp", timestampSec)
	req.Header.Set("X-Agent-Signature", signature)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("api: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("api: read response body: %w", err)
	}

	// HTTP 层错误
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var pr platformResponse
		_ = json.Unmarshal(rawBody, &pr)
		msg := pr.Message
		if msg == "" {
			msg = string(rawBody)
		}
		code := pr.Code
		if code == "" {
			code = strconv.Itoa(resp.StatusCode)
		}
		return newPlatformError(code, msg, resp.StatusCode)
	}

	// 解析平台统一响应
	var wrapper struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &wrapper); err != nil {
		// 响应不是平台标准格式，尝试直接反序列化到 dest
		if dest != nil {
			if err2 := json.Unmarshal(rawBody, dest); err2 != nil {
				return fmt.Errorf("api: parse response: %w", err)
			}
		}
		return nil
	}

	// 平台业务错误（code 非 "200"）
	if wrapper.Code != "" && wrapper.Code != "200" {
		return newPlatformError(wrapper.Code, wrapper.Message, 0)
	}

	// 反序列化 data 字段到 dest
	if dest != nil && len(wrapper.Data) > 0 && string(wrapper.Data) != "null" {
		if err := json.Unmarshal(wrapper.Data, dest); err != nil {
			return fmt.Errorf("api: unmarshal data field: %w", err)
		}
	}

	return nil
}
