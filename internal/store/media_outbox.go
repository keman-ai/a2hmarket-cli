package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ─── SQL strings ─────────────────────────────────────────────────────────────

const sqlEnqueueMedia = `
INSERT INTO media_outbox(
	event_id, session_key, channel, to_target, account_id, thread_id,
	message_text, media_url, status, attempt, next_retry_at, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,'PENDING',0,?,?,?)`

const sqlListPendingMedia = `
SELECT id, event_id, session_key, channel, to_target, account_id, thread_id,
       message_text, media_url, status, attempt, next_retry_at, updated_at
FROM media_outbox
WHERE status IN ('PENDING','RETRY') AND next_retry_at <= ?
ORDER BY id ASC
LIMIT ?`

const sqlMarkMediaSent = `
UPDATE media_outbox SET status='SENT', updated_at=?, last_error=NULL WHERE id=?`

const sqlMarkMediaRetry = `
UPDATE media_outbox
SET status='RETRY', attempt=?, next_retry_at=?, updated_at=?, last_error=?
WHERE id=?`

const sqlMarkMediaFailed = `
UPDATE media_outbox SET status='FAILED', updated_at=?, last_error=? WHERE id=?`

// ─── Types ───────────────────────────────────────────────────────────────────

// MediaOutboxRow is a pending media notification.
type MediaOutboxRow struct {
	ID          int64
	EventID     string
	SessionKey  string
	Channel     string
	ToTarget    string
	AccountID   string
	ThreadID    string
	MessageText string
	MediaURL    string
	Status      string
	Attempt     int
	NextRetryAt int64
	UpdatedAt   int64
}

// MediaEnqueueInput is the parameter struct for EnqueueMediaOutbox.
type MediaEnqueueInput struct {
	EventID     string
	SessionKey  string
	Channel     string
	To          string
	AccountID   string
	ThreadID    string
	MessageText string
	MediaURL    string
}

// MediaEnqueueResult is returned by EnqueueMediaOutbox.
type MediaEnqueueResult struct {
	Inserted bool
	Reason   string
}

// ─── Methods ─────────────────────────────────────────────────────────────────

// EnqueueMediaOutbox adds a media notification to the queue.
func (s *EventStore) EnqueueMediaOutbox(ctx context.Context, input MediaEnqueueInput) (MediaEnqueueResult, error) {
	ts := nowMs()
	_, err := s.stmtEnqueueMedia.ExecContext(ctx,
		input.EventID,
		nullableText(input.SessionKey),
		input.Channel,
		input.To,
		nullableText(input.AccountID),
		nullableText(input.ThreadID),
		input.MessageText,
		nullableText(input.MediaURL),
		ts, // next_retry_at
		ts, // created_at
		ts, // updated_at
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return MediaEnqueueResult{Inserted: false, Reason: "duplicate"}, nil
		}
		return MediaEnqueueResult{}, fmt.Errorf("enqueue media_outbox: %w", err)
	}
	return MediaEnqueueResult{Inserted: true}, nil
}

// ListPendingMediaOutbox returns PENDING/RETRY rows due for dispatch.
func (s *EventStore) ListPendingMediaOutbox(ctx context.Context, nowUnixMs int64, batchSize int) ([]MediaOutboxRow, error) {
	if batchSize <= 0 {
		batchSize = 20
	}
	rows, err := s.stmtListPendingMedia.QueryContext(ctx, nowUnixMs, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MediaOutboxRow
	for rows.Next() {
		var r MediaOutboxRow
		var sessKey, accountID, threadID, mediaURL sql.NullString
		if err := rows.Scan(
			&r.ID, &r.EventID, &sessKey, &r.Channel, &r.ToTarget,
			&accountID, &threadID, &r.MessageText, &mediaURL,
			&r.Status, &r.Attempt, &r.NextRetryAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		r.SessionKey = fromNullableText(sessKey)
		r.AccountID = fromNullableText(accountID)
		r.ThreadID = fromNullableText(threadID)
		r.MediaURL = fromNullableText(mediaURL)
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkMediaSent marks the row as SENT.
func (s *EventStore) MarkMediaSent(ctx context.Context, outboxID int64) error {
	ts := nowMs()
	_, err := s.stmtMarkMediaSent.ExecContext(ctx, ts, outboxID)
	return err
}

// MarkMediaRetry marks the row for retry.
func (s *EventStore) MarkMediaRetry(ctx context.Context, outboxID int64, attempt int, nextRetryAt int64, lastError string) error {
	ts := nowMs()
	_, err := s.stmtMarkMediaRetry.ExecContext(ctx,
		attempt, nextRetryAt, ts, truncate(lastError, 1000), outboxID)
	return err
}

// MarkMediaFailed permanently marks the row as FAILED (no more retries).
func (s *EventStore) MarkMediaFailed(ctx context.Context, outboxID int64, lastError string) error {
	ts := nowMs()
	_, err := s.stmtMarkMediaFailed.ExecContext(ctx, ts, truncate(lastError, 1000), outboxID)
	return err
}
