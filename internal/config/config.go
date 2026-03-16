package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"github.com/keman-ai/a2hmarket-cli/internal/common"
)

const (
	// DefaultBaseURL 默认API地址
	DefaultBaseURL = "https://a2hmarket.ai"
	// DefaultAPIVersion 默认API版本
	DefaultAPIVersion = "v1"
	// DefaultConfigDir 默认配置目录
	DefaultConfigDir = ".a2hmarket"
	// DefaultCredentialsFile 默认凭证文件
	DefaultCredentialsFile = "credentials.json"
)

// Config CLI配置
type Config struct {
	BaseURL       string `mapstructure:"base_url"`
	APIVersion    string `mapstructure:"api_version"`
	ConfigDir     string `mapstructure:"config_dir"`
	Debug         bool   `mapstructure:"debug"`
	MQTTTimeout   int    `mapstructure:"mqtt_timeout"`
	AuthTimeout   int    `mapstructure:"auth_timeout"`
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	return &Config{
		BaseURL:       DefaultBaseURL,
		APIVersion:    DefaultAPIVersion,
		ConfigDir:     filepath.Join(homeDir, DefaultConfigDir),
		Debug:         false,
		MQTTTimeout:   30,
		AuthTimeout:   300,
	}
}

// Load 加载配置
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// 设置默认值
	viper.SetDefault("base_url", DefaultBaseURL)
	viper.SetDefault("api_version", DefaultAPIVersion)
	viper.SetDefault("mqtt_timeout", 30)
	viper.SetDefault("auth_timeout", 300)

	// 尝试从配置文件加载
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(cfg.ConfigDir)
	viper.AddConfigPath("/etc/a2hmarket/")

	// 找不到配置文件是正常情况，使用默认值即可
	_ = viper.ReadInConfig()

	// 从环境变量覆盖
	viper.BindEnv("base_url", "A2H_BASE_URL")
	viper.BindEnv("api_version", "A2H_API_VERSION")
	viper.BindEnv("debug", "A2H_DEBUG")
	viper.BindEnv("mqtt_timeout", "A2H_MQTT_TIMEOUT")
	viper.BindEnv("auth_timeout", "A2H_AUTH_TIMEOUT")

	// 解析配置
	err := viper.Unmarshal(cfg)
	if err != nil {
		return nil, common.ConfigError("无法解析配置", err)
	}

	// 确保配置目录存在
	if _, err := os.Stat(cfg.ConfigDir); os.IsNotExist(err) {
		err = os.MkdirAll(cfg.ConfigDir, 0755)
		if err != nil {
			return nil, common.ConfigError("无法创建配置目录", err)
		}
	}

	return cfg, nil
}

// GetAuthURL 获取鉴权URL
func (c *Config) GetAuthURL() string {
	return fmt.Sprintf("%s/%s/auth", c.BaseURL, c.APIVersion)
}

// GetInitLoginURL 获取初始化登录URL
func (c *Config) GetInitLoginURL() string {
	return fmt.Sprintf("%s/%s/auth/init-login", c.BaseURL, c.APIVersion)
}

// GetCheckAuthURL 获取检查鉴权URL
func (c *Config) GetCheckAuthURL() string {
	return fmt.Sprintf("%s/%s/auth/check", c.BaseURL, c.APIVersion)
}

// GetCredentialsPath 获取凭证文件路径
func (c *Config) GetCredentialsPath() string {
	return filepath.Join(c.ConfigDir, DefaultCredentialsFile)
}
