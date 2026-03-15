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

// GetMostRecentSessionID runs `openclaw sessions --json` and returns the sessionId of
// the first session (sessions are ordered by updatedAt desc — most recent first).
// Returns an empty string if no sessions are found or the command fails.
func GetMostRecentSessionID() (string, error) {
	out, err := exec.Command("openclaw", "sessions", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("openclaw sessions: %w", err)
	}

	var result sessionsOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("openclaw sessions: parse JSON: %w", err)
	}
	if len(result.Sessions) == 0 {
		return "", fmt.Errorf("openclaw sessions: no sessions found")
	}

	id := strings.TrimSpace(result.Sessions[0].SessionID)
	if id == "" {
		return "", fmt.Errorf("openclaw sessions: first session has empty sessionId")
	}
	return id, nil
}

// SendToSession runs `openclaw agent --session-id <id> --message <msg>`.
// It returns an error if the command exits non-zero.
func SendToSession(sessionID, message string) error {
	cmd := exec.Command("openclaw", "agent",
		"--session-id", sessionID,
		"--message", message,
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
