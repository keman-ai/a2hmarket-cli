package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ─── SQL strings ─────────────────────────────────────────────────────────────

const sqlEnqueuePushOutbox = `
INSERT INTO push_outbox(event_id, target, attempt, next_retry_at, status, created_at, updated_at)
VALUES(?,?,0,?,'PENDING',?,?)`

const sqlListPendingPush = `
SELECT o.id, o.event_id, o.attempt,
       e.peer_id, e.unread_count, e.preview, e.payload_json, e.state,
       e.target_session_id, e.target_session_key
FROM push_outbox o
JOIN message_event e ON e.event_id = o.event_id
WHERE o.status IN ('PENDING','RETRY') AND o.next_retry_at <= ?
ORDER BY o.id ASC
LIMIT ?`

const sqlListSentPush = `
SELECT o.id, o.event_id, o.attempt, o.next_retry_at, o.updated_at,
       e.peer_id, e.unread_count, e.preview, e.payload_json, e.state,
       e.target_session_id, e.target_session_key
FROM push_outbox o
JOIN message_event e ON e.event_id = o.event_id
WHERE o.status = 'SENT'
ORDER BY o.id ASC
LIMIT ?`

const sqlMarkPushSent = `
UPDATE push_outbox SET status='SENT', next_retry_at=?, updated_at=?, last_error=NULL WHERE id=?`

const sqlMarkPushSentEvent = `
UPDATE message_event SET state='PUSHED', updated_at=? WHERE event_id=?`

const sqlMarkPushAcked = `
UPDATE push_outbox SET status='ACKED', updated_at=?, last_error=NULL WHERE id=?`

const sqlMarkPushAckedEvent = `
UPDATE message_event SET state='CONSUMED', updated_at=? WHERE event_id=?`

const sqlMarkPushRetry = `
UPDATE push_outbox SET status='RETRY', attempt=?, next_retry_at=?, updated_at=?, last_error=? WHERE id=?`

// ─── Types ───────────────────────────────────────────────────────────────────

// PushOutboxRow is a row from push_outbox joined with message_event.
type PushOutboxRow struct {
	OutboxID        int64
	EventID         string
	Attempt         int
	PeerID          string
	UnreadCount     int
	Preview         string
	State           string
	TargetSessionID string
	TargetSessionKey string
	Payload         map[string]interface{}
	// Only set for SENT rows:
	NextRetryAt int64
	UpdatedAt   int64
}

// ─── Methods ─────────────────────────────────────────────────────────────────

// ListPendingPushOutbox returns PENDING/RETRY rows due for dispatch.
func (s *EventStore) ListPendingPushOutbox(ctx context.Context, nowUnixMs int64, batchSize int) ([]PushOutboxRow, error) {
	if batchSize <= 0 {
		batchSize = 20
	}
	rows, err := s.stmtListPendingPush.QueryContext(ctx, nowUnixMs, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPushRows(rows, false)
}

// ListSentPushOutbox returns rows with status=SENT (waiting for ACK or retry).
func (s *EventStore) ListSentPushOutbox(ctx context.Context, batchSize int) ([]PushOutboxRow, error) {
	if batchSize <= 0 {
		batchSize = 20
	}
	rows, err := s.stmtListSentPush.QueryContext(ctx, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPushRows(rows, true)
}

func scanPushRows(rows *sql.Rows, includeTimestamps bool) ([]PushOutboxRow, error) {
	var out []PushOutboxRow
	for rows.Next() {
		var r PushOutboxRow
		var tSessID, tSessKey sql.NullString
		var payloadJSON string
		var scanErr error
		if includeTimestamps {
			scanErr = rows.Scan(
				&r.OutboxID, &r.EventID, &r.Attempt, &r.NextRetryAt, &r.UpdatedAt,
				&r.PeerID, &r.UnreadCount, &r.Preview, &payloadJSON, &r.State,
				&tSessID, &tSessKey,
			)
		} else {
			scanErr = rows.Scan(
				&r.OutboxID, &r.EventID, &r.Attempt,
				&r.PeerID, &r.UnreadCount, &r.Preview, &payloadJSON, &r.State,
				&tSessID, &tSessKey,
			)
		}
		if scanErr != nil {
			return nil, scanErr
		}
		r.TargetSessionID = fromNullableText(tSessID)
		r.TargetSessionKey = fromNullableText(tSessKey)
		if err := json.Unmarshal([]byte(payloadJSON), &r.Payload); err != nil {
			r.Payload = map[string]interface{}{"raw": payloadJSON}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkPushSent marks the row as SENT and sets the ACK deadline.
func (s *EventStore) MarkPushSent(ctx context.Context, outboxID int64, eventID string, ackDeadlineAt int64) error {
	ts := nowMs()
	if _, err := s.stmtMarkPushSent.ExecContext(ctx, ackDeadlineAt, ts, outboxID); err != nil {
		return fmt.Errorf("mark push sent: %w", err)
	}
	if _, err := s.stmtMarkPushSentEvent.ExecContext(ctx, ts, eventID); err != nil {
		return fmt.Errorf("mark event pushed: %w", err)
	}
	return nil
}

// MarkPushAcked marks the row as ACKED (push confirmed consumed).
func (s *EventStore) MarkPushAcked(ctx context.Context, outboxID int64, eventID string) error {
	ts := nowMs()
	if _, err := s.stmtMarkPushAcked.ExecContext(ctx, ts, outboxID); err != nil {
		return fmt.Errorf("mark push acked: %w", err)
	}
	if _, err := s.stmtMarkPushAckedEvent.ExecContext(ctx, ts, eventID); err != nil {
		return fmt.Errorf("mark event consumed: %w", err)
	}
	return nil
}

// MarkPushRetry marks the row for retry with exponential backoff.
func (s *EventStore) MarkPushRetry(ctx context.Context, outboxID int64, attempt int, nextRetryAt int64, lastError string) error {
	ts := nowMs()
	_, err := s.stmtMarkPushRetry.ExecContext(ctx,
		attempt, nextRetryAt, ts, truncate(lastError, 1000), outboxID)
	return err
}
