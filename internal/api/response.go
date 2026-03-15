package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// platformResponse 是平台统一响应包装格式
//
//	{ "code": "200", "message": "ok", "data": <any> }
//
// Code 使用 json.RawMessage 以兼容服务端返回整数 200 或字符串 "200" 两种格式。
type platformResponse struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Data    interface{}     `json:"data"`
}

// codeString 返回去除引号后的 code 字符串（兼容整数和字符串两种格式）。
func (p *platformResponse) codeString() string {
	return strings.Trim(string(p.Code), `"`)
}

// PlatformError 表示平台返回的业务错误（HTTP 状态正常但 code != "200"，或 HTTP 非 2xx）
type PlatformError struct {
	// PlatformCode 是平台返回的 code 字段（字符串形式）
	PlatformCode string
	// Message 是错误信息
	Message string
	// HTTPStatus 是 HTTP 状态码（HTTP 非 2xx 时有值）
	HTTPStatus int
}

func (e *PlatformError) Error() string {
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("platform error: HTTP %d, code=%s, message=%s",
			e.HTTPStatus, e.PlatformCode, e.Message)
	}
	return fmt.Sprintf("platform error: code=%s, message=%s", e.PlatformCode, e.Message)
}

func newPlatformError(code, message string, httpStatus int) *PlatformError {
	return &PlatformError{
		PlatformCode: code,
		Message:      message,
		HTTPStatus:   httpStatus,
	}
}
