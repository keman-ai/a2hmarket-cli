package dispatcher

import (
	"context"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/openclaw"
	"github.com/keman-ai/a2hmarket-cli/internal/store"
)

// MediaDispatchConfig controls media_outbox flush behaviour.
type MediaDispatchConfig struct {
	BatchSize  int // rows per flush (default 20)
	MaxRetries int // max retry attempts before FAILED (default 10)
}

// MediaStats summarises one flush run.
type MediaStats struct {
	Sent    int
	Retried int
	Failed  int
}

// FlushMediaOutbox reads pending media_outbox rows and delivers them
// to the external channel (e.g. Feishu) via OpenClaw.
func FlushMediaOutbox(ctx context.Context, es *store.EventStore, cfg MediaDispatchConfig) (MediaStats, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 10
	}

	nowMs := time.Now().UnixMilli()
	rows, err := es.ListPendingMediaOutbox(ctx, nowMs, cfg.BatchSize)
	if err != nil {
		return MediaStats{}, err
	}
	if len(rows) == 0 {
		return MediaStats{}, nil
	}

	var stats MediaStats
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}

		sendErr := deliverMedia(row)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)

		if sendErr == nil {
			if err := es.MarkMediaSent(dbCtx, row.ID); err != nil {
				common.Warnf("media: mark sent failed id=%d: %v", row.ID, err)
			} else {
				common.Infof("media: delivered event_id=%s channel=%s to=%s", row.EventID, row.Channel, row.ToTarget)
			}
			stats.Sent++
			dbCancel()
			continue
		}

		nextAttempt := row.Attempt + 1
		if nextAttempt >= cfg.MaxRetries {
			if err := es.MarkMediaFailed(dbCtx, row.ID, sendErr.Error()); err != nil {
				common.Warnf("media: mark failed id=%d: %v", row.ID, err)
			}
			common.Warnf("media: permanently failed event_id=%s after %d attempts: %v", row.EventID, nextAttempt, sendErr)
			stats.Failed++
		} else {
			delayMs := CalculateBackoffMs(nextAttempt, 300_000)
			nextRetryAt := time.Now().UnixMilli() + delayMs
			if err := es.MarkMediaRetry(dbCtx, row.ID, nextAttempt, nextRetryAt, sendErr.Error()); err != nil {
				common.Warnf("media: mark retry failed id=%d: %v", row.ID, err)
			}
			common.Warnf("media: delivery failed event_id=%s attempt=%d retry_in_ms=%d: %v",
				row.EventID, nextAttempt, int(delayMs), sendErr)
			stats.Retried++
		}
		dbCancel()
	}
	return stats, nil
}

// deliverMedia sends a single media_outbox row to the external channel.
func deliverMedia(row store.MediaOutboxRow) error {
	message := row.MessageText

	// If there's a media URL, download and send with media.
	if row.MediaURL != "" {
		localPath, dlErr := openclaw.DownloadFile(row.MediaURL, "")
		if dlErr != nil {
			common.Warnf("media: download failed (%s), sending text only: %v", row.MediaURL, dlErr)
			// Fall through to text-only send.
		} else {
			return openclaw.SendMediaToChannel(row.Channel, row.ToTarget, message, localPath)
		}
	}

	// Text-only send.
	return openclaw.SendMediaToChannel(row.Channel, row.ToTarget, message, "")
}
