// gateway.go — WebSocket client for the local OpenClaw gateway.
//
// Protocol summary (v3):
//  1. Dial ws://127.0.0.1:{port}
//  2. Receive event{type:"event", event:"connect.challenge", data:{nonce}}
//  3. Build Ed25519 signature over canonical payload string
//  4. Send req{method:"connect", params:{auth, device:{sig, pubKey, ...}}}
//  5. Execute target RPC (sessions.list / chat.send / send)
//  6. Close
//
// This replaces spawning the `openclaw` CLI and works even when openclaw is
// not on PATH, as long as the gateway process is running locally.
package openclaw

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	gwClientID      = "node-host"
	gwClientVersion = "1.0.0"
	gwClientMode    = "backend"
	gwRole          = "operator"
	gwScopes        = "operator.admin,operator.read,operator.write"
	gwProtocol      = 3
	gwDefaultPort   = 18789
	gwTimeout       = 30 * time.Second
)

// ──────────────────────────────────────────────────────────────────────────────
// Config loaders
// ──────────────────────────────────────────────────────────────────────────────

type gwOpenclawJSON struct {
	Gateway struct {
		Port int `json:"port"`
		Auth struct {
			Mode     string `json:"mode"`
			Token    string `json:"token"`
			Password string `json:"password"`
		} `json:"auth"`
	} `json:"gateway"`
}

type gwDeviceIdentity struct {
	DeviceID      string `json:"deviceId"`
	PrivateKeyPem string `json:"privateKeyPem"`
}

type gwDeviceAuthFile struct {
	Tokens map[string]struct {
		Token string `json:"token"`
	} `json:"tokens"`
}

func gwStateDir() string {
	if v := os.Getenv("OPENCLAW_STATE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("OPENCLAW_HOME"); v != "" {
		return filepath.Join(v, ".openclaw")
	}
	return filepath.Join(os.Getenv("HOME"), ".openclaw")
}

func gwLoadConfig() (*gwOpenclawJSON, error) {
	cfgPath := filepath.Join(gwStateDir(), "openclaw.json")
	if p := os.Getenv("OPENCLAW_CONFIG_PATH"); p != "" {
		cfgPath = p
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("openclaw.json: %w", err)
	}
	// Strip # comments (JSONC-like)
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines = append(lines, "")
		} else {
			lines = append(lines, line)
		}
	}
	var cfg gwOpenclawJSON
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &cfg); err != nil {
		return nil, fmt.Errorf("openclaw.json parse: %w", err)
	}
	if cfg.Gateway.Port == 0 {
		cfg.Gateway.Port = gwDefaultPort
	}
	if cfg.Gateway.Auth.Mode == "" {
		cfg.Gateway.Auth.Mode = "token"
	}
	return &cfg, nil
}

func gwLoadDevice() (*gwDeviceIdentity, error) {
	p := filepath.Join(gwStateDir(), "identity", "device.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("device.json: %w", err)
	}
	var d gwDeviceIdentity
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("device.json parse: %w", err)
	}
	return &d, nil
}

func gwLoadAuthToken(role string) string {
	p := filepath.Join(gwStateDir(), "identity", "device-auth.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var f gwDeviceAuthFile
	if err := json.Unmarshal(data, &f); err != nil {
		return ""
	}
	if entry, ok := f.Tokens[role]; ok {
		return entry.Token
	}
	return ""
}

// ──────────────────────────────────────────────────────────────────────────────
// Ed25519 helpers
// ──────────────────────────────────────────────────────────────────────────────

func gwParsePrivKey(pemStr string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8: %w", err)
	}
	ed, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not Ed25519")
	}
	return ed, nil
}

func gwBase64URL(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	s = strings.ReplaceAll(s, "+", "-")
	s = strings.ReplaceAll(s, "/", "_")
	return strings.TrimRight(s, "=")
}

// gwBuildPayload mirrors buildSignPayloadV3 from the Node.js gateway client.
func gwBuildPayload(deviceID, nonce, token string, signedAtMs int64) string {
	parts := []string{
		"v3",
		deviceID,
		gwClientID,
		gwClientMode,
		gwRole,
		gwScopes,
		fmt.Sprintf("%d", signedAtMs),
		token, // empty string when no token
		nonce,
		strings.ToLower(runtime.GOOS),
		"", // deviceFamily
	}
	return strings.Join(parts, "|")
}

// ──────────────────────────────────────────────────────────────────────────────
// WebSocket RPC primitives
// ──────────────────────────────────────────────────────────────────────────────

type gwReq struct {
	Type   string      `json:"type"`
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

type gwFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Event   string          `json:"event,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`    // older gateway versions
	Payload json.RawMessage `json:"payload,omitempty"` // newer gateway versions use "payload"
}

var gwReqCounter uint64

func gwNextID() string {
	return fmt.Sprintf("a2h_%d_%d", atomic.AddUint64(&gwReqCounter, 1), time.Now().UnixMilli())
}

// sendRPC writes a request and waits for the matching response, skipping
// unrelated events. Must only be called from within a gwSession.
func sendRPC(conn *websocket.Conn, method string, params interface{}) (json.RawMessage, error) {
	id := gwNextID()
	req := gwReq{Type: "req", ID: id, Method: method, Params: params}
	data, _ := json.Marshal(req)

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	conn.SetReadDeadline(time.Now().Add(gwTimeout))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("read response for %s: %w", method, err)
		}
		var f gwFrame
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.ID != id {
			continue // unrelated event or response — skip
		}
		if f.Type == "res" {
			// newer gateway versions put the body in "payload"; older use "result"
			if len(f.Payload) > 0 {
				return f.Payload, nil
			}
			return f.Result, nil
		}
		if f.Type == "err" {
			return nil, fmt.Errorf("gateway %s error: %s", method, string(f.Error))
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// gwSession — authenticated WebSocket session
// ──────────────────────────────────────────────────────────────────────────────

type gwSession struct {
	conn *websocket.Conn
}

// openGatewaySession dials the gateway, waits for challenge, authenticates,
// and returns a ready-to-use session.
func openGatewaySession() (*gwSession, error) {
	cfg, err := gwLoadConfig()
	if err != nil {
		return nil, err
	}
	device, err := gwLoadDevice()
	if err != nil {
		return nil, err
	}
	privKey, err := gwParsePrivKey(device.PrivateKeyPem)
	if err != nil {
		return nil, err
	}
	// Primary: use token from openclaw.json (single source of truth).
	// Fallback: device-auth.json for backward compat with older setups.
	authToken := cfg.Gateway.Auth.Token
	if authToken == "" {
		authToken = gwLoadAuthToken(gwRole)
	}

	url := fmt.Sprintf("ws://127.0.0.1:%d", cfg.Gateway.Port)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("gateway dial %s: %w", url, err)
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	// Wait for connect.challenge
	var nonce string
	for nonce == "" {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("waiting for challenge: %w", err)
		}
		var f gwFrame
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.Type == "event" && f.Event == "connect.challenge" {
			var d struct {
				Nonce string `json:"nonce"`
			}
			// gateway uses "payload" (newer) or "data" (older) for event body
			raw := f.Payload
			if len(raw) == 0 {
				raw = f.Data
			}
			if err := json.Unmarshal(raw, &d); err == nil && d.Nonce != "" {
				nonce = d.Nonce
			}
		}
	}

	// Sign challenge
	signedAtMs := time.Now().UnixMilli()
	payload := gwBuildPayload(device.DeviceID, nonce, authToken, signedAtMs)
	sig := ed25519.Sign(privKey, []byte(payload))

	// Public key: raw 32-byte Ed25519 public key (stripped from PKCS8/SPKI)
	pub := privKey.Public().(ed25519.PublicKey)

	var authField interface{}
	switch {
	case cfg.Gateway.Auth.Mode == "password" && cfg.Gateway.Auth.Password != "":
		authField = map[string]string{"password": cfg.Gateway.Auth.Password}
	case authToken != "":
		authField = map[string]string{"token": authToken}
	}

	connectParams := map[string]interface{}{
		"minProtocol": gwProtocol,
		"maxProtocol": gwProtocol,
		"client": map[string]string{
			"id":       gwClientID,
			"version":  gwClientVersion,
			"platform": strings.ToLower(runtime.GOOS),
			"mode":     gwClientMode,
		},
		"caps":   []interface{}{},
		"role":   gwRole,
		"scopes": strings.Split(gwScopes, ","),
		"auth":   authField,
		"device": map[string]interface{}{
			"id":        device.DeviceID,
			"publicKey": gwBase64URL(pub),
			"signature": gwBase64URL(sig),
			"signedAt":  signedAtMs,
			"nonce":     nonce,
		},
	}

	if _, err := sendRPC(conn, "connect", connectParams); err != nil {
		conn.Close()
		return nil, fmt.Errorf("gateway auth: %w", err)
	}

	return &gwSession{conn: conn}, nil
}

func (s *gwSession) close() {
	if s.conn != nil {
		s.conn.Close()
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Public API — called from session.go
// ──────────────────────────────────────────────────────────────────────────────

// GatewaySessionsList returns the list of sessions via WebSocket sessions.list RPC.
func GatewaySessionsList() ([]Session, error) {
	sess, err := openGatewaySession()
	if err != nil {
		return nil, err
	}
	defer sess.close()

	result, err := sendRPC(sess.conn, "sessions.list", nil)
	if err != nil {
		return nil, fmt.Errorf("sessions.list: %w", err)
	}

	// Response: { "sessions": [...], "count": N, ... }
	// Sessions array may live at top-level or inside a wrapper.
	var wrapper struct {
		Sessions []Session `json:"sessions"`
	}
	if err := json.Unmarshal(result, &wrapper); err == nil && len(wrapper.Sessions) > 0 {
		return wrapper.Sessions, nil
	}
	// Fallback: response is directly an array
	var list []Session
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("sessions.list parse: %w (raw=%s)", err, string(result)[:min(120, len(result))])
	}
	return list, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GatewayChatSend injects a message into an AI session via chat.send RPC.
// Equivalent to: openclaw agent --session-id <id> --message <msg> --deliver
func GatewayChatSend(sessionKey, message string) error {
	sess, err := openGatewaySession()
	if err != nil {
		return err
	}
	defer sess.close()

	nonce := make([]byte, 8)
	rand.Read(nonce)
	idempotencyKey := fmt.Sprintf("a2h_%s_%d", gwBase64URL(nonce), time.Now().UnixMilli())

	params := map[string]interface{}{
		"sessionKey":     sessionKey,
		"message":        message,
		"idempotencyKey": idempotencyKey,
	}
	if _, err := sendRPC(sess.conn, "chat.send", params); err != nil {
		return fmt.Errorf("chat.send: %w", err)
	}
	return nil
}

// GatewaySend sends a message directly to an external channel via send RPC.
// mediaURL may be empty (text-only) or a file:// / https:// URL.
// Equivalent to: openclaw message send --channel <ch> --target <to> --media <url>
func GatewaySend(channel, target, message, mediaURL string) error {
	sess, err := openGatewaySession()
	if err != nil {
		return err
	}
	defer sess.close()

	nonce := make([]byte, 8)
	rand.Read(nonce)

	params := map[string]interface{}{
		"channel":        channel,
		"to":             target,
		"idempotencyKey": fmt.Sprintf("a2h_%s_%d", gwBase64URL(nonce), time.Now().UnixMilli()),
	}
	if message != "" {
		params["message"] = message
	}
	if mediaURL != "" {
		params["mediaUrl"] = mediaURL
	}

	if _, err := sendRPC(sess.conn, "send", params); err != nil {
		return fmt.Errorf("gateway send: %w", err)
	}
	return nil
}
