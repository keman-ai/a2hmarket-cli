package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ─── SQL strings ─────────────────────────────────────────────────────────────

const sqlAckInsert = `
INSERT OR IGNORE INTO consumer_ack(consumer_id, event_id, acked_at) VALUES(?,?,?)`

const sqlAckUpdateEvent = `
UPDATE message_event SET state='CONSUMED', updated_at=? WHERE event_id=?`

const sqlGetEventForAck = `
SELECT msg_ts, created_at FROM message_event WHERE event_id=? LIMIT 1`

const sqlGetAck = `
SELECT acked_at FROM consumer_ack WHERE consumer_id=? AND event_id=? LIMIT 1`

const sqlIsEventAcked = `
SELECT 1 FROM consumer_ack WHERE consumer_id=? AND event_id=? LIMIT 1`

const sqlPeekUnread = `
SELECT COUNT(1) FROM message_event e
LEFT JOIN consumer_ack a ON a.event_id=e.event_id AND a.consumer_id=?
WHERE a.event_id IS NULL`

const sqlPeekPendingPush = `
SELECT COUNT(1) FROM push_outbox WHERE status IN ('PENDING','RETRY','SENT')`

const sqlCheckUnread = `
SELECT COUNT(1), MIN(e.created_at) FROM message_event e
LEFT JOIN consumer_ack a ON a.event_id=e.event_id AND a.consumer_id=?
WHERE a.event_id IS NULL`

const sqlCheckPendingPush = `
SELECT COUNT(1) FROM push_outbox WHERE status IN ('PENDING','RETRY','SENT')`

// ─── Types ───────────────────────────────────────────────────────────────────

// RouteBinding is an optional session binding to record on first ACK.
type RouteBinding struct {
	SessionID  string
	SessionKey string
	Source     string
}

// AckResult is returned by AckEvent.
type AckResult struct {
	AckedAt        int64
	Inserted       bool   // true = first ACK for this (consumer, event) pair
	RouteBound     bool
	RouteBindReason string
}

// PeekResult holds the unread and pending push counts.
type PeekResult struct {
	Unread      int
	PendingPush int
}

// CheckResult is the full health status returned by CheckStatus.
type CheckResult struct {
	Unread           int
	OldestUnreadAt   int64 // 0 if no unread events
	PendingPush      int
}

// ─── Methods ─────────────────────────────────────────────────────────────────

// AckEvent records that consumerId has consumed eventId.
// If routeBinding is non-nil and this is the first ACK, the peer session is bound.
func (s *EventStore) AckEvent(ctx context.Context, consumerID, eventID string, routeBinding *RouteBinding) (AckResult, error) {
	ts := nowMs()

	// Verify event exists.
	var msgTs, createdAt int64
	err := s.stmtGetEventForAck.QueryRowContext(ctx, eventID).Scan(&msgTs, &createdAt)
	if err == sql.ErrNoRows {
		return AckResult{}, fmt.Errorf("cannot ack non-existent event: %s", eventID)
	}
	if err != nil {
		return AckResult{}, fmt.Errorf("get event for ack: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AckResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.StmtContext(ctx, s.stmtAckInsert).ExecContext(ctx, consumerID, eventID, ts)
	if err != nil {
		return AckResult{}, fmt.Errorf("insert ack: %w", err)
	}
	inserted, _ := res.RowsAffected()

	if _, err := tx.StmtContext(ctx, s.stmtAckUpdateEvent).ExecContext(ctx, ts, eventID); err != nil {
		return AckResult{}, fmt.Errorf("update event state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return AckResult{}, fmt.Errorf("commit: %w", err)
	}

	firstAck := inserted > 0

	// Read back the acked_at (either just-inserted or pre-existing).
	var ackedAt int64
	if firstAck {
		ackedAt = ts
	} else {
		if err := s.stmtGetAck.QueryRowContext(ctx, consumerID, eventID).Scan(&ackedAt); err != nil {
			ackedAt = ts
		}
	}

	result := AckResult{
		AckedAt:  ackedAt,
		Inserted: firstAck,
	}

	// Bind peer→session on first ACK only.
	if firstAck && routeBinding != nil && (routeBinding.SessionID != "" || routeBinding.SessionKey != "") {
		updatedAt := msgTs
		if updatedAt == 0 {
			updatedAt = createdAt
		}
		bound, reason := s.bindPeerSessionForEvent(ctx, eventID, routeBinding.SessionID, routeBinding.SessionKey, routeBinding.Source, updatedAt)
		result.RouteBound = bound
		result.RouteBindReason = reason
	} else if !firstAck {
		result.RouteBindReason = "already_acked"
	} else {
		result.RouteBindReason = "missing_session_ref"
	}

	return result, nil
}

// IsEventAckedByConsumer reports whether the given consumer has already acked eventID.
func (s *EventStore) IsEventAckedByConsumer(ctx context.Context, consumerID, eventID string) (bool, error) {
	var dummy int
	err := s.stmtIsEventAcked.QueryRowContext(ctx, consumerID, eventID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// PeekUnread returns the count of unread events and pending push rows for consumerID.
func (s *EventStore) PeekUnread(ctx context.Context, consumerID string) (PeekResult, error) {
	var r PeekResult
	if err := s.stmtPeekUnread.QueryRowContext(ctx, consumerID).Scan(&r.Unread); err != nil {
		return r, err
	}
	if err := s.stmtPeekPendingPush.QueryRowContext(ctx).Scan(&r.PendingPush); err != nil {
		return r, err
	}
	return r, nil
}

// CheckStatus returns fuller status including oldest unread timestamp.
func (s *EventStore) CheckStatus(ctx context.Context, consumerID string) (CheckResult, error) {
	var r CheckResult
	var oldest sql.NullInt64
	if err := s.stmtCheckUnread.QueryRowContext(ctx, consumerID).Scan(&r.Unread, &oldest); err != nil {
		return r, err
	}
	if oldest.Valid {
		r.OldestUnreadAt = oldest.Int64
	}
	if err := s.stmtCheckPendingPush.QueryRowContext(ctx).Scan(&r.PendingPush); err != nil {
		return r, err
	}
	return r, nil
}
