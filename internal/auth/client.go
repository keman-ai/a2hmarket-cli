package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/config"
)

// Client HTTP客户端
type Client struct {
	baseURL  string
	client   *http.Client
	timeout  time.Duration
}

// NewClient 创建HTTP客户端
func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		client: &http.Client{
			Timeout: time.Duration(cfg.AuthTimeout) * time.Second,
		},
		timeout: time.Duration(cfg.AuthTimeout) * time.Second,
	}
}

// InitLogin 初始化登录
func (c *Client) InitLogin(req *InitLoginRequest) (*InitLoginResponse, error) {
	reqURL := fmt.Sprintf("%s/v1/auth/init-login", c.baseURL)

	// 构建表单数据
	values := url.Values{}
	values.Set("timestamp", fmt.Sprintf("%d", req.Timestamp))
	values.Set("mac", req.MAC)
	values.Set("feishu_user_id", req.FeishuUserID)

	common.Debugf("发送初始化登录请求: %s", reqURL)
	common.Debugf("请求参数: timestamp=%d, mac=%s, feishu_user_id=%s", req.Timestamp, req.MAC, req.FeishuUserID)

	// 使用Form URL encoded方式提交
	resp, err := c.client.PostForm(reqURL, values)
	if err != nil {
		return nil, common.NetworkError("初始化登录请求失败", err)
	}
	defer resp.Body.Close()

	var result InitLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, common.NetworkError("解析响应失败", err)
	}

	if result.Error != "" {
		return nil, common.AuthFailedError(result.Error, nil)
	}

	return &result, nil
}

// CheckAuth 检查鉴权状态
func (c *Client) CheckAuth(code string) (*CheckAuthResponse, error) {
	reqURL := fmt.Sprintf("%s/v1/auth/check?code=%s", c.baseURL, code)

	common.Debugf("发送检查鉴权请求: %s", reqURL)

	resp, err := c.client.Get(reqURL)
	if err != nil {
		return nil, common.NetworkError("检查鉴权请求失败", err)
	}
	defer resp.Body.Close()

	var result CheckAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, common.NetworkError("解析响应失败", err)
	}

	return &result, nil
}
