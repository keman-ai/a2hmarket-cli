package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ─── SQL strings ─────────────────────────────────────────────────────────────

const sqlEnqueueA2aOutbox = `
INSERT INTO a2a_outbox(
	message_id, trace_id, target_agent_id, message_type, qos, envelope_json,
	source_session_id, source_session_key, status, attempt, next_retry_at,
	last_error, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,'PENDING',0,?,NULL,?,?)`

const sqlListPendingA2a = `
SELECT id, message_id, trace_id, target_agent_id, message_type, qos, envelope_json,
       source_session_id, source_session_key, status, attempt
FROM a2a_outbox
WHERE status IN ('PENDING','RETRY') AND next_retry_at <= ?
ORDER BY id ASC
LIMIT ?`

const sqlMarkA2aSent = `
UPDATE a2a_outbox SET status='SENT', updated_at=?, last_error=NULL WHERE id=?`

const sqlMarkA2aSentGetRow = `
SELECT target_agent_id, source_session_id, source_session_key, created_at
FROM a2a_outbox WHERE id=? LIMIT 1`

const sqlMarkA2aRetry = `
UPDATE a2a_outbox
SET status='RETRY', attempt=?, next_retry_at=?, updated_at=?, last_error=?
WHERE id=?`

// ─── Types ───────────────────────────────────────────────────────────────────

// A2AOutboxRow is a pending outbound A2A message.
type A2AOutboxRow struct {
	ID              int64
	MessageID       string
	TraceID         string
	TargetAgentID   string
	MessageType     string
	QoS             int
	Envelope        map[string]interface{}
	SourceSessionID string
	SourceSessionKey string
	Status          string
	Attempt         int
}

// EnqueueResult is returned by EnqueueA2aOutbox.
type EnqueueResult struct {
	Created   bool
	MessageID string
}

// ─── Methods ─────────────────────────────────────────────────────────────────

// EnqueueA2aOutbox inserts an outbound A2A message into the queue.
// Returns {Created: false} if message_id already exists (duplicate send protection).
func (s *EventStore) EnqueueA2aOutbox(ctx context.Context, input EnqueueA2aInput) (EnqueueResult, error) {
	if input.MessageID == "" {
		return EnqueueResult{}, fmt.Errorf("message_id is required")
	}
	ts := nowMs()

	envelopeJSON, err := json.Marshal(input.Envelope)
	if err != nil {
		envelopeJSON = []byte("{}")
	}

	_, err = s.stmtEnqueueA2aOutbox.ExecContext(ctx,
		input.MessageID,
		nullableText(input.TraceID),
		input.TargetAgentID,
		input.MessageType,
		input.QoS,
		string(envelopeJSON),
		nullableText(input.SourceSessionID),
		nullableText(input.SourceSessionKey),
		ts, // next_retry_at
		ts, // created_at
		ts, // updated_at
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return EnqueueResult{Created: false, MessageID: input.MessageID}, nil
		}
		return EnqueueResult{}, fmt.Errorf("enqueue a2a_outbox: %w", err)
	}
	return EnqueueResult{Created: true, MessageID: input.MessageID}, nil
}

// EnqueueA2aInput is the parameter struct for EnqueueA2aOutbox.
type EnqueueA2aInput struct {
	MessageID        string
	TraceID          string
	TargetAgentID    string
	MessageType      string
	QoS              int
	Envelope         interface{}
	SourceSessionID  string
	SourceSessionKey string
}

// ListPendingA2aOutbox returns up to batchSize rows that are ready to send.
func (s *EventStore) ListPendingA2aOutbox(ctx context.Context, nowUnixMs int64, batchSize int) ([]A2AOutboxRow, error) {
	if batchSize <= 0 {
		batchSize = 50
	}
	rows, err := s.stmtListPendingA2a.QueryContext(ctx, nowUnixMs, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []A2AOutboxRow
	for rows.Next() {
		var r A2AOutboxRow
		var traceID, sourceSessID, sourceSessKey sql.NullString
		var envelopeJSON string
		if err := rows.Scan(
			&r.ID, &r.MessageID, &traceID, &r.TargetAgentID, &r.MessageType, &r.QoS,
			&envelopeJSON, &sourceSessID, &sourceSessKey, &r.Status, &r.Attempt,
		); err != nil {
			return nil, err
		}
		r.TraceID = fromNullableText(traceID)
		r.SourceSessionID = fromNullableText(sourceSessID)
		r.SourceSessionKey = fromNullableText(sourceSessKey)
		if err := json.Unmarshal([]byte(envelopeJSON), &r.Envelope); err != nil {
			r.Envelope = map[string]interface{}{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkA2aOutboxSent marks the row as SENT and binds the peer session.
func (s *EventStore) MarkA2aOutboxSent(ctx context.Context, id int64) error {
	ts := nowMs()

	// Read session info before updating (for peer binding).
	var targetAgentID, sourceSessID, sourceSessKey sql.NullString
	var createdAt int64
	_ = s.stmtMarkA2aSentGetRow.QueryRowContext(ctx, id).Scan(
		&targetAgentID, &sourceSessID, &sourceSessKey, &createdAt)

	if _, err := s.stmtMarkA2aSent.ExecContext(ctx, ts, id); err != nil {
		return fmt.Errorf("mark a2a sent: %w", err)
	}

	// Bind the peer session so reply routing works.
	if ta := fromNullableText(targetAgentID); ta != "" {
		updatedAt := createdAt
		if updatedAt == 0 {
			updatedAt = ts
		}
		s.BindPeerSession(ctx, ta,
			fromNullableText(sourceSessID),
			fromNullableText(sourceSessKey),
			"a2a-send", updatedAt)
	}
	return nil
}

// MarkA2aOutboxRetry marks the row as RETRY with exponential backoff.
func (s *EventStore) MarkA2aOutboxRetry(ctx context.Context, id int64, attempt int, nextRetryAt int64, lastError string) error {
	ts := nowMs()
	_, err := s.stmtMarkA2aRetry.ExecContext(ctx,
		attempt, nextRetryAt, ts, truncate(lastError, 1000), id)
	return err
}
