package a2a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	mqttpkg "github.com/keman-ai/a2hmarket-cli/internal/mqtt"
	"github.com/keman-ai/a2hmarket-cli/internal/protocol"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// A2AService binds an MQTT transport to an EventStore and handles inbound message routing.
// Use NewA2AService then call Start() before MQTT subscribes.
type A2AService struct {
	es        *store.EventStore
	transport *mqttpkg.Transport
	cfg       RouterConfig

	// stats
	received  atomic.Int64
	accepted  atomic.Int64
	rejected  atomic.Int64
}

// NewA2AService creates a new A2AService.
func NewA2AService(es *store.EventStore, transport *mqttpkg.Transport, cfg RouterConfig) *A2AService {
	return &A2AService{
		es:        es,
		transport: transport,
		cfg:       cfg,
	}
}

// Route processes a single MQTT message, routing it into the store.
// Call this from the transport's OnMessage handler.
func (s *A2AService) Route(msg mqttpkg.Message) {
	s.received.Add(1)

	ctx := context.Background()
	result := HandleA2AMessage(ctx, s.es, s.cfg, msg.Topic, msg.Payload)
	if result.Accepted {
		s.accepted.Add(1)
		common.Debugf("a2a: accepted event_id=%s peer=%s msg_id=%s",
			result.EventID, result.PeerID, result.MessageID)
	} else {
		s.rejected.Add(1)
		common.Debugf("a2a: rejected reason=%s topic=%s", result.Reason, msg.Topic)
	}
}

// Start registers Route as the transport's OnMessage handler.
// Prefer wiring Route manually when you need a combined handler (e.g., verbose logging).
func (s *A2AService) Start() {
	s.transport.OnMessage(func(msg mqttpkg.Message) {
		s.Route(msg)
	})
}

// Stop is a no-op placeholder for cleanup; the transport handles actual connection cleanup.
func (s *A2AService) Stop() {}

// Stats returns the current counters.
func (s *A2AService) Stats() (received, accepted, rejected int64) {
	return s.received.Load(), s.accepted.Load(), s.rejected.Load()
}

// PublishEnvelope signs and publishes an envelope to the target agent via MQTT.
func (s *A2AService) PublishEnvelope(targetAgentID string, env *protocol.Envelope, qos int) error {
	if env == nil {
		return fmt.Errorf("a2a: nil envelope")
	}
	_ = qos // qos is set on the transport level; keep for API parity with plan

	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("a2a: marshal envelope: %w", err)
	}

	// Reuse transport.Publish which accepts interface{} and marshals to JSON.
	return s.transport.Publish(targetAgentID, json.RawMessage(data))
}

// randomHex6 generates a 6-byte (12 hex chars) random suffix for event IDs.
func randomHex6() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
