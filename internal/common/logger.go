package common

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
)

var logger zerolog.Logger

func init() {
	logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
}

// NewLogger 创建新的日志实例
func NewLogger() zerolog.Logger {
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}

// SetLogLevel 设置日志级别
func SetLogLevel(level string) {
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

// GetLogger 获取日志实例
func GetLogger() *zerolog.Logger {
	return &logger
}

// Debug 调试日志
func Debug(args ...interface{}) {
	logger.Debug().Msg(formatArgs(args...))
}

// Info 信息日志
func Info(args ...interface{}) {
	logger.Info().Msg(formatArgs(args...))
}

// Warn 警告日志
func Warn(args ...interface{}) {
	logger.Warn().Msg(formatArgs(args...))
}

// Error 错误日志
func Error(args ...interface{}) {
	logger.Error().Msg(formatArgs(args...))
}

// Fatal 致命错误日志
func Fatal(args ...interface{}) {
	logger.Fatal().Msg(formatArgs(args...))
}

// Debugf 格式化调试日志
func Debugf(format string, args ...interface{}) {
	logger.Debug().Msgf(format, args...)
}

// Infof 格式化信息日志
func Infof(format string, args ...interface{}) {
	logger.Info().Msgf(format, args...)
}

// Warnf 格式化警告日志
func Warnf(format string, args ...interface{}) {
	logger.Warn().Msgf(format, args...)
}

// Errorf 格式化错误日志
func Errorf(format string, args ...interface{}) {
	logger.Error().Msgf(format, args...)
}

// Fatalf 格式化致命错误日志
func Fatalf(format string, args ...interface{}) {
	logger.Fatal().Msgf(format, args...)
}

// formatArgs 格式化日志参数
func formatArgs(args ...interface{}) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		result += fmt.Sprintf("%v", arg)
	}
	return result
}
