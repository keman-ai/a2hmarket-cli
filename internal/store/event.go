package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ─── SQL strings used by db.go's prepareAll ──────────────────────────────────

const sqlInsertEvent = `
INSERT INTO message_event(
	event_id, peer_id, message_id, msg_ts, hash, unread_count, preview, payload_json,
	state, source, a2a_message_id, target_session_id, target_session_key, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

const sqlGetEvent = `
SELECT seq, event_id, peer_id, message_id, msg_ts, unread_count, preview,
       payload_json, state, source, a2a_message_id, target_session_id, target_session_key,
       created_at, updated_at
FROM message_event WHERE event_id = ?`

const sqlPullEvents = `
SELECT e.seq, e.event_id, e.peer_id, e.message_id, e.msg_ts, e.unread_count,
       e.preview, e.payload_json, e.state, e.created_at, e.updated_at
FROM message_event e
LEFT JOIN consumer_ack a ON a.event_id = e.event_id AND a.consumer_id = ?
WHERE e.seq > ? AND a.event_id IS NULL
ORDER BY e.seq ASC
LIMIT ?`

const sqlPullEventsWithPeer = `
SELECT e.seq, e.event_id, e.peer_id, e.message_id, e.msg_ts, e.unread_count,
       e.preview, e.payload_json, e.state, e.created_at, e.updated_at
FROM message_event e
LEFT JOIN consumer_ack a ON a.event_id = e.event_id AND a.consumer_id = ?
WHERE e.seq > ? AND a.event_id IS NULL AND e.peer_id = ?
ORDER BY e.seq ASC
LIMIT ?`

const sqlGetLatestEvents = `
SELECT seq, event_id, peer_id, unread_count, preview, state, created_at
FROM message_event
ORDER BY seq DESC
LIMIT ?`

// ─── Types ───────────────────────────────────────────────────────────────────

// InsertResult is returned by InsertIncomingEvent.
type InsertResult struct {
	Created bool
	EventID string
	Reason  string // "event_exists" | "push_outbox_conflict" | ""
}

// Event represents a single row from message_event.
type Event struct {
	Seq              int64
	EventID          string
	PeerID           string
	MessageID        string // empty if NULL
	MsgTs            int64
	Hash             string
	UnreadCount      int
	Preview          string
	State            string
	Source           string
	A2AMessageID     string // empty if NULL
	TargetSessionID  string
	TargetSessionKey string
	CreatedAt        int64
	UpdatedAt        int64
	Payload          map[string]interface{}
}

// PullEvent is a lighter Event struct returned by PullEvents.
type PullEvent struct {
	Seq         int64
	EventID     string
	PeerID      string
	MessageID   string
	MsgTs       int64
	UnreadCount int
	Preview     string
	State       string
	CreatedAt   int64
	UpdatedAt   int64
	Payload     map[string]interface{}
}

// ─── Methods ─────────────────────────────────────────────────────────────────

// InsertIncomingEvent inserts a new incoming A2A message atomically.
// It also inserts a push_outbox row if input.PushEnabled is true.
// Returns InsertResult with Created=false if the event already exists (dedup).
func (s *EventStore) InsertIncomingEvent(ctx context.Context, input InsertEventInput) (InsertResult, error) {
	ts := nowMs()
	if input.CreatedAt == 0 {
		input.CreatedAt = ts
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InsertResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	payloadJSON, err := json.Marshal(input.Payload)
	if err != nil {
		payloadJSON = []byte("{}")
	}

	_, err = tx.StmtContext(ctx, s.stmtInsertEvent).ExecContext(ctx,
		input.EventID,
		input.PeerID,
		nullableText(input.MessageID),
		input.MsgTs,
		input.Hash,
		input.UnreadCount,
		input.Preview,
		string(payloadJSON),
		input.State,
		input.Source,
		nullableText(input.A2AMessageID),
		nullableText(input.TargetSessionID),
		nullableText(input.TargetSessionKey),
		input.CreatedAt,
		ts,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return InsertResult{Created: false, EventID: input.EventID, Reason: "event_exists"}, nil
		}
		return InsertResult{}, fmt.Errorf("insert event: %w", err)
	}

	if input.PushEnabled {
		_, err = tx.StmtContext(ctx, s.stmtEnqueuePushOutbox).ExecContext(ctx,
			input.EventID,
			input.PushTarget,
			ts, // next_retry_at
			ts, // created_at
			ts, // updated_at
		)
		if err != nil {
			if isUniqueConstraint(err) {
				return InsertResult{Created: false, EventID: input.EventID, Reason: "push_outbox_conflict"}, nil
			}
			return InsertResult{}, fmt.Errorf("insert push_outbox: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return InsertResult{}, fmt.Errorf("commit: %w", err)
	}
	return InsertResult{Created: true, EventID: input.EventID}, nil
}

// InsertEventInput is the parameter struct for InsertIncomingEvent.
type InsertEventInput struct {
	EventID          string
	PeerID           string
	MessageID        string
	MsgTs            int64
	Hash             string
	UnreadCount      int
	Preview          string
	Payload          interface{} // marshalled to JSON
	State            string
	Source           string
	A2AMessageID     string
	TargetSessionID  string
	TargetSessionKey string
	CreatedAt        int64
	PushEnabled      bool
	PushTarget       string // default "openclaw"
}

// GetEvent retrieves a single event by its event_id. Returns nil if not found.
func (s *EventStore) GetEvent(ctx context.Context, eventID string) (*Event, error) {
	row := s.stmtGetEvent.QueryRowContext(ctx, eventID)
	return scanEvent(row)
}

func scanEvent(row *sql.Row) (*Event, error) {
	var e Event
	var messageID, a2aMsgID, targetSessID, targetSessKey sql.NullString
	var payloadJSON string
	err := row.Scan(
		&e.Seq, &e.EventID, &e.PeerID, &messageID, &e.MsgTs, &e.UnreadCount,
		&e.Preview, &payloadJSON, &e.State, &e.Source,
		&a2aMsgID, &targetSessID, &targetSessKey, &e.CreatedAt, &e.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.MessageID = fromNullableText(messageID)
	e.A2AMessageID = fromNullableText(a2aMsgID)
	e.TargetSessionID = fromNullableText(targetSessID)
	e.TargetSessionKey = fromNullableText(targetSessKey)

	if err := json.Unmarshal([]byte(payloadJSON), &e.Payload); err != nil {
		e.Payload = map[string]interface{}{"raw": payloadJSON}
	}
	return &e, nil
}

// PullEventsOpts holds optional filters for PullEvents.
type PullEventsOpts struct {
	PeerID string // if non-empty, only return events from this peer
}

// PullEvents returns events with seq > cursor that have not been acked by consumerId.
func (s *EventStore) PullEvents(ctx context.Context, consumerID string, cursor int64, limit int, opts ...PullEventsOpts) ([]PullEvent, error) {
	if limit <= 0 {
		limit = 20
	}

	var peerID string
	if len(opts) > 0 {
		peerID = opts[0].PeerID
	}

	var rows *sql.Rows
	var err error
	if peerID == "" {
		rows, err = s.stmtPullEvents.QueryContext(ctx, consumerID, cursor, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, sqlPullEventsWithPeer, consumerID, cursor, peerID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []PullEvent
	for rows.Next() {
		var e PullEvent
		var messageID sql.NullString
		var payloadJSON string
		if err := rows.Scan(
			&e.Seq, &e.EventID, &e.PeerID, &messageID, &e.MsgTs, &e.UnreadCount,
			&e.Preview, &payloadJSON, &e.State, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		e.MessageID = fromNullableText(messageID)
		if err := json.Unmarshal([]byte(payloadJSON), &e.Payload); err != nil {
			e.Payload = map[string]interface{}{"raw": payloadJSON}
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// LatestEvent is a summary row returned by GetLatestEvents.
type LatestEvent struct {
	Seq         int64
	EventID     string
	PeerID      string
	UnreadCount int
	Preview     string
	State       string
	CreatedAt   int64
}

// GetLatestEvents returns the most recent limit events (descending by seq).
func (s *EventStore) GetLatestEvents(ctx context.Context, limit int) ([]LatestEvent, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.stmtGetLatestEvents.QueryContext(ctx, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LatestEvent
	for rows.Next() {
		var r LatestEvent
		if err := rows.Scan(&r.Seq, &r.EventID, &r.PeerID, &r.UnreadCount, &r.Preview, &r.State, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateEventTargetSession sets target_session_id and target_session_key on an event.
func (s *EventStore) UpdateEventTargetSession(ctx context.Context, eventID, sessionID, sessionKey string) (bool, error) {
	ts := nowMs()
	res, err := s.stmtUpdateEventSession.ExecContext(ctx,
		nullableText(sessionID),
		nullableText(sessionKey),
		ts,
		eventID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

const sqlUpdateEventSession = `
UPDATE message_event
SET target_session_id=?, target_session_key=?, updated_at=?
WHERE event_id=?`
