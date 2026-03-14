// Package store implements the SQLite-backed event store for the a2hmarket-cli listener daemon.
// It mirrors the JS runtime's store/event-store.js behaviour.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// nowMs returns the current Unix time in milliseconds.
func nowMs() int64 {
	return time.Now().UnixMilli()
}

// EventStore is the SQLite-backed persistent store for incoming messages and outboxes.
// All methods are safe for concurrent use from multiple goroutines.
type EventStore struct {
	db *sql.DB

	// Prepared statements — compiled once at Open() time.
	stmtInsertEvent          *sql.Stmt
	stmtGetEvent             *sql.Stmt
	stmtPullEvents           *sql.Stmt
	stmtGetLatestEvents      *sql.Stmt
	stmtAckInsert            *sql.Stmt
	stmtAckUpdateEvent       *sql.Stmt
	stmtGetEventForAck       *sql.Stmt
	stmtGetAck               *sql.Stmt
	stmtIsEventAcked         *sql.Stmt
	stmtPeekUnread           *sql.Stmt
	stmtPeekPendingPush      *sql.Stmt
	stmtCheckUnread          *sql.Stmt
	stmtCheckPendingPush     *sql.Stmt
	stmtEnqueueA2aOutbox     *sql.Stmt
	stmtListPendingA2a       *sql.Stmt
	stmtMarkA2aSent          *sql.Stmt
	stmtMarkA2aSentGetRow    *sql.Stmt
	stmtMarkA2aRetry         *sql.Stmt
	stmtEnqueuePushOutbox    *sql.Stmt
	stmtListPendingPush      *sql.Stmt
	stmtListSentPush         *sql.Stmt
	stmtMarkPushSent         *sql.Stmt
	stmtMarkPushSentEvent    *sql.Stmt
	stmtMarkPushAcked        *sql.Stmt
	stmtMarkPushAckedEvent   *sql.Stmt
	stmtMarkPushRetry        *sql.Stmt
	stmtEnqueueMedia         *sql.Stmt
	stmtListPendingMedia     *sql.Stmt
	stmtMarkMediaSent        *sql.Stmt
	stmtMarkMediaRetry       *sql.Stmt
	stmtMarkMediaFailed      *sql.Stmt
	stmtBindPeerGetExisting  *sql.Stmt
	stmtBindPeerUpsert       *sql.Stmt
	stmtGetPeerByEventID     *sql.Stmt
	stmtFindRouteByTrace     *sql.Stmt
	stmtFindRoutePeerBinding *sql.Stmt
	stmtFindRouteByPeer      *sql.Stmt
	stmtUpdateEventSession   *sql.Stmt
}

// Open creates (or opens) the SQLite database at path, applies PRAGMA settings,
// runs schema migrations, and pre-compiles all statements.
// Callers must call Close() when done.
func Open(path string) (*EventStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("store: mkdir %s: %w", filepath.Dir(path), err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	// Single-writer mode is fine for the listener; reduces lock contention.
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", p, err)
		}
	}

	if err := applySchema(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: schema: %w", err)
	}

	s := &EventStore{db: db}
	if err := s.prepareAll(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: prepare: %w", err)
	}
	return s, nil
}

// Close releases all prepared statements and closes the database connection.
func (s *EventStore) Close() error {
	stmts := []*sql.Stmt{
		s.stmtInsertEvent, s.stmtGetEvent, s.stmtPullEvents, s.stmtGetLatestEvents,
		s.stmtAckInsert, s.stmtAckUpdateEvent, s.stmtGetEventForAck, s.stmtGetAck,
		s.stmtIsEventAcked, s.stmtPeekUnread, s.stmtPeekPendingPush, s.stmtCheckUnread,
		s.stmtCheckPendingPush, s.stmtEnqueueA2aOutbox, s.stmtListPendingA2a,
		s.stmtMarkA2aSent, s.stmtMarkA2aSentGetRow, s.stmtMarkA2aRetry,
		s.stmtEnqueuePushOutbox, s.stmtListPendingPush, s.stmtListSentPush,
		s.stmtMarkPushSent, s.stmtMarkPushSentEvent, s.stmtMarkPushAcked,
		s.stmtMarkPushAckedEvent, s.stmtMarkPushRetry, s.stmtEnqueueMedia,
		s.stmtListPendingMedia, s.stmtMarkMediaSent, s.stmtMarkMediaRetry,
		s.stmtMarkMediaFailed, s.stmtBindPeerGetExisting, s.stmtBindPeerUpsert,
		s.stmtGetPeerByEventID, s.stmtFindRouteByTrace, s.stmtFindRoutePeerBinding,
		s.stmtFindRouteByPeer, s.stmtUpdateEventSession,
	}
	for _, st := range stmts {
		if st != nil {
			st.Close()
		}
	}
	return s.db.Close()
}

// prepareAll pre-compiles every SQL statement used by the store.
func (s *EventStore) prepareAll(ctx context.Context) error {
	type nameSQL struct {
		dest **sql.Stmt
		sql  string
	}
	defs := []nameSQL{
		{&s.stmtInsertEvent, sqlInsertEvent},
		{&s.stmtGetEvent, sqlGetEvent},
		{&s.stmtPullEvents, sqlPullEvents},
		{&s.stmtGetLatestEvents, sqlGetLatestEvents},
		{&s.stmtAckInsert, sqlAckInsert},
		{&s.stmtAckUpdateEvent, sqlAckUpdateEvent},
		{&s.stmtGetEventForAck, sqlGetEventForAck},
		{&s.stmtGetAck, sqlGetAck},
		{&s.stmtIsEventAcked, sqlIsEventAcked},
		{&s.stmtPeekUnread, sqlPeekUnread},
		{&s.stmtPeekPendingPush, sqlPeekPendingPush},
		{&s.stmtCheckUnread, sqlCheckUnread},
		{&s.stmtCheckPendingPush, sqlCheckPendingPush},
		{&s.stmtEnqueueA2aOutbox, sqlEnqueueA2aOutbox},
		{&s.stmtListPendingA2a, sqlListPendingA2a},
		{&s.stmtMarkA2aSent, sqlMarkA2aSent},
		{&s.stmtMarkA2aSentGetRow, sqlMarkA2aSentGetRow},
		{&s.stmtMarkA2aRetry, sqlMarkA2aRetry},
		{&s.stmtEnqueuePushOutbox, sqlEnqueuePushOutbox},
		{&s.stmtListPendingPush, sqlListPendingPush},
		{&s.stmtListSentPush, sqlListSentPush},
		{&s.stmtMarkPushSent, sqlMarkPushSent},
		{&s.stmtMarkPushSentEvent, sqlMarkPushSentEvent},
		{&s.stmtMarkPushAcked, sqlMarkPushAcked},
		{&s.stmtMarkPushAckedEvent, sqlMarkPushAckedEvent},
		{&s.stmtMarkPushRetry, sqlMarkPushRetry},
		{&s.stmtEnqueueMedia, sqlEnqueueMedia},
		{&s.stmtListPendingMedia, sqlListPendingMedia},
		{&s.stmtMarkMediaSent, sqlMarkMediaSent},
		{&s.stmtMarkMediaRetry, sqlMarkMediaRetry},
		{&s.stmtMarkMediaFailed, sqlMarkMediaFailed},
		{&s.stmtBindPeerGetExisting, sqlBindPeerGetExisting},
		{&s.stmtBindPeerUpsert, sqlBindPeerUpsert},
		{&s.stmtGetPeerByEventID, sqlGetPeerByEventID},
		{&s.stmtFindRouteByTrace, sqlFindRouteByTrace},
		{&s.stmtFindRoutePeerBinding, sqlFindRoutePeerBinding},
		{&s.stmtFindRouteByPeer, sqlFindRouteByPeer},
		{&s.stmtUpdateEventSession, sqlUpdateEventSession},
	}
	for _, d := range defs {
		stmt, err := s.db.PrepareContext(ctx, d.sql)
		if err != nil {
			return fmt.Errorf("prepare %q: %w", d.sql[:min(40, len(d.sql))], err)
		}
		*d.dest = stmt
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// nullableText converts a string to sql.NullString, treating empty as NULL.
func nullableText(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

// fromNullableText extracts the string from a sql.NullString, returning "" for NULL.
func fromNullableText(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

// isUniqueConstraint reports whether err is a SQLite UNIQUE constraint violation.
// modernc.org/sqlite wraps the error as "constraint failed: UNIQUE constraint failed: ..."
func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "UNIQUE constraint failed")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSub(s, sub))
}

func findSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// truncate limits s to max bytes.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
