package dispatcher

import (
	"context"
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
// Each row is dispatched in its own goroutine so that slow openclaw agent
// calls (which involve an LLM round-trip of 30-120 s) do NOT block the
// caller's heartbeat ticker.  The caller's context is only used for the
// initial DB read; individual delivery goroutines use independent contexts.
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

	session, sessErr := openclaw.GetMostRecentSession()
	if sessErr != nil {
		common.Warnf("push: cannot resolve openclaw session, skipping %d rows (will retry next tick): %v", len(rows), sessErr)
		return PushStats{Skipped: len(rows), SessionUnavailable: true}, nil
	}

	channel, target := openclaw.ParseSessionKey(session.Key)

	var stats PushStats
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}

		// Mark in-flight immediately so the next flush tick doesn't pick it
		// up again while the goroutine is still waiting for OpenClaw.
		// We use status=SENT with a generous ack deadline; the goroutine will
		// update it to SENT (on success) or RETRY (on failure) when done.
		inflight := time.Now().UnixMilli() + int64(cfg.MaxDelayMs)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := es.MarkPushSent(dbCtx, row.OutboxID, row.EventID, inflight); err != nil {
			dbCancel()
			common.Warnf("push: mark inflight failed event_id=%s: %v", row.EventID, err)
			continue
		}
		dbCancel()

		rowCopy := row
		sessCopy := session
		dispatchAsync(es, rowCopy, sessCopy, channel, target, cfg)
		stats.Sent++
	}

	return stats, nil
}

// dispatchAsync runs in a goroutine per message.  It calls openclaw (slow),
// then updates the DB with the outcome.
func dispatchAsync(es *store.EventStore, row store.PushOutboxRow, session *openclaw.Session, channel, target string, cfg PushDispatchConfig) {
	sendErr := dispatchRow(row, session, channel, target)

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()

	if sendErr == nil {
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

// dispatchRow decides the best delivery method for a single push row.
func dispatchRow(row store.PushOutboxRow, session *openclaw.Session, channel, target string) error {
	att := openclaw.ExtractAttachment(row)

	if att == nil {
		text := openclaw.FormatPushText(row)
		return openclaw.SendToSession(session.Key, text)
	}

	if channel != "" && target != "" {
		return dispatchWithMedia(row, att, channel, target, session)
	}

	text := openclaw.FormatExternalURLText(row, att)
	return openclaw.SendToSession(session.Key, text)
}

// dispatchWithMedia downloads the attachment and sends it via openclaw message send.
func dispatchWithMedia(row store.PushOutboxRow, att *openclaw.AttachmentInfo, channel, target string, session *openclaw.Session) error {
	localPath, dlErr := openclaw.DownloadFile(att.URL, att.Name)
	if dlErr != nil {
		common.Warnf("push: download failed (%s), falling back to URL text: %v", att.Name, dlErr)
		text := openclaw.FormatExternalURLText(row, att)
		return openclaw.SendToSession(session.Key, text)
	}

	common.Debugf("push: downloaded %s → %s", att.Name, localPath)

	msgText := openclaw.FormatPushTextForMedia(row)
	sendErr := openclaw.SendMediaToChannel(channel, target, msgText, localPath)
	if sendErr != nil {
		common.Warnf("push: media send failed, falling back to agent: %v", sendErr)
		text := openclaw.FormatExternalURLText(row, att)
		return openclaw.SendToSession(session.Key, text)
	}
	return nil
}
