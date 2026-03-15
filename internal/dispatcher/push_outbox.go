package dispatcher

import (
	"context"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/openclaw"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// PushDispatchConfig controls push_outbox flush behaviour.
type PushDispatchConfig struct {
	BatchSize  int // rows per flush (default 20)
	MaxDelayMs int // max retry back-off in ms (default 300_000 = 5 min)
}

// PushStats summarises one flush run.
type PushStats struct {
	Sent    int
	Retried int
	Skipped int
}

// FlushPushOutbox reads pending push_outbox rows and delivers them to OpenClaw
// via `openclaw sessions --json` + `openclaw agent --session-id <id> --message <msg>`.
//
// Session discovery is best-effort: the first session returned by `openclaw sessions`
// (ordered by updatedAt desc) is used as the push target.
func FlushPushOutbox(ctx context.Context, es *store.EventStore, cfg PushDispatchConfig) (PushStats, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if cfg.MaxDelayMs <= 0 {
		cfg.MaxDelayMs = 300_000
	}

	nowUnixMs := time.Now().UnixMilli()

	rows, err := es.ListPendingPushOutbox(ctx, nowUnixMs, cfg.BatchSize)
	if err != nil {
		return PushStats{}, err
	}
	if len(rows) == 0 {
		return PushStats{}, nil
	}

	// Resolve the target session once per flush (avoids repeated subprocess calls).
	sessionID, sessErr := openclaw.GetMostRecentSessionID()
	if sessErr != nil {
		common.Warnf("push: cannot resolve openclaw session: %v", sessErr)
		// Don't retry individual rows yet; wait for the next tick.
		return PushStats{}, nil
	}

	var stats PushStats
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}

		text := openclaw.FormatPushText(row)
		sendErr := openclaw.SendToSession(sessionID, text)

		// Use a fresh background context for DB writes: the openclaw agent command
		// may take several seconds, potentially exhausting the caller's deadline
		// before we can persist the outcome.
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)

		if sendErr == nil {
			// Mark SENT; ack deadline = now + 15 s (simple, no consumer ACK needed here)
			ackDeadline := time.Now().UnixMilli() + 15_000
			if err := es.MarkPushSent(dbCtx, row.OutboxID, row.EventID, ackDeadline); err != nil {
				common.Warnf("push: mark sent failed event_id=%s: %v", row.EventID, err)
			} else {
				common.Infof("push: delivered event_id=%s session=%s", row.EventID, sessionID)
				stats.Sent++
			}
			dbCancel()
			continue
		}

		// Delivery failed — schedule retry with exponential backoff.
		nextAttempt := row.Attempt + 1
		delayMs := CalculateBackoffMs(nextAttempt, int64(cfg.MaxDelayMs))
		nextRetryAt := time.Now().UnixMilli() + delayMs
		if err := es.MarkPushRetry(dbCtx, row.OutboxID, nextAttempt, nextRetryAt, sendErr.Error()); err != nil {
			common.Warnf("push: mark retry failed event_id=%s: %v", row.EventID, err)
		}
		dbCancel()
		common.Warnf("push: delivery failed event_id=%s attempt=%d retry_in_ms=%d: %v",
			row.EventID, nextAttempt, int(delayMs), sendErr)
		stats.Retried++
	}

	return stats, nil
}
