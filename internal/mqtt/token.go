// Package mqtt implements MQTT token fetching and connection utilities.
//
// Token endpoint: POST {apiURL}/mqtt-token/api/v1/token
// Auth: same HMAC-SHA256 signature as the main API client.
package mqtt

import (
	"bytes"
	cryptoRand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/api"
)

const (
	tokenPath             = "/mqtt-token/api/v1/token"
	MQTTClientGroupID     = "GID_agent"
	tokenRefreshThreshold = 30 * time.Minute // refresh when < 30 min remain
)

// Credential is an MQTT temporary credential returned by the token server.
type Credential struct {
	ClientID   string
	Username   string
	Password   string
	ExpireTime int64 // Unix milliseconds
}

func (c *Credential) isValid() bool {
	if c == nil {
		return false
	}
	remaining := time.Until(time.UnixMilli(c.ExpireTime))
	return remaining > tokenRefreshThreshold
}

// BuildClientID returns the base MQTT clientId for a given agentId (used for topic subscription).
// Format: GID_agent@@@{agentId}
func BuildClientID(agentID string) string {
	return fmt.Sprintf("%s@@@%s", MQTTClientGroupID, agentID)
}

// BuildConnectionClientID returns an instance-unique clientId to prevent mutual kicking.
// Format: GID_agent@@@{agentId}_rt_{instanceId}
func BuildConnectionClientID(agentID, instanceID string) string {
	return fmt.Sprintf("%s@@@%s_rt_%s", MQTTClientGroupID, agentID, instanceID)
}

// BuildSendClientID returns a short-lived clientId for one-shot publish (send command).
// Uses a cryptographically random 8-char suffix so concurrent send commands never collide.
// Format: GID_agent@@@{agentId}_pub_{random8}
func BuildSendClientID(agentID string) string {
	const chars = "abcdef0123456789"
	b := make([]byte, 8)
	randBytes := make([]byte, 8)
	_, _ = cryptoRand.Read(randBytes)
	for i := range b {
		b[i] = chars[randBytes[i]%16]
	}
	return fmt.Sprintf("%s@@@%s_pub_%s", MQTTClientGroupID, agentID, b)
}

// IncomingTopic returns the P2P topic this agent subscribes to.
func IncomingTopic(agentID string) string {
	return fmt.Sprintf("P2P_TOPIC/p2p/%s", BuildClientID(agentID))
}

// OutgoingTopic returns the P2P topic for publishing to a target agent.
func OutgoingTopic(targetAgentID string) string {
	return fmt.Sprintf("P2P_TOPIC/p2p/%s", BuildClientID(targetAgentID))
}

// TokenClient fetches and caches MQTT credentials from the token server.
type TokenClient struct {
	apiURL        string // e.g. "http://api.a2hmarket.ai"
	agentID       string
	agentKey      string
	clientVersion string // sent as X-Client-Version header
	http          *http.Client
	mu            sync.Mutex
	cache         map[string]*Credential
}

// NewTokenClient creates a new token client.
// clientVersion is sent as X-Client-Version header for version gate enforcement.
func NewTokenClient(apiURL, agentID, agentKey, clientVersion string) *TokenClient {
	return &TokenClient{
		apiURL:        strings.TrimRight(apiURL, "/"),
		agentID:       agentID,
		agentKey:      agentKey,
		clientVersion: clientVersion,
		http:          &http.Client{Timeout: 15 * time.Second},
		cache:         make(map[string]*Credential),
	}
}

// GetToken returns a valid token for the given clientID, fetching one if needed.
// Pass forceRefresh=true to bypass the cache.
func (tc *TokenClient) GetToken(clientID string, forceRefresh bool) (*Credential, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if !forceRefresh {
		if cred := tc.cache[clientID]; cred.isValid() {
			return cred, nil
		}
	}
	delete(tc.cache, clientID)

	cred, err := tc.fetchToken(clientID)
	if err != nil {
		return nil, err
	}
	tc.cache[clientID] = cred
	return cred, nil
}

// Invalidate removes the cached token for the given clientID.
func (tc *TokenClient) Invalidate(clientID string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	delete(tc.cache, clientID)
}

func (tc *TokenClient) fetchToken(clientID string) (*Credential, error) {
	url := tc.apiURL + tokenPath
	timestampSec := fmt.Sprintf("%d", time.Now().Unix())
	signature := api.ComputeHTTPSignature(tc.agentKey, "POST", tokenPath, tc.agentID, timestampSec)

	body, err := json.Marshal(map[string]string{"client_id": clientID})
	if err != nil {
		return nil, fmt.Errorf("mqtt token: marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mqtt token: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Id", tc.agentID)
	req.Header.Set("X-Timestamp", timestampSec)
	req.Header.Set("X-Agent-Signature", signature)
	if tc.clientVersion != "" {
		req.Header.Set("X-Client-Version", tc.clientVersion)
	}

	resp, err := tc.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mqtt token: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if err != nil {
		return nil, fmt.Errorf("mqtt token: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mqtt token: HTTP %d: %s", resp.StatusCode, trimStr(string(raw), 200))
	}

	var wrapper struct {
		Success bool `json:"success"`
		Data    struct {
			ClientID   string `json:"client_id"`
			Username   string `json:"username"`
			Password   string `json:"password"`
			ExpireTime int64  `json:"expire_time"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("mqtt token: parse response: %w", err)
	}
	if !wrapper.Success {
		return nil, fmt.Errorf("mqtt token: server error: %s", wrapper.Error)
	}

	return &Credential{
		ClientID:   wrapper.Data.ClientID,
		Username:   wrapper.Data.Username,
		Password:   wrapper.Data.Password,
		ExpireTime: wrapper.Data.ExpireTime,
	}, nil
}

func trimStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
