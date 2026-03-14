package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/protocol"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// PublishFn is the function used to publish an envelope to a target agent.
// It matches the signature of A2AService.PublishEnvelope.
type PublishFn func(targetAgentID string, env *protocol.Envelope, qos int) error

// A2ADispatchConfig tunes the flush behaviour.
type A2ADispatchConfig struct {
	BatchSize  int   // rows per flush (default 50)
	MaxRetries int   // rows with attempt >= MaxRetries are marked FAILED (0 = unlimited)
	MaxDelayMs int64 // max backoff (default 120 000)
}

// A2AFlushStats is returned by FlushA2AOutbox.
type A2AFlushStats struct {
	Sent    int
	Retried int
	Skipped int // rows where max retries exceeded
}

// FlushA2AOutbox reads pending rows from a2a_outbox and publishes them via the provided
// PublishFn. Rows that fail to publish are marked RETRY with exponential backoff.
//
// It mirrors JS flushA2aOutbox() in a2a-outbox-dispatcher.js.
func FlushA2AOutbox(ctx context.Context, es *store.EventStore, publish PublishFn, cfg A2ADispatchConfig) (A2AFlushStats, error) {
	var stats A2AFlushStats

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 50
	}

	rows, err := es.ListPendingA2aOutbox(ctx, time.Now().UnixMilli(), batchSize)
	if err != nil {
		return stats, fmt.Errorf("list pending a2a outbox: %w", err)
	}

	for _, row := range rows {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		// Skip rows that have exceeded max retries.
		if cfg.MaxRetries > 0 && row.Attempt >= cfg.MaxRetries {
			stats.Skipped++
			common.Warnf("a2a-dispatcher: msg_id=%s exceeded max_retries=%d, skipping",
				row.MessageID, cfg.MaxRetries)
			continue
		}

		env, parseErr := envelopeFromRow(row)
		if parseErr != nil {
			common.Warnf("a2a-dispatcher: msg_id=%s invalid envelope: %v", row.MessageID, parseErr)
			_ = es.MarkA2aOutboxRetry(ctx, row.ID, row.Attempt+1,
				NextRetryAt(row.Attempt+1, cfg.MaxDelayMs), parseErr.Error())
			stats.Retried++
			continue
		}

		if err := publish(row.TargetAgentID, env, row.QoS); err != nil {
			nextAttempt := row.Attempt + 1
			nextRetry := NextRetryAt(nextAttempt, cfg.MaxDelayMs)
			_ = es.MarkA2aOutboxRetry(ctx, row.ID, nextAttempt, nextRetry, err.Error())
			common.Warnf("a2a-dispatcher: msg_id=%s publish failed (attempt=%d next_retry_in=%ds): %v",
				row.MessageID, nextAttempt, (nextRetry-time.Now().UnixMilli())/1000, err)
			stats.Retried++
			continue
		}

		if err := es.MarkA2aOutboxSent(ctx, row.ID); err != nil {
			common.Warnf("a2a-dispatcher: msg_id=%s mark sent failed: %v", row.MessageID, err)
		}
		common.Debugf("a2a-dispatcher: sent msg_id=%s target=%s", row.MessageID, row.TargetAgentID)
		stats.Sent++
	}

	return stats, nil
}

// envelopeFromRow reconstructs a protocol.Envelope from the stored JSON.
func envelopeFromRow(row store.A2AOutboxRow) (*protocol.Envelope, error) {
	if row.Envelope == nil {
		return nil, fmt.Errorf("nil envelope map")
	}
	data, err := json.Marshal(row.Envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope map: %w", err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return &env, nil
}
