package main

// inbox_history.go — inbox history subcommand: 通过服务端 API 查询两个 agent 的消息记录。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/urfave/cli/v2"
)

const historyAPIPath = "/agent-message/api/v1/agents/sessions/messages"

// historyItem mirrors the server response for a single message.
type historyItem struct {
	MessageID  string `json:"messageId"`
	SenderID   string `json:"senderId"`
	ReceiverID string `json:"receiverId"`
	Content    string `json:"content"`
	TraceID    string `json:"traceId"`
	Timestamp  string `json:"timestamp"`
}

// historyData is the data block returned by the server.
// The API returns { success, data: historyData }, and api.Client.GetJSON
// extracts the "data" field into dest, so we bind directly to this struct.
type historyData struct {
	Me      map[string]interface{} `json:"me"`
	Partner map[string]interface{} `json:"partner"`
	Items   []historyItem          `json:"items"`
	Page    int                    `json:"page"`
	Limit   int                    `json:"limit"`
	Total   int                    `json:"total"`
}

func inboxHistoryCommand() *cli.Command {
	return &cli.Command{
		Name:   "history",
		Usage:  "Fetch message history with a peer via server-side API",
		Action: inboxHistoryCmd,
		Flags: []cli.Flag{
			configDirFlag(),
			&cli.StringFlag{Name: "peer-id", Usage: "peer agent ID", Required: true},
			&cli.IntFlag{Name: "page", Value: 1, Usage: "page number"},
			&cli.IntFlag{Name: "limit", Value: 20, Usage: "messages per page (max 100)"},
			&cli.BoolFlag{Name: "raw-content", Usage: "include raw A2A envelope in content field"},
		},
	}
}

func inboxHistoryCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	peerID := strings.TrimSpace(c.String("peer-id"))
	page := c.Int("page")
	limit := c.Int("limit")
	rawContent := c.Bool("raw-content")

	if peerID == "" {
		return outputError("inbox.history", fmt.Errorf("--peer-id is required"))
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	creds, err := loadCreds(configDir)
	if err != nil {
		return outputError("inbox.history", err)
	}

	// sessionId: 两个 agentId 按字母序拼接
	ids := []string{creds.AgentID, peerID}
	sort.Strings(ids)
	sessionID := strings.Join(ids, "_")

	apiPath := fmt.Sprintf("%s?sessionId=%s&page=%d&limit=%d", historyAPIPath, sessionID, page, limit)

	client := buildAPIClient(creds)

	// api.Client.GetJSON 解析 {code,data} 或 {success,data} 格式时，
	// 均会把 "data" 字段内容反序列化到 dest。
	var data historyData
	if err := client.GetJSON(apiPath, historyAPIPath, &data); err != nil {
		return outputError("inbox.history", err)
	}

	// 格式化 items 输出
	items := make([]map[string]interface{}, 0, len(data.Items))
	for _, m := range data.Items {
		direction := "recv"
		if m.SenderID == creds.AgentID {
			direction = "sent"
		}

		item := map[string]interface{}{
			"message_id": m.MessageID,
			"sender_id":  m.SenderID,
			"direction":  direction,
			"timestamp":  m.Timestamp,
			"text":       historyExtractText(m.Content),
		}
		if m.TraceID != "" {
			item["trace_id"] = m.TraceID
		}
		if rawContent {
			item["content"] = m.Content
		}
		items = append(items, item)
	}

	return outputOK("inbox.history", map[string]interface{}{
		"session_id": sessionID,
		"me":         data.Me,
		"partner":    data.Partner,
		"page":       data.Page,
		"limit":      data.Limit,
		"total":      data.Total,
		"items":      items,
		"count":      len(items),
	})
}

// historyExtractText extracts human-readable text from an A2A envelope JSON string.
func historyExtractText(content string) string {
	if content == "" {
		return ""
	}
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		return content
	}
	payload, _ := env["payload"].(map[string]interface{})
	if payload == nil {
		return ""
	}

	var parts []string
	if text, _ := payload["text"].(string); text != "" {
		parts = append(parts, strings.TrimSpace(text))
	}
	if qr, _ := payload["payment_qr"].(string); qr != "" {
		parts = append(parts, "[收款二维码]: "+qr)
	}
	if att, ok := payload["attachment"].(map[string]interface{}); ok {
		name, _ := att["name"].(string)
		if name == "" {
			name = "文件"
		}
		url, _ := att["url"].(string)
		parts = append(parts, fmt.Sprintf("[附件: %s] %s", name, url))
	}
	return strings.Join(parts, "\n")
}
