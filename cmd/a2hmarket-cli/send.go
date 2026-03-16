package main

// send.go — send command (direct MQTT publish with _pub_ clientId).

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	mqttpkg "github.com/keman-ai/a2hmarket-cli/internal/mqtt"
	"github.com/keman-ai/a2hmarket-cli/internal/oss"
	"github.com/keman-ai/a2hmarket-cli/internal/protocol"
	"github.com/urfave/cli/v2"
)

func sendCommand() *cli.Command {
	return &cli.Command{
		Name:   "send",
		Usage:  "Send A2A message to target agent via MQTT",
		Action: sendMessageCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "target-agent-id", Usage: "Target agent ID", Required: true},
			&cli.StringFlag{Name: "text", Usage: "Message text (sets payload.text)"},
			&cli.StringFlag{Name: "payload-json", Usage: "Full payload as JSON object"},
			&cli.StringFlag{Name: "message-type", Value: "chat.request", Usage: "Message type"},
			&cli.StringFlag{Name: "payment-qr", Usage: "Payment QR code URL (sets payload.payment_qr)"},
			&cli.StringFlag{Name: "attachment", Aliases: []string{"a"}, Usage: "Local file path to upload as OSS attachment"},
			&cli.StringFlag{Name: "url", Aliases: []string{"u"}, Usage: "External URL as attachment link"},
			&cli.StringFlag{Name: "url-name", Usage: "Filename hint for --url"},
			&cli.StringFlag{Name: "url-mime", Usage: "MIME type hint for --url"},
			&cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"},
		},
	}
}

func sendMessageCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))

	creds, err := loadCreds(configDir)
	if err != nil {
		return outputError("send", err)
	}

	targetAgentID := c.String("target-agent-id")
	messageType := c.String("message-type")
	text := c.String("text")
	payloadJSON := c.String("payload-json")

	payload := make(map[string]interface{})
	if payloadJSON != "" {
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return outputError("send", fmt.Errorf("invalid --payload-json: %w", err))
		}
	}
	if text != "" {
		payload["text"] = text
	}

	if _, hasImage := payload["image"]; hasImage {
		return outputError("send", fmt.Errorf("payload.image 字段已废弃，请改用 --payment-qr 或 --attachment / --url"))
	}

	if qr := c.String("payment-qr"); qr != "" {
		if !strings.HasPrefix(qr, "http://") && !strings.HasPrefix(qr, "https://") {
			return outputError("send", fmt.Errorf("--payment-qr 必须以 http:// 或 https:// 开头"))
		}
		payload["payment_qr"] = qr
	}

	attachPath := c.String("attachment")
	externalURL := c.String("url")
	if attachPath != "" && externalURL != "" {
		return outputError("send", fmt.Errorf("--attachment 和 --url 不能同时使用"))
	}

	if attachPath != "" {
		fileInfo, err := oss.Upload(creds, attachPath, "chatfile", nil)
		if err != nil {
			return outputError("send", fmt.Errorf("attachment upload: %w", err))
		}
		payload["attachment"] = map[string]interface{}{
			"url":        fileInfo.URL,
			"name":       fileInfo.FileName,
			"size":       fileInfo.FileSize,
			"mime_type":  fileInfo.MIMEType,
			"expires_at": fileInfo.ExpiresAt,
			"source":     "oss",
		}
	} else if externalURL != "" {
		if !strings.HasPrefix(externalURL, "http://") && !strings.HasPrefix(externalURL, "https://") {
			return outputError("send", fmt.Errorf("--url 必须以 http:// 或 https:// 开头"))
		}
		att := map[string]interface{}{
			"url":    externalURL,
			"source": "external",
		}
		if name := c.String("url-name"); name != "" {
			att["name"] = name
		} else if derived := oss.MIMEFromPath(externalURL); derived != "" {
			if idx := strings.LastIndex(externalURL, "/"); idx >= 0 {
				if base := externalURL[idx+1:]; base != "" {
					att["name"] = strings.SplitN(base, "?", 2)[0]
				}
			}
		}
		if mime := c.String("url-mime"); mime != "" {
			att["mime_type"] = mime
		} else if name, ok := att["name"].(string); ok && name != "" {
			if m := oss.MIMEFromPath(name); m != "" {
				att["mime_type"] = m
			}
		}
		payload["attachment"] = att
	}

	env, err := protocol.BuildEnvelope(creds.AgentID, targetAgentID, messageType, payload)
	if err != nil {
		return outputError("send", fmt.Errorf("build envelope: %w", err))
	}
	signed := protocol.Sign(creds.AgentKey, env)

	common.Infof("send to=%s type=%s msg_id=%s", targetAgentID, messageType, signed.MessageID)

	// Use a short-lived _pub_ clientId so we never kick a running listener.
	tc := mqttpkg.NewTokenClient(creds.APIURL, creds.AgentID, creds.AgentKey)
	sendClientID := mqttpkg.BuildSendClientID(creds.AgentID)
	transport := mqttpkg.NewTransportWithClientID(creds.MQTTURL, tc, creds.AgentID, sendClientID)

	if err := transport.Connect(); err != nil {
		return outputError("send", fmt.Errorf("mqtt connect: %w", err))
	}
	defer transport.Close()

	// Retry publish up to 3 times with exponential backoff (1s, 2s, 4s).
	const maxRetries = 3
	var publishErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		publishErr = transport.Publish(targetAgentID, signed)
		if publishErr == nil {
			break
		}
		common.Warnf("send: publish attempt %d/%d failed: %v", attempt+1, maxRetries, publishErr)
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
	}
	if publishErr != nil {
		return outputError("send", fmt.Errorf("mqtt publish (after %d attempts): %w", maxRetries, publishErr))
	}

	// Brief pause so paho flushes the packet before Disconnect.
	time.Sleep(300 * time.Millisecond)

	return outputOK("send", map[string]interface{}{
		"message_id": signed.MessageID,
		"target_id":  targetAgentID,
		"type":       messageType,
	})
}
