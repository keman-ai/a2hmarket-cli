package store

import (
	"context"
	"database/sql"
)

// ─── SQL strings ─────────────────────────────────────────────────────────────

const sqlBindPeerGetExisting = `
SELECT session_id, session_key, updated_at
FROM peer_session_route WHERE peer_id=? LIMIT 1`

const sqlBindPeerUpsert = `
INSERT INTO peer_session_route(peer_id, session_id, session_key, source, updated_at)
VALUES(?,?,?,?,?)
ON CONFLICT(peer_id) DO UPDATE SET
  session_id=excluded.session_id,
  session_key=excluded.session_key,
  source=excluded.source,
  updated_at=excluded.updated_at`

const sqlGetPeerByEventID = `
SELECT peer_id FROM message_event WHERE event_id=? LIMIT 1`

const sqlFindRouteByTrace = `
SELECT source_session_id, source_session_key
FROM a2a_outbox
WHERE target_agent_id=? AND trace_id=? AND status='SENT'
  AND (source_session_id IS NOT NULL OR source_session_key IS NOT NULL)
ORDER BY created_at DESC, id DESC
LIMIT 1`

const sqlFindRoutePeerBinding = `
SELECT session_id, session_key
FROM peer_session_route
WHERE peer_id=? AND (session_id IS NOT NULL OR session_key IS NOT NULL)
LIMIT 1`

const sqlFindRouteByPeer = `
SELECT source_session_id, source_session_key
FROM a2a_outbox
WHERE target_agent_id=? AND status='SENT'
  AND (source_session_id IS NOT NULL OR source_session_key IS NOT NULL)
ORDER BY created_at DESC, id DESC
LIMIT 1`

// ─── Types ───────────────────────────────────────────────────────────────────

// RouteResult is returned by FindA2aReplyRoute.
type RouteResult struct {
	SessionID  string
	SessionKey string
	MatchedBy  string // "trace" | "peer-binding" | "peer-latest"
}

// ─── Methods ─────────────────────────────────────────────────────────────────

// BindPeerSession records or updates the session routing for peerId.
// If the existing binding has a newer updated_at, the update is skipped (stale binding).
func (s *EventStore) BindPeerSession(ctx context.Context, peerID, sessionID, sessionKey, source string, updatedAt int64) (updated bool, reason string) {
	if peerID == "" {
		return false, "missing_peer_id"
	}
	if sessionID == "" && sessionKey == "" {
		return false, "missing_session_ref"
	}

	var existSessID, existSessKey sql.NullString
	var existUpdatedAt int64
	err := s.stmtBindPeerGetExisting.QueryRowContext(ctx, peerID).Scan(&existSessID, &existSessKey, &existUpdatedAt)
	if err != nil && err != sql.ErrNoRows {
		return false, "db_error"
	}
	if err == nil && existUpdatedAt > updatedAt {
		return false, "stale_binding"
	}

	// Merge: prefer sessionKey-bearing entry; fill gaps from existing.
	nextSessKey := sessionKey
	if nextSessKey == "" {
		nextSessKey = fromNullableText(existSessKey)
	}
	nextSessID := sessionID
	if nextSessKey != "" {
		// When we have a key, clear ambiguous id unless explicitly set
		if sessionID == "" {
			nextSessID = fromNullableText(existSessID)
		}
	} else {
		if nextSessID == "" {
			nextSessID = fromNullableText(existSessID)
		}
	}

	if _, err := s.stmtBindPeerUpsert.ExecContext(ctx,
		peerID,
		nullableText(nextSessID),
		nullableText(nextSessKey),
		source,
		updatedAt,
	); err != nil {
		return false, "db_error"
	}
	return true, ""
}

// bindPeerSessionForEvent binds a session via the peer_id found in message_event.
// This is the internal helper used by AckEvent.
func (s *EventStore) bindPeerSessionForEvent(ctx context.Context, eventID, sessionID, sessionKey, source string, updatedAt int64) (bound bool, reason string) {
	if eventID == "" {
		return false, "missing_event_id"
	}
	var peerID string
	err := s.stmtGetPeerByEventID.QueryRowContext(ctx, eventID).Scan(&peerID)
	if err == sql.ErrNoRows || peerID == "" {
		return false, "event_not_found"
	}
	if err != nil {
		return false, "db_error"
	}
	return s.BindPeerSession(ctx, peerID, sessionID, sessionKey, source, updatedAt)
}

// BindPeerSessionForEvent is the public version for external callers (e.g. inbox ack).
func (s *EventStore) BindPeerSessionForEvent(ctx context.Context, eventID, sessionID, sessionKey, source string, updatedAt int64) (bound bool, reason string) {
	return s.bindPeerSessionForEvent(ctx, eventID, sessionID, sessionKey, source, updatedAt)
}

// FindA2aReplyRoute looks up the best reply session for a given peer / trace.
// Priority: trace → peer binding table → latest outbox entry for peer.
func (s *EventStore) FindA2aReplyRoute(ctx context.Context, peerID, traceID string) (*RouteResult, error) {
	if peerID == "" {
		return nil, nil
	}

	// 1. By trace_id
	if traceID != "" {
		var sessID, sessKey sql.NullString
		err := s.stmtFindRouteByTrace.QueryRowContext(ctx, peerID, traceID).Scan(&sessID, &sessKey)
		if err == nil {
			return &RouteResult{
				SessionID:  fromNullableText(sessID),
				SessionKey: fromNullableText(sessKey),
				MatchedBy:  "trace",
			}, nil
		}
	}

	// 2. Peer binding table
	{
		var sessID, sessKey sql.NullString
		err := s.stmtFindRoutePeerBinding.QueryRowContext(ctx, peerID).Scan(&sessID, &sessKey)
		if err == nil {
			return &RouteResult{
				SessionID:  fromNullableText(sessID),
				SessionKey: fromNullableText(sessKey),
				MatchedBy:  "peer-binding",
			}, nil
		}
	}

	// 3. Latest outbox entry for this peer
	{
		var sessID, sessKey sql.NullString
		err := s.stmtFindRouteByPeer.QueryRowContext(ctx, peerID).Scan(&sessID, &sessKey)
		if err != nil {
			return nil, nil
		}
		return &RouteResult{
			SessionID:  fromNullableText(sessID),
			SessionKey: fromNullableText(sessKey),
			MatchedBy:  "peer-latest",
		}, nil
	}
}
