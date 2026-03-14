package auth

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/config"
)

// Auth 鉴权服务
type Auth struct {
	client *Client
	config *config.Config
}

// New 创建鉴权服务
func New(cfg *config.Config) *Auth {
	return &Auth{
		client: NewClient(cfg),
		config: cfg,
	}
}

// GetMACAddress 获取MAC地址
func GetMACAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", common.AuthFailedError("获取网络接口失败", err)
	}

	// 查找第一个非空MAC地址
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		mac := iface.HardwareAddr.String()
		if mac != "" {
			common.Debugf("获取到MAC地址: %s (interface: %s)", mac, iface.Name)
			return mac, nil
		}
	}

	return "", common.AuthFailedError("无法获取MAC地址", nil)
}

// GenAuthCode 生成鉴权码
func (a *Auth) GenAuthCode(feishuUserID string) (*InitLoginResponse, error) {
	// 获取MAC地址
	mac, err := GetMACAddress()
	if err != nil {
		return nil, err
	}

	// 获取时间戳
	timestamp := time.Now().Unix()

	// 构建请求
	req := &InitLoginRequest{
		Timestamp:    timestamp,
		MAC:          mac,
		FeishuUserID: feishuUserID,
	}

	common.Infof("正在生成鉴权码...")
	common.Infof("MAC: %s", mac)
	common.Infof("Timestamp: %d", timestamp)
	common.Infof("FeishuUserID: %s", feishuUserID)

	// 发送请求
	resp, err := a.client.InitLogin(req)
	if err != nil {
		return nil, err
	}

	common.Infof("鉴权码生成成功!")
	common.Infof("Code: %s", resp.Code)
	common.Infof("URL: %s", resp.URL)

	return resp, nil
}

// GetAuth 获取授权凭证
func (a *Auth) GetAuth(code string, poll bool, interval int) (*config.Credentials, error) {
	if interval <= 0 {
		interval = 2 // 默认2秒轮询一次
	}

	for {
		common.Infof("正在检查鉴权状态...")

		resp, err := a.client.CheckAuth(code)
		if err != nil {
			return nil, err
		}

		if !resp.IsSuccess() {
			return nil, common.AuthFailedError(fmt.Sprintf("服务器错误 (code=%s): %s", resp.Code, resp.Message), nil)
		}

		if resp.IsAuthorized() {
			d := resp.Data
			expireAt, err := time.Parse(time.RFC3339, d.ExpireAt)
			if err != nil {
				common.Warnf("无法解析过期时间，使用默认: %v", err)
				expireAt = time.Now().AddDate(1, 0, 0)
			}
			apiURL := d.APIURL
			if apiURL == "" {
				apiURL = "https://api.a2hmarket.ai"
			}
			mqttURL := d.MQTTURL
			if mqttURL == "" {
				mqttURL = "mqtts://post-cn-e4k4o78q702.mqtt.aliyuncs.com:8883"
			}

			creds := &config.Credentials{
				AgentID:   d.AgentID,
				AgentKey:  d.AgentKey,
				APIURL:    apiURL,
				MQTTURL:   mqttURL,
				ExpireAt:  expireAt,
				CreatedAt: time.Now(),
			}

			credPath := a.config.GetCredentialsPath()
			if err = creds.Save(credPath); err != nil {
				return nil, err
			}

			common.Infof("授权成功!")
			common.Infof("AgentID: %s", creds.AgentID)
			common.Infof("APIURL: %s", creds.APIURL)
			common.Infof("MQTTURL: %s", creds.MQTTURL)
			common.Infof("ExpireAt: %s", creds.ExpireAt.Format(time.RFC3339))

			return creds, nil
		}

		// code==200 且 data 为空 → 等待用户授权
		common.Infof("等待授权中 (pending)...")
		if !poll {
			return nil, common.AuthFailedError("等待授权中，请使用 --poll 参数轮询", nil)
		}
		common.Infof("%d秒后重试...", interval)
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

// GetSavedCredentials 获取已保存的凭证
func (a *Auth) GetSavedCredentials() (*config.Credentials, error) {
	credPath := a.config.GetCredentialsPath()
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return nil, err
	}

	if creds.IsExpired() {
		common.Warnf("凭证已过期")
		return nil, common.CredentialError("凭证已过期", nil)
	}

	return creds, nil
}

// ClearCredentials 清除保存的凭证
func (a *Auth) ClearCredentials() error {
	credPath := a.config.GetCredentialsPath()
	return config.DeleteCredentials(credPath)
}

// ParseURLs 解析API和MQTT URL
func ParseURLs(apiURL string) (apiBase string, mqttURL string) {
	// 去除协议前缀
	apiBase = strings.TrimPrefix(apiURL, "https://")
	apiBase = strings.TrimPrefix(apiBase, "http://")

	// 构建MQTT URL
	mqttURL = strings.Replace(apiBase, "api.", "mqtt.", 1)
	mqttURL = "mqtts://" + mqttURL

	if !strings.HasSuffix(mqttURL, ":8883") {
		mqttURL = mqttURL + ":8883"
	}

	return apiBase, mqttURL
}
