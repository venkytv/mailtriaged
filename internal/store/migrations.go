package store

import (
	"database/sql"
	"fmt"
)

var migrations = []string{
	// Migration 1: initial schema
	`CREATE TABLE messages (
		id INTEGER PRIMARY KEY,
		account TEXT NOT NULL,
		folder TEXT NOT NULL,
		imap_uid INTEGER NOT NULL,
		uid_validity INTEGER NOT NULL DEFAULT 0,
		message_id TEXT,
		from_email TEXT,
		from_domain TEXT,
		subject TEXT,
		received_at TEXT,
		seen_at TEXT NOT NULL,
		UNIQUE(account, folder, imap_uid, uid_validity)
	);

	CREATE TABLE decisions (
		id INTEGER PRIMARY KEY,
		message_id INTEGER NOT NULL,
		action TEXT NOT NULL,
		source TEXT NOT NULL,
		rule_id TEXT,
		reason TEXT,
		created_at TEXT NOT NULL,
		FOREIGN KEY(message_id) REFERENCES messages(id)
	);

	CREATE TABLE summary_items (
		id INTEGER PRIMARY KEY,
		message_id INTEGER NOT NULL,
		summary TEXT NOT NULL,
		sent INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		sent_at TEXT,
		FOREIGN KEY(message_id) REFERENCES messages(id)
	);

	CREATE TABLE rule_hits (
		id INTEGER PRIMARY KEY,
		rule_id TEXT NOT NULL,
		message_id INTEGER NOT NULL,
		action TEXT NOT NULL,
		hit_at TEXT NOT NULL,
		FOREIGN KEY(message_id) REFERENCES messages(id)
	);

	CREATE TABLE classifier_calls (
		id INTEGER PRIMARY KEY,
		message_id INTEGER NOT NULL,
		command TEXT NOT NULL,
		request_json TEXT NOT NULL,
		response_json TEXT,
		exit_code INTEGER,
		stderr TEXT,
		duration_ms INTEGER,
		created_at TEXT NOT NULL,
		FOREIGN KEY(message_id) REFERENCES messages(id)
	);

	CREATE TABLE notifications (
		id INTEGER PRIMARY KEY,
		message_id INTEGER NOT NULL,
		channel TEXT NOT NULL,
		status TEXT NOT NULL,
		error TEXT,
		created_at TEXT NOT NULL,
		sent_at TEXT,
		FOREIGN KEY(message_id) REFERENCES messages(id)
	);`,

	// Migration 2: classifier metadata for model tracking
	`CREATE TABLE classifier_metadata (
		id INTEGER PRIMARY KEY,
		classifier_call_id INTEGER NOT NULL,
		model TEXT,
		confidence REAL,
		escalated INTEGER NOT NULL DEFAULT 0,
		raw_json TEXT,
		created_at TEXT NOT NULL,
		FOREIGN KEY(classifier_call_id) REFERENCES classifier_calls(id)
	);`,
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	var current int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	for i := current; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration %d: %w", i+1, err)
		}

		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("applying migration %d: %w", i+1, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", i+1); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %d: %w", i+1, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", i+1, err)
		}
	}

	return nil
}
