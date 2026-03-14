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

// CheckAuthResponse 服务器返回通用格式：
//
//	{"code":"200","message":"OK"}          — pending（等待用户授权）
//	{"code":"200","message":"OK","data":{}} — authorized（已授权，data 含凭证）
//	{"code":"4xx","message":"..."}          — 错误（code 过期/不存在等）
type CheckAuthResponse struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Data    *Credentials `json:"data,omitempty"`
}

// IsSuccess 服务器是否返回 200
func (r *CheckAuthResponse) IsSuccess() bool {
	return r.Code == "200"
}

// IsAuthorized 用户已完成授权（data 字段有凭证）
func (r *CheckAuthResponse) IsAuthorized() bool {
	return r.IsSuccess() && r.Data != nil && r.Data.AgentID != ""
}

// Credentials 凭证信息（鉴权模块使用）
type Credentials struct {
	AgentID  string `json:"agent_id"`
	AgentKey string `json:"agent_key"`
	APIURL   string `json:"api_url"`
	MQTTURL  string `json:"mqtt_url"`
	ExpireAt string `json:"expire_at"`
}
