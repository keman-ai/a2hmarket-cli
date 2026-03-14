package config

import (
	"encoding/json"
	"os"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
)

// Credentials 凭证信息
type Credentials struct {
	AgentID    string    `json:"agent_id"`
	AgentKey   string    `json:"agent_key"`
	APIURL     string    `json:"api_url"`
	MQTTURL    string    `json:"mqtt_url"`
	ExpireAt   time.Time `json:"expire_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// CredentialsConfig 凭证配置（用于JSON持久化）
type CredentialsConfig struct {
	AgentID   string `json:"agent_id"`
	AgentKey  string `json:"agent_key"`
	APIURL    string `json:"api_url"`
	MQTTURL   string `json:"mqtt_url"`
	ExpiresAt string `json:"expires_at"`
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

	// 转换
	creds := &Credentials{
		AgentID:  credsConfig.AgentID,
		AgentKey: credsConfig.AgentKey,
		APIURL:   credsConfig.APIURL,
		MQTTURL:  credsConfig.MQTTURL,
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
