package auth

// InitLoginRequest 初始化登录请求
type InitLoginRequest struct {
	Timestamp    int64  `json:"timestamp"`
	MAC          string `json:"mac"`
	FeishuUserID string `json:"feishu_user_id"`
}

// InitLoginResponse 初始化登录响应
type InitLoginResponse struct {
	Code string `json:"code"`
	URL  string `json:"url"`
	Error string `json:"error,omitempty"`
}

// CheckAuthRequest 检查鉴权请求
type CheckAuthRequest struct {
	Code string `json:"code"`
}

// CheckAuthResponse 检查鉴权响应（支持嵌套credentials结构）
type CheckAuthResponse struct {
	Status      string       `json:"status"`
	Credentials *Credentials `json:"credentials,omitempty"`
	AgentID     string       `json:"agent_id,omitempty"`
	AgentKey    string       `json:"agent_key,omitempty"`
	APIURL      string       `json:"api_url,omitempty"`
	MQTTURL     string       `json:"mqtt_url,omitempty"`
	ExpireAt    string       `json:"expire_at,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// GetCredentials 获取凭证（支持两种响应格式）
func (r *CheckAuthResponse) GetCredentials() *Credentials {
	if r.Credentials != nil {
		return r.Credentials
	}
	// 兼容扁平结构
	return &Credentials{
		AgentID:  r.AgentID,
		AgentKey: r.AgentKey,
		APIURL:   r.APIURL,
		MQTTURL:  r.MQTTURL,
		ExpireAt: r.ExpireAt,
	}
}

// AuthStatus 鉴权状态
type AuthStatus string

const (
	AuthStatusPending     AuthStatus = "pending"
	AuthStatusAuthorized AuthStatus = "authorized"
	AuthStatusExpired    AuthStatus = "expired"
	AuthStatusUsed       AuthStatus = "used"
	AuthStatusNotFound   AuthStatus = "not_found"
)

// Credentials 凭证信息（鉴权模块使用）
type Credentials struct {
	AgentID  string `json:"agent_id"`
	AgentKey string `json:"agent_key"`
	APIURL   string `json:"api_url"`
	MQTTURL  string `json:"mqtt_url"`
	ExpireAt string `json:"expire_at"`
}
