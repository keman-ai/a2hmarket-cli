package common

import "fmt"

// ErrorCode 错误码
type ErrorCode string

const (
	ErrCodeInvalidParams   ErrorCode = "INVALID_PARAMS"
	ErrCodeAuthFailed      ErrorCode = "AUTH_FAILED"
	ErrCodeNetworkError    ErrorCode = "NETWORK_ERROR"
	ErrCodeTimeout         ErrorCode = "TIMEOUT"
	ErrCodeMQTTError       ErrorCode = "MQTT_ERROR"
	ErrCodeConfigError     ErrorCode = "CONFIG_ERROR"
	ErrCodeCredentialError ErrorCode = "CREDENTIAL_ERROR"
	ErrCodeUnknown         ErrorCode = "UNKNOWN"
)

// AppError 应用错误
type AppError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Err     error     `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// NewError 创建新错误
func NewError(code ErrorCode, message string, err error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// InvalidParamsError 参数错误
func InvalidParamsError(message string) *AppError {
	return NewError(ErrCodeInvalidParams, message, nil)
}

// AuthFailedError 鉴权失败错误
func AuthFailedError(message string, err error) *AppError {
	return NewError(ErrCodeAuthFailed, message, err)
}

// NetworkError 网络错误
func NetworkError(message string, err error) *AppError {
	return NewError(ErrCodeNetworkError, message, err)
}

// TimeoutError 超时错误
func TimeoutError(message string, err error) *AppError {
	return NewError(ErrCodeTimeout, message, err)
}

// MQTTError MQTT错误
func MQTTError(message string, err error) *AppError {
	return NewError(ErrCodeMQTTError, message, err)
}

// ConfigError 配置错误
func ConfigError(message string, err error) *AppError {
	return NewError(ErrCodeConfigError, message, err)
}

// CredentialError 凭证错误
func CredentialError(message string, err error) *AppError {
	return NewError(ErrCodeCredentialError, message, err)
}
