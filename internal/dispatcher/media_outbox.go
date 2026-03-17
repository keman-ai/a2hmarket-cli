package dispatcher

import (
	"context"
	"fmt"
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

// FlushMediaOutbox reads pending media_outbox rows and delivers them via OpenClaw.
//
// Delivery strategy:
//  1. Find the deliverable feishu session from media_outbox row's session_key
//  2. Send message to that session via chat.send with deliver=true
//     → AI processes the message and routes its reply to feishu
//  3. Fallback: if no session key or chat.send fails, use raw send to channel
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

// deliverMedia sends a single media_outbox row.
//
// Primary path: chat.send with deliver=true → AI processes and delivers to feishu.
// Fallback: raw send to channel (bypasses AI, for when session is unavailable).
func deliverMedia(row store.MediaOutboxRow) error {
	sessionKey := row.SessionKey

	// If we have a session key with a deliverable channel, use chat.send + deliver=true.
	// The AI will process the message and route its reply to feishu.
	if sessionKey != "" {
		ch, _ := openclaw.ParseSessionKey(sessionKey)
		if ch != "" {
			message := formatMediaSessionMessage(row)
			err := openclaw.SendToSession(sessionKey, message, true)
			if err == nil {
				return nil
			}
			common.Warnf("media: chat.send+deliver failed, falling back to raw send: %v", err)
		}
	}

	// Fallback: find a deliverable session from openclaw sessions list.
	if sessionKey == "" {
		if ds, _ := openclaw.FindMostRecentDeliverableSession(); ds != nil {
			sessionKey = ds.Key
			message := formatMediaSessionMessage(row)
			err := openclaw.SendToSession(sessionKey, message, true)
			if err == nil {
				return nil
			}
			common.Warnf("media: chat.send+deliver (fallback session) failed: %v", err)
		}
	}

	// Last resort: raw send directly to channel (bypasses AI).
	return deliverMediaRaw(row)
}

// formatMediaSessionMessage formats the summary for AI session delivery.
// The AI will process this and generate a proper response to the human.
func formatMediaSessionMessage(row store.MediaOutboxRow) string {
	parts := []string{
		fmt.Sprintf("[A2H Market 通知 | event:%s]", row.EventID),
		"",
		"以下内容需要转达给人类用户，请通过当前会话的外部渠道发送给人类：",
		"",
	}

	if row.MessageText != "" {
		parts = append(parts, row.MessageText)
	}

	if row.MediaURL != "" {
		parts = append(parts, "", fmt.Sprintf("附带图片: %s", row.MediaURL))
	}

	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result
}

// deliverMediaRaw sends directly to external channel, bypassing AI.
func deliverMediaRaw(row store.MediaOutboxRow) error {
	message := row.MessageText

	if row.MediaURL != "" {
		localPath, dlErr := openclaw.DownloadFile(row.MediaURL, "")
		if dlErr != nil {
			common.Warnf("media: download failed (%s), sending text only: %v", row.MediaURL, dlErr)
		} else {
			return openclaw.SendMediaToChannel(row.Channel, row.ToTarget, message, localPath)
		}
	}

	return openclaw.SendMediaToChannel(row.Channel, row.ToTarget, message, "")
}
