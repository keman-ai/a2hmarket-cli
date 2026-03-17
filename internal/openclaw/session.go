// Package openclaw provides helpers for interacting with the OpenClaw gateway.
//
// All public functions try the local WebSocket gateway first (gateway.go) and
// fall back to the CLI only if the gateway is unavailable. This means openclaw
// does NOT need to be on PATH as long as the gateway process is running.
package openclaw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
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

// ListSessions returns all OpenClaw sessions.
// Tries the local gateway first, then falls back to the openclaw CLI.
func ListSessions() ([]Session, error) {
	// 1. Try gateway
	sessions, gwErr := GatewaySessionsList()
	if gwErr == nil && len(sessions) > 0 {
		return sessions, nil
	}
	if gwErr != nil {
		common.Debugf("openclaw gateway unavailable (%v), falling back to CLI", gwErr)
	}

	// 2. Fall back to CLI
	bin, err := findOpenclawBinary()
	if err != nil {
		return nil, fmt.Errorf("openclaw sessions: gateway unavailable and %w", err)
	}
	cmd := exec.Command(bin, "sessions", "--json")
	cmd.Env = enrichedEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("openclaw sessions (cli): %w", err)
	}
	raw := extractJSON(out)
	if raw == nil {
		return nil, fmt.Errorf("openclaw sessions (cli): parse JSON: no valid JSON found in output: %s", string(out[:min(200, len(out))]))
	}

	var result sessionsOutput
	if err := json.Unmarshal(raw, &result); err == nil && len(result.Sessions) > 0 {
		return result.Sessions, nil
	}
	var list []Session
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("openclaw sessions (cli): parse JSON: %w", err)
	}
	return list, nil
}

// GetMostRecentSession returns the most recently updated session.
func GetMostRecentSession() (*Session, error) {
	sessions, err := ListSessions()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("openclaw sessions: no sessions found")
	}
	s := &sessions[0]
	s.SessionID = strings.TrimSpace(s.SessionID)
	if s.SessionID == "" {
		return nil, fmt.Errorf("openclaw sessions: first session has empty sessionId")
	}
	return s, nil
}

// FindMostRecentDeliverableSession scans all OpenClaw sessions and returns
// the most recently updated one whose key contains a deliverable channel
// (e.g. feishu). Returns nil if no deliverable session is found.
func FindMostRecentDeliverableSession() (*Session, error) {
	sessions, err := ListSessions()
	if err != nil {
		return nil, err
	}
	var best *Session
	for i := range sessions {
		s := &sessions[i]
		channel, target := ParseSessionKey(s.Key)
		if channel == "" || target == "" {
			continue
		}
		if best == nil || s.UpdatedAt > best.UpdatedAt {
			best = s
		}
	}
	return best, nil
}

// ResolvePushSession picks the best session for push delivery from a list.
// Priority (mirrors JS openclaw-routing.js):
//  1. Channel session with feishu preferred (most recently updated)
//  2. Any non-main session (most recently updated)
//  3. First session with a valid sessionId
func ResolvePushSession(sessions []Session) *Session {
	if len(sessions) == 0 {
		return nil
	}

	// 1. Pick best channel session (feishu preferred)
	var bestChannel *Session
	for i := range sessions {
		s := &sessions[i]
		ch, tgt := ParseSessionKey(s.Key)
		if ch == "" || tgt == "" {
			continue
		}
		if bestChannel == nil {
			bestChannel = s
			continue
		}
		// Prefer feishu over other channels
		bestIsFeishu := strings.Contains(strings.ToLower(bestChannel.Key), "feishu")
		curIsFeishu := strings.Contains(strings.ToLower(s.Key), "feishu")
		if curIsFeishu && !bestIsFeishu {
			bestChannel = s
		} else if curIsFeishu == bestIsFeishu && s.UpdatedAt > bestChannel.UpdatedAt {
			bestChannel = s
		}
	}
	if bestChannel != nil && strings.TrimSpace(bestChannel.SessionID) != "" {
		return bestChannel
	}

	// 2. Any non-main session
	var bestNonMain *Session
	for i := range sessions {
		s := &sessions[i]
		if s.Key == "agent:main:main" || strings.TrimSpace(s.SessionID) == "" {
			continue
		}
		if bestNonMain == nil || s.UpdatedAt > bestNonMain.UpdatedAt {
			bestNonMain = s
		}
	}
	if bestNonMain != nil {
		return bestNonMain
	}

	// 3. Fallback: first session with valid sessionId
	for i := range sessions {
		s := &sessions[i]
		if strings.TrimSpace(s.SessionID) != "" {
			return s
		}
	}
	return nil
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

// SendToSession injects a message into an AI session, triggering the agent to reply.
// When deliver is true, uses the "agent" RPC (not chat.send) so the AI's reply
// is reliably routed to the session's external channel (e.g. Feishu).
// Tries the gateway first, falls back to the CLI.
func SendToSession(sessionKey, message string, deliver ...bool) error {
	shouldDeliver := len(deliver) > 0 && deliver[0]

	// 1. Try gateway
	if shouldDeliver {
		// Use "agent" RPC for delivery — it uses resolveAgentDeliveryPlan
		// which has no client mode restrictions (unlike chat.send).
		// Must pass channel and target explicitly for feishu delivery.
		channel, target := ParseSessionKey(sessionKey)
		if err := GatewayAgentSend(sessionKey, message, true, channel, target); err == nil {
			return nil
		}
	} else {
		// Use chat.send for internal-only push (no external delivery).
		if err := GatewayChatSend(sessionKey, message); err == nil {
			return nil
		}
	}

	// 2. Fall back to CLI
	sess, err := GetMostRecentSession()
	if err != nil {
		return fmt.Errorf("SendToSession fallback: cannot resolve session: %w", err)
	}

	bin, binErr := findOpenclawBinary()
	if binErr != nil {
		return fmt.Errorf("openclaw not available (gateway and cli both failed): %w", binErr)
	}
	args := []string{"agent",
		"--session-id", sess.SessionID,
		"--message", message,
	}
	if shouldDeliver {
		args = append(args, "--deliver")
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = enrichedEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return fmt.Errorf("openclaw agent (cli): %w", err)
		}
		return fmt.Errorf("openclaw agent (cli): %w — %s", err, detail)
	}
	return nil
}

// SendMediaToChannel sends a message with an optional media attachment directly
// through an external channel (e.g. Feishu), bypassing the AI agent.
// mediaPath must be a local file path or empty (text-only).
// Tries the gateway first (passing a file:// URL), falls back to the CLI.
func SendMediaToChannel(channel, target, message, mediaPath string) error {
	// 1. Try gateway (send RPC)
	mediaURL := ""
	if mediaPath != "" {
		// Convert local path to file:// URL so OpenClaw gateway can read it.
		abs, err := filepath.Abs(mediaPath)
		if err == nil {
			mediaURL = "file://" + abs
		}
	}
	if err := GatewaySend(channel, target, message, mediaURL); err == nil {
		return nil
	}

	// 2. Fall back to CLI
	bin, err := findOpenclawBinary()
	if err != nil {
		return fmt.Errorf("SendMediaToChannel: gateway unavailable and %w", err)
	}
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
	cmd := exec.Command(bin, args...)
	cmd.Env = enrichedEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return fmt.Errorf("openclaw message send (cli): %w", err)
		}
		return fmt.Errorf("openclaw message send (cli): %w — %s", err, detail)
	}
	return nil
}

// findOpenclawBinary locates the openclaw executable.
// It tries $PATH first, then common install locations (including Homebrew and nvm paths).
func findOpenclawBinary() (string, error) {
	if path, err := exec.LookPath("openclaw"); err == nil {
		return path, nil
	}
	home := os.Getenv("HOME")
	candidates := []string{
		filepath.Join(home, ".local/bin/openclaw"),
		filepath.Join(home, "bin/openclaw"),
		"/usr/local/bin/openclaw",
		"/opt/homebrew/bin/openclaw",
		"/usr/bin/openclaw",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("openclaw binary not found in PATH or common locations")
}

// findNodeBinary locates the node executable so Node.js-based CLI wrappers can run.
// Under LaunchAgent / nohup the PATH is minimal and nvm is not initialised.
func findNodeBinary() string {
	if path, err := exec.LookPath("node"); err == nil {
		return path
	}
	home := os.Getenv("HOME")
	// nvm installs node under ~/.nvm/versions/node/<version>/bin/node
	nvmDir := filepath.Join(home, ".nvm", "versions", "node")
	if entries, err := os.ReadDir(nvmDir); err == nil {
		// pick the last (highest) version directory
		for i := len(entries) - 1; i >= 0; i-- {
			candidate := filepath.Join(nvmDir, entries[i].Name(), "bin", "node")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	// Homebrew node (Apple Silicon / Intel)
	for _, p := range []string{
		"/opt/homebrew/bin/node",
		"/usr/local/bin/node",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// extractJSON finds the first complete JSON object or array in raw output.
// This handles CLI output that may contain log lines or multiple JSON values.
func extractJSON(data []byte) []byte {
	// Find the first '{' or '[' that starts a valid JSON value.
	for _, opener := range []byte{'{', '['} {
		idx := bytes.IndexByte(data, opener)
		if idx < 0 {
			continue
		}
		candidate := data[idx:]
		// Use json.Decoder to read exactly one value.
		dec := json.NewDecoder(bytes.NewReader(candidate))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == nil {
			return []byte(raw)
		}
	}
	return nil
}

// enrichedEnv returns os.Environ() with the node binary's directory prepended to PATH,
// so that Node.js-based CLI wrappers (like openclaw) work under restricted environments.
func enrichedEnv() []string {
	nodeBin := findNodeBinary()
	if nodeBin == "" {
		return os.Environ()
	}
	nodeDir := filepath.Dir(nodeBin)
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + nodeDir + string(os.PathListSeparator) + e[5:]
			return env
		}
	}
	return append(env, "PATH="+nodeDir)
}
