// 集成测试：Gateway 获取最近 session 列表、给 session 发消息、给 channel 发消息。
// 若本地未运行 OpenClaw gateway 或认证失败，测试会跳过。
// 参考 internal/dispatcher/a2a_outbox.go / push_outbox.go 的调用方式。
package test

import (
	"strings"
	"testing"

	"github.com/keman-ai/a2hmarket-cli/internal/openclaw"
)

// skipReasonForGatewayErr 根据错误内容返回更易读的跳过原因
func skipReasonForGatewayErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(s, "token mismatch") || strings.Contains(s, "unauthorized") {
		return "gateway 已连接但认证失败（请检查 ~/.openclaw 下 openclaw.json / identity 的 token 与 gateway 配置一致），跳过: " + s
	}
	return "gateway 不可用，跳过: " + s
}

// TestGatewaySessionsListAndSendToSession 测试：获取最近 session 列表 + 给 session 发送消息
func TestGatewaySessionsListAndSendToSession(t *testing.T) {
	sessions, err := openclaw.GatewaySessionsList()
	if err != nil {
		t.Skip(skipReasonForGatewayErr(err))
	}
	if len(sessions) == 0 {
		t.Skip("gateway 返回 0 个 session，跳过发送测试")
	}

	// 取最近一个 session（GatewaySessionsList 返回顺序由服务端决定，通常已按更新时间）
	sess := &sessions[0]
	if sess.Key == "" {
		t.Fatal("最近 session 的 key 为空")
	}

	// 给该 session 发送一条测试消息
	err = openclaw.GatewayChatSend(sess.Key, "[test] gateway session message")
	if err != nil {
		t.Fatalf("GatewayChatSend: %v", err)
	}
}

// TestGatewaySessionsListAndSendToChannel 测试：获取最近 session（并解析 channel/target）+ 给 channel 发送消息
func TestGatewaySessionsListAndSendToChannel(t *testing.T) {
	sessions, err := openclaw.GatewaySessionsList()
	if err != nil {
		t.Skip(skipReasonForGatewayErr(err))
	}
	if len(sessions) == 0 {
		t.Skip("gateway 返回 0 个 session，无法解析 channel")
	}

	sess := &sessions[0]
	channel, target := openclaw.ParseSessionKey(sess.Key)
	if channel == "" || target == "" {
		t.Skipf("无法从 session key 解析 channel/target (key=%q)，跳过 channel 发送", sess.Key)
	}

	// 给 channel 发送一条文本消息（不经过 agent）
	err = openclaw.GatewaySend(channel, target, "[test1111111111] gateway channel message", "")
	if err != nil {
		t.Fatalf("GatewaySend: %v", err)
	}
}
