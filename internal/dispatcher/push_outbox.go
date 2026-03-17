package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/openclaw"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// PushDispatchConfig controls push_outbox flush behaviour.
type PushDispatchConfig struct {
	BatchSize  int             // rows per flush (default 20)
	MaxDelayMs int             // max retry back-off in ms (default 300_000 = 5 min)
	WaitGroup  *sync.WaitGroup // optional: track in-flight goroutines for graceful shutdown
}

// PushStats summarises one flush run.
type PushStats struct {
	Sent               int
	Retried            int
	Skipped            int
	SessionUnavailable bool
}

// FlushPushOutbox reads pending push_outbox rows and delivers them to OpenClaw.
//
// Delivery strategy (ported from JS runtime push-dispatcher.js):
//  1. Resolve the best session for push (prefer channel sessions like feishu)
//  2. If the message contains an image (payment_qr) AND the session has a
//     deliverable channel → direct push to external channel (e.g. feishu)
//  3. Always chatSend to the AI session so the agent can process the message
//  4. On success, auto-bind the target_session on the event for future routing
func FlushPushOutbox(ctx context.Context, es *store.EventStore, cfg PushDispatchConfig) (PushStats, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if cfg.MaxDelayMs <= 0 {
		cfg.MaxDelayMs = 300_000
	}

	nowUnixMs := time.Now().UnixMilli()

	// Phase 1: Clean up SENT rows (push_once mode — ported from JS push-dispatcher.js:90-131).
	// Messages are pushed once to the AI session; we don't wait for consumer ACK.
	sentRows, err := es.ListSentPushOutbox(ctx, cfg.BatchSize)
	if err == nil {
		for _, row := range sentRows {
			dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := es.MarkPushAcked(dbCtx, row.OutboxID, row.EventID); err != nil {
				common.Debugf("push: ack cleanup failed event_id=%s: %v", row.EventID, err)
			}
			dbCancel()
		}
		if len(sentRows) > 0 {
			common.Debugf("push: cleaned up %d SENT rows (push_once)", len(sentRows))
		}
	}

	// Phase 2: Dispatch PENDING/RETRY rows.
	rows, err := es.ListPendingPushOutbox(ctx, nowUnixMs, cfg.BatchSize)
	if err != nil {
		return PushStats{}, err
	}
	if len(rows) == 0 {
		return PushStats{}, nil
	}

	// Resolve sessions: get all sessions to pick the best one per row.
	allSessions, sessErr := openclaw.ListSessions()
	if sessErr != nil {
		common.Warnf("push: cannot list openclaw sessions, skipping %d rows (will retry next tick): %v", len(rows), sessErr)
		return PushStats{Skipped: len(rows), SessionUnavailable: true}, nil
	}
	if len(allSessions) == 0 {
		common.Warnf("push: no openclaw sessions found, skipping %d rows", len(rows))
		return PushStats{Skipped: len(rows), SessionUnavailable: true}, nil
	}

	// Pick the best session for push: prefer channel session (feishu), fallback to latest.
	pushSession := openclaw.ResolvePushSession(allSessions)
	if pushSession == nil {
		common.Warnf("push: no valid session resolved, skipping %d rows", len(rows))
		return PushStats{Skipped: len(rows), SessionUnavailable: true}, nil
	}
	channel, target := openclaw.ParseSessionKey(pushSession.Key)

	var stats PushStats
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}

		inflight := time.Now().UnixMilli() + int64(cfg.MaxDelayMs)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := es.MarkPushSent(dbCtx, row.OutboxID, row.EventID, inflight); err != nil {
			dbCancel()
			common.Warnf("push: mark inflight failed event_id=%s: %v", row.EventID, err)
			continue
		}
		dbCancel()

		rowCopy := row
		sessCopy := pushSession
		dispatchDualChannel(es, rowCopy, sessCopy, channel, target, cfg)
		stats.Sent++
	}

	return stats, nil
}

// dispatchDualChannel implements the JS runtime's dual-channel push strategy:
// 1. If image + deliverable channel → direct push to external channel (feishu)
// 2. Always chatSend to AI session
// 3. On success, auto-bind target_session on the event
func dispatchDualChannel(es *store.EventStore, row store.PushOutboxRow, session *openclaw.Session, channel, target string, cfg PushDispatchConfig) {
	att := openclaw.ExtractAttachment(row)

	// Step 1: Direct push to external channel if image is present
	// (ported from JS push-dispatcher.js:162-171)
	if att != nil && att.IsImage && channel != "" && target != "" {
		directErr := directPushToChannel(row, att, channel, target)
		if directErr == nil {
			common.Infof("push: direct push ok (image auto) event_id=%s channel=%s", row.EventID, channel)
		} else {
			common.Warnf("push: direct push failed event_id=%s: %v", row.EventID, directErr)
		}
	}

	// Step 2: Always chatSend to AI session.
	// When the session has a deliverable channel (feishu), use deliver=true
	// so the AI's reply is routed to the external channel, not just webchat.
	shouldDeliver := channel != "" && target != ""
	text := openclaw.FormatPushText(row)
	sendErr := openclaw.SendToSession(session.Key, text, shouldDeliver)

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()

	if sendErr == nil {
		// Step 3: Auto-bind target_session on the event (ported from JS push-dispatcher.js:178-188)
		if channel != "" && target != "" {
			if _, err := es.UpdateEventTargetSession(dbCtx, row.EventID, session.SessionID, session.Key); err != nil {
				common.Debugf("push: bind target_session failed event_id=%s: %v", row.EventID, err)
			}
		}

		ackDeadline := time.Now().UnixMilli() + 15_000
		if err := es.MarkPushSent(dbCtx, row.OutboxID, row.EventID, ackDeadline); err != nil {
			common.Warnf("push: mark sent failed event_id=%s: %v", row.EventID, err)
		} else {
			common.Infof("push: delivered event_id=%s session=%s", row.EventID, session.SessionID)
		}
		return
	}

	nextAttempt := row.Attempt + 1
	delayMs := CalculateBackoffMs(nextAttempt, int64(cfg.MaxDelayMs))
	nextRetryAt := time.Now().UnixMilli() + delayMs
	if err := es.MarkPushRetry(dbCtx, row.OutboxID, nextAttempt, nextRetryAt, sendErr.Error()); err != nil {
		common.Warnf("push: mark retry failed event_id=%s: %v", row.EventID, err)
	}
	common.Warnf("push: delivery failed event_id=%s attempt=%d retry_in_ms=%d: %v",
		row.EventID, nextAttempt, int(delayMs), sendErr)
}

// directPushToChannel downloads the image and sends it directly to the
// external channel (e.g. feishu), bypassing the AI session.
func directPushToChannel(row store.PushOutboxRow, att *openclaw.AttachmentInfo, channel, target string) error {
	localPath, dlErr := openclaw.DownloadFile(att.URL, att.Name)
	if dlErr != nil {
		return fmt.Errorf("download %s: %w", att.Name, dlErr)
	}
	common.Debugf("push: downloaded %s → %s", att.Name, localPath)

	msgText := openclaw.FormatPushTextForMedia(row)
	return openclaw.SendMediaToChannel(channel, target, msgText, localPath)
}
