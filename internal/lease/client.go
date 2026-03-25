// Package lease provides a client for the agent-service control plane lease API.
//
// The lease API coordinates which runtime instance is the "leader" (active)
// and which are "followers" (standby) for a given agentId.
//
// API paths mirror runtime/js/src/listener/lease-client.js.
package lease

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/api"
)

const (
	pathAcquire   = "/agent-service/api/v1/agent-runtime/lease/acquire"
	pathHeartbeat = "/agent-service/api/v1/agent-runtime/lease/heartbeat"
	pathTakeover  = "/agent-service/api/v1/agent-runtime/lease/takeover"
	pathStatus    = "/agent-service/api/v1/agent-runtime/lease/status"

	requestTimeout = 10 * time.Second
)

// Role indicates whether this instance is leader or follower.
type Role string

const (
	RoleLeader     Role = "leader"
	RoleFollower   Role = "follower"
	RoleStandalone Role = "standalone"
)

// OldLeaseInfo contains info about the displaced leader after a takeover.
type OldLeaseInfo struct {
	InstanceID string `json:"instanceId"`
	Hostname   string `json:"hostname"`
	ClientID   string `json:"clientId"`
}

// AcquireResult is returned from the acquire call.
type AcquireResult struct {
	Role             Role          `json:"role"`
	Epoch            int64         `json:"epoch"`
	LeaseUntil       int64         `json:"leaseUntil"`       // Unix milliseconds
	LeaderInstanceID string        `json:"leaderInstanceId"` // populated when follower
	OldLease         *OldLeaseInfo `json:"oldLease"`         // populated when takeover displaced a leader
}

// HeartbeatResult is returned from the heartbeat call.
type HeartbeatResult struct {
	OK         bool   `json:"ok"`
	Reason     string `json:"reason"`
	Epoch      int64  `json:"epoch"`
	LeaseUntil int64  `json:"leaseUntil"`
}

// TakeoverResult is returned from the takeover call.
// The server wraps the response in {"success":true,"data":{...}}; the do()
// method already returns an error when success!=true, so a nil error from
// Takeover() means the takeover succeeded on the server side.
type TakeoverResult struct {
	Role             Role   `json:"role"`
	Epoch            int64  `json:"epoch"`
	PrevLeaderID     string `json:"prevLeaderId"`
	LeaseUntil       int64  `json:"leaseUntil"`
	LeaderInstanceID string `json:"leaderInstanceId"`
}

// StatusResult is returned from the status call.
type StatusResult struct {
	LeaderInstanceID string `json:"leaderInstanceId"`
	Epoch            int64  `json:"epoch"`
	LeaseUntil       int64  `json:"leaseUntil"`
	MyRole           Role   `json:"myRole"`
}

// Client is the lease control-plane HTTP client.
type Client struct {
	baseURL  string
	agentID  string
	agentKey string
	http     *http.Client
}

// NewClient creates a new lease client.
// baseURL is the platform base URL (e.g. "http://api.a2hmarket.ai").
func NewClient(baseURL, agentID, agentKey string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		agentID:  agentID,
		agentKey: agentKey,
		http:     &http.Client{Timeout: requestTimeout},
	}
}

// AcquireRequest holds the parameters for the acquire call.
type AcquireRequest struct {
	InstanceID     string `json:"instanceId"`
	ClientID       string `json:"clientId"`
	DeviceLabel    string `json:"deviceLabel,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
	IP             string `json:"ip,omitempty"`
	ForceTakeover  bool   `json:"forceTakeover,omitempty"`
}

// Acquire requests or renews the leader lease for this instance.
// Called once on startup.
func (c *Client) Acquire(req AcquireRequest) (*AcquireResult, error) {
	var result AcquireResult
	if err := c.post(pathAcquire, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Heartbeat renews the lease while this instance holds the leader role.
// epoch must match the epoch returned from Acquire.
func (c *Client) Heartbeat(instanceID string, epoch int64) (*HeartbeatResult, error) {
	var result HeartbeatResult
	body := map[string]interface{}{
		"instanceId": instanceID,
		"epoch":      epoch,
	}
	if err := c.post(pathHeartbeat, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// TakeoverRequest holds the parameters for an explicit takeover.
type TakeoverRequest struct {
	InstanceID     string `json:"instanceId"`
	ClientID       string `json:"clientId"`
	DeviceLabel    string `json:"deviceLabel,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
	IP             string `json:"ip,omitempty"`
}

// Takeover explicitly seizes the leader role for this instance.
// The existing leader will demote on its next heartbeat.
func (c *Client) Takeover(req TakeoverRequest) (*TakeoverResult, error) {
	var result TakeoverResult
	if err := c.post(pathTakeover, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Status queries the current lease state.
// instanceID is optional; when provided the response includes MyRole for this instance.
func (c *Client) Status(instanceID string) (*StatusResult, error) {
	apiPath := pathStatus
	if instanceID != "" {
		apiPath += "?instanceId=" + instanceID
	}
	var result StatusResult
	if err := c.get(apiPath, pathStatus, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) buildHeaders(method, path string) http.Header {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := api.ComputeHTTPSignature(c.agentKey, method, path, c.agentID, ts)
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("X-Agent-Id", c.agentID)
	h.Set("X-Timestamp", ts)
	h.Set("X-Agent-Signature", sig)
	return h
}

func (c *Client) post(apiPath string, body, dest interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("lease: marshal body: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+apiPath, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("lease: build request: %w", err)
	}
	req.Header = c.buildHeaders("POST", apiPath)

	return c.do(req, dest)
}

func (c *Client) get(apiPath, signPath string, dest interface{}) error {
	req, err := http.NewRequest("GET", c.baseURL+apiPath, nil)
	if err != nil {
		return fmt.Errorf("lease: build request: %w", err)
	}
	req.Header = c.buildHeaders("GET", signPath)

	return c.do(req, dest)
}

func (c *Client) do(req *http.Request, dest interface{}) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("lease: request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("lease: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("lease: HTTP %d: %s", resp.StatusCode, trimStr(string(raw), 200))
	}

	var wrapper struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return fmt.Errorf("lease: parse response: %w", err)
	}
	if !wrapper.Success {
		return fmt.Errorf("lease: server error: %s", wrapper.Error)
	}

	if dest != nil && len(wrapper.Data) > 0 {
		if err := json.Unmarshal(wrapper.Data, dest); err != nil {
			return fmt.Errorf("lease: unmarshal data: %w", err)
		}
	}
	return nil
}

func trimStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
