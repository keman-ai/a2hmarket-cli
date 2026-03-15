// Package openclaw provides helpers for interacting with the OpenClaw gateway CLI.
package openclaw

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Session holds the key fields of an OpenClaw session.
type Session struct {
	Key       string `json:"key"`
	UpdatedAt int64  `json:"updatedAt"`
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	AgentID   string `json:"agentId"`
}

// sessionsOutput is the top-level JSON returned by `openclaw sessions --json`.
type sessionsOutput struct {
	Sessions []Session `json:"sessions"`
}

// GetMostRecentSession runs `openclaw sessions --json` and returns the most
// recently updated session (first in the list, ordered by updatedAt desc).
func GetMostRecentSession() (*Session, error) {
	out, err := exec.Command("openclaw", "sessions", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("openclaw sessions: %w", err)
	}

	var result sessionsOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("openclaw sessions: parse JSON: %w", err)
	}
	if len(result.Sessions) == 0 {
		return nil, fmt.Errorf("openclaw sessions: no sessions found")
	}

	s := &result.Sessions[0]
	if strings.TrimSpace(s.SessionID) == "" {
		return nil, fmt.Errorf("openclaw sessions: first session has empty sessionId")
	}
	s.SessionID = strings.TrimSpace(s.SessionID)
	return s, nil
}

// GetMostRecentSessionID is a convenience wrapper returning just the session ID.
func GetMostRecentSessionID() (string, error) {
	s, err := GetMostRecentSession()
	if err != nil {
		return "", err
	}
	return s.SessionID, nil
}

// ParseSessionKey extracts channel and target from a session key.
// Format: "agent:<agentId>:<channel>:<kind>:<target>"
// Example: "agent:main:feishu:direct:ou_9adf14b0..."
// Returns channel="" if the key doesn't match the expected format.
func ParseSessionKey(key string) (channel, target string) {
	parts := strings.Split(key, ":")
	if len(parts) < 5 {
		return "", ""
	}
	// parts[0]="agent", parts[1]=agentId, parts[2]=channel, parts[3]=kind, parts[4:]=target
	channel = parts[2]
	target = strings.Join(parts[4:], ":")
	return channel, target
}

// SendToSession runs `openclaw agent --session-id <id> --message <msg> --deliver`.
// The --deliver flag ensures the agent's reply is forwarded to the session's
// bound channel (e.g. Feishu, Telegram).
func SendToSession(sessionID, message string) error {
	cmd := exec.Command("openclaw", "agent",
		"--session-id", sessionID,
		"--message", message,
		"--deliver",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return fmt.Errorf("openclaw agent: %w", err)
		}
		return fmt.Errorf("openclaw agent: %w — %s", err, detail)
	}
	return nil
}

// SendMediaToChannel sends a message with a media attachment directly via
// `openclaw message send --channel <ch> --target <tgt> --media <path> --message <text>`.
// This bypasses the agent and sends directly through the channel,
// allowing images/files to be displayed natively (e.g. in Feishu).
func SendMediaToChannel(channel, target, message, mediaPath string) error {
	args := []string{"message", "send",
		"--channel", channel,
		"--target", target,
	}
	if message != "" {
		args = append(args, "--message", message)
	}
	if mediaPath != "" {
		args = append(args, "--media", mediaPath)
	}
	cmd := exec.Command("openclaw", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return fmt.Errorf("openclaw message send: %w", err)
		}
		return fmt.Errorf("openclaw message send: %w — %s", err, detail)
	}
	return nil
}
