package config

import (
	"encoding/json"
	"os"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
)

// ListenerConfig holds optional listener daemon configuration.
type ListenerConfig struct {
	UpdateCheckInterval string `json:"update_check_interval,omitempty"` // Go duration, default "12h"
	FlushInterval       string `json:"flush_interval,omitempty"`        // Go duration, default "5s"
}

// ParseUpdateCheckInterval returns the update check interval as time.Duration.
func (c *ListenerConfig) ParseUpdateCheckInterval() time.Duration {
	if c == nil || c.UpdateCheckInterval == "" {
		return 12 * time.Hour
	}
	d, err := time.ParseDuration(c.UpdateCheckInterval)
	if err != nil || d < 1*time.Minute {
		return 12 * time.Hour
	}
	return d
}

// ParseFlushInterval returns the flush interval as time.Duration.
func (c *ListenerConfig) ParseFlushInterval() time.Duration {
	if c == nil || c.FlushInterval == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(c.FlushInterval)
	if err != nil || d < 1*time.Second {
		return 5 * time.Second
	}
	return d
}

// Credentials 凭证信息
type Credentials struct {
	AgentID     string    `json:"agent_id"`
	AgentKey    string    `json:"agent_key"`
	APIURL      string    `json:"api_url"`
	MQTTURL     string    `json:"mqtt_url"`
	ExpireAt    time.Time `json:"expire_at"`
	CreatedAt   time.Time `json:"created_at"`
	// PushEnabled 控制 listener 是否在收到消息时主动推送到 OpenClaw。
	PushEnabled bool            `json:"push_enabled"`
	Listener    *ListenerConfig `json:"listener,omitempty"`
}

// CredentialsConfig 凭证配置（用于JSON持久化）
// PushEnabled 使用指针类型以区分"未设置"（默认 true）和"显式 false"。
type CredentialsConfig struct {
	AgentID     string  `json:"agent_id"`
	AgentKey    string  `json:"agent_key"`
	APIURL      string  `json:"api_url"`
	MQTTURL     string  `json:"mqtt_url"`
	ExpiresAt   string  `json:"expires_at"`
	// PushEnabled 控制消息推送模式。nil 或缺失时默认 true（即时推送）。
	PushEnabled *bool           `json:"push_enabled,omitempty"`
	Listener    *ListenerConfig `json:"listener,omitempty"`
}

// IsExpired 检查凭证是否过期
func (c *Credentials) IsExpired() bool {
	return time.Now().After(c.ExpireAt)
}

// Save 保存凭证到文件
func (c *Credentials) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return common.CredentialError("序列化凭证失败", err)
	}

	err = os.WriteFile(path, data, 0600)
	if err != nil {
		return common.CredentialError("保存凭证失败", err)
	}

	common.Infof("凭证已保存到: %s", path)
	return nil
}

// LoadCredentials 从文件加载凭证
func LoadCredentials(path string) (*Credentials, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, common.CredentialError("凭证文件不存在", nil)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, common.CredentialError("读取凭证文件失败", err)
	}

	var credsConfig CredentialsConfig
	err = json.Unmarshal(data, &credsConfig)
	if err != nil {
		return nil, common.CredentialError("解析凭证文件失败", err)
	}

	// 转换：PushEnabled 未设置时默认 true（即时推送模式）
	pushEnabled := true
	if credsConfig.PushEnabled != nil {
		pushEnabled = *credsConfig.PushEnabled
	}
	creds := &Credentials{
		AgentID:     credsConfig.AgentID,
		AgentKey:    credsConfig.AgentKey,
		APIURL:      credsConfig.APIURL,
		MQTTURL:     credsConfig.MQTTURL,
		PushEnabled: pushEnabled,
		Listener:    credsConfig.Listener,
	}

	if credsConfig.ExpiresAt != "" {
		// 尝试解析时间
		parsed, err := time.Parse(time.RFC3339, credsConfig.ExpiresAt)
		if err == nil {
			creds.ExpireAt = parsed
		}
	}

	return creds, nil
}

// Delete 删除凭证文件
func DeleteCredentials(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	err := os.Remove(path)
	if err != nil {
		return common.CredentialError("删除凭证文件失败", err)
	}

	common.Infof("凭证已删除: %s", path)
	return nil
}
