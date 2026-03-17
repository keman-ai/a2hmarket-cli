// 集成测试：通过 session 层获取最近 session、解析 channel、给 session 发消息、给 channel 发消息。
// 若本地未运行 OpenClaw gateway、认证失败或无 session，测试会跳过。
// 参考 internal/dispatcher/push_outbox.go 中 GetMostRecentSession、ParseSessionKey、SendToSession、SendMediaToChannel 的用法。
package test

import (
	"strings"
	"testing"

	"github.com/keman-ai/a2hmarket-cli/internal/openclaw"
)

// skipReasonForSessionErr 根据错误内容返回更易读的跳过原因（含 gateway 认证失败）
func skipReasonForSessionErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(s, "token mismatch") || strings.Contains(s, "unauthorized") {
		return "gateway 已连接但认证失败（请检查 ~/.openclaw 下 token 与 gateway 配置一致），跳过: " + s
	}
	return "无法获取最近 session，跳过: " + s
}

// TestGetMostRecentSessionAndSendToSession 测试：获取最近 session + 给 session 发送消息
func TestGetMostRecentSessionAndSendToSession(t *testing.T) {
	sess, err := openclaw.GetMostRecentSession()
	if err != nil {
		t.Skip(skipReasonForSessionErr(err))
	}
	if sess == nil || sess.Key == "" {
		t.Skip("最近 session 为空或 key 为空")
	}

	// 给该 session 发送消息（会走 gateway 的 chat.send 或 CLI fallback）
	err = openclaw.SendToSession(sess.Key, "[test] session send message")
	if err != nil {
		t.Fatalf("SendToSession: %v", err)
	}
}

// TestGetMostRecentSessionAndSendToChannel 测试：获取最近 session、解析 channel/target + 给 channel 发送消息
func TestGetMostRecentSessionAndSendToChannel(t *testing.T) {
	sess, err := openclaw.GetMostRecentSession()
	if err != nil {
		t.Skip(skipReasonForSessionErr(err))
	}
	if sess == nil || sess.Key == "" {
		t.Skip("最近 session 为空或 key 为空")
	}

	channel, target := openclaw.ParseSessionKey(sess.Key)
	if channel == "" || target == "" {
		t.Skipf("无法从 session key 解析 channel/target (key=%q)，跳过 channel 发送", sess.Key)
	}

	// 给 channel 发送纯文本（无附件）
	err = openclaw.SendMediaToChannel(channel, target, "[test] channel send message", "")
	if err != nil {
		t.Fatalf("SendMediaToChannel: %v", err)
	}
}
