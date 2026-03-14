package store

import (
	"context"
	"database/sql"
	"fmt"
)

// applySchema creates all tables, indexes, and applies column-level migrations
// in an idempotent way (mirrors JS store/schema.js applySchema).
func applySchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		// ── message_event ────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS message_event (
			seq              INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id         TEXT    NOT NULL UNIQUE,
			peer_id          TEXT    NOT NULL,
			message_id       TEXT,
			msg_ts           INTEGER NOT NULL DEFAULT 0,
			hash             TEXT    NOT NULL,
			unread_count     INTEGER NOT NULL DEFAULT 0,
			preview          TEXT    NOT NULL DEFAULT '',
			payload_json     TEXT    NOT NULL DEFAULT '{}',
			state            TEXT    NOT NULL DEFAULT 'NEW',
			source           TEXT    NOT NULL DEFAULT 'MQTT',
			a2a_message_id   TEXT,
			target_session_id  TEXT,
			target_session_key TEXT,
			created_at       INTEGER NOT NULL,
			updated_at       INTEGER NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_message_event_message_id
			ON message_event(message_id) WHERE message_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_message_event_peer_hash
			ON message_event(peer_id, hash)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_message_event_a2a_message_id
			ON message_event(a2a_message_id) WHERE a2a_message_id IS NOT NULL`,

		// ── push_outbox ──────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS push_outbox (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id      TEXT    NOT NULL UNIQUE,
			target        TEXT    NOT NULL DEFAULT 'openclaw',
			attempt       INTEGER NOT NULL DEFAULT 0,
			next_retry_at INTEGER NOT NULL,
			status        TEXT    NOT NULL DEFAULT 'PENDING',
			last_error    TEXT,
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_push_outbox_status_retry
			ON push_outbox(status, next_retry_at)`,

		// ── a2a_outbox ───────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS a2a_outbox (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id         TEXT    NOT NULL UNIQUE,
			trace_id           TEXT,
			target_agent_id    TEXT    NOT NULL DEFAULT '',
			message_type       TEXT    NOT NULL DEFAULT '',
			qos                INTEGER NOT NULL DEFAULT 1,
			envelope_json      TEXT    NOT NULL DEFAULT '{}',
			source_session_id  TEXT,
			source_session_key TEXT,
			status             TEXT    NOT NULL DEFAULT 'PENDING',
			attempt            INTEGER NOT NULL DEFAULT 0,
			next_retry_at      INTEGER NOT NULL,
			last_error         TEXT,
			created_at         INTEGER NOT NULL,
			updated_at         INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_outbox_status_retry
			ON a2a_outbox(status, next_retry_at)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_outbox_target_trace
			ON a2a_outbox(target_agent_id, trace_id, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_outbox_target_updated
			ON a2a_outbox(target_agent_id, updated_at)`,

		// ── peer_session_route ───────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS peer_session_route (
			peer_id     TEXT    NOT NULL PRIMARY KEY,
			session_id  TEXT,
			session_key TEXT,
			source      TEXT    NOT NULL DEFAULT 'manual',
			updated_at  INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_peer_session_route_updated
			ON peer_session_route(updated_at)`,

		// ── consumer_ack ─────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS consumer_ack (
			consumer_id TEXT    NOT NULL,
			event_id    TEXT    NOT NULL,
			acked_at    INTEGER NOT NULL,
			PRIMARY KEY (consumer_id, event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_consumer_ack_event_id
			ON consumer_ack(event_id)`,

		// ── media_outbox ─────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS media_outbox (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id      TEXT    NOT NULL UNIQUE,
			session_key   TEXT,
			channel       TEXT    NOT NULL DEFAULT '',
			to_target     TEXT    NOT NULL DEFAULT '',
			account_id    TEXT,
			thread_id     TEXT,
			message_text  TEXT    NOT NULL DEFAULT '',
			media_url     TEXT,
			status        TEXT    NOT NULL DEFAULT 'PENDING',
			attempt       INTEGER NOT NULL DEFAULT 0,
			next_retry_at INTEGER NOT NULL,
			last_error    TEXT,
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_media_outbox_status_retry
			ON media_outbox(status, next_retry_at)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec schema: %w (sql: %.60s)", err, stmt)
		}
	}

	// Column-level migrations (idempotent — add if missing).
	type colMigration struct {
		table  string
		column string
		alter  string
	}
	migrations := []colMigration{
		{"message_event", "source", "ALTER TABLE message_event ADD COLUMN source TEXT NOT NULL DEFAULT 'MQTT'"},
		{"message_event", "a2a_message_id", "ALTER TABLE message_event ADD COLUMN a2a_message_id TEXT"},
		{"message_event", "target_session_id", "ALTER TABLE message_event ADD COLUMN target_session_id TEXT"},
		{"message_event", "target_session_key", "ALTER TABLE message_event ADD COLUMN target_session_key TEXT"},
		{"a2a_outbox", "trace_id", "ALTER TABLE a2a_outbox ADD COLUMN trace_id TEXT"},
		{"a2a_outbox", "source_session_id", "ALTER TABLE a2a_outbox ADD COLUMN source_session_id TEXT"},
		{"a2a_outbox", "source_session_key", "ALTER TABLE a2a_outbox ADD COLUMN source_session_key TEXT"},
		{"media_outbox", "media_url", "ALTER TABLE media_outbox ADD COLUMN media_url TEXT"},
	}
	for _, m := range migrations {
		if !columnExists(ctx, db, m.table, m.column) {
			if _, err := db.ExecContext(ctx, m.alter); err != nil {
				return fmt.Errorf("migration add column %s.%s: %w", m.table, m.column, err)
			}
		}
	}

	return nil
}

// columnExists returns true if the named column exists in the named table.
func columnExists(ctx context.Context, db *sql.DB, table, column string) bool {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

// tableExists returns true if the named table exists.
func tableExists(ctx context.Context, db *sql.DB, table string) bool {
	var name string
	err := db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
	return err == nil
}
