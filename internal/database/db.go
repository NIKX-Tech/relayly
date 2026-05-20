// Package database manages the SQLite connection and schema migrations.
// It uses modernc.org/sqlite — a pure-Go driver requiring no CGo.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register the sqlite3 driver
)

// initSQL contains the base schema (version 1).
const initSQL = `
CREATE TABLE IF NOT EXISTS devices (
    id          TEXT PRIMARY KEY,
    name        TEXT    NOT NULL,
    public_key  TEXT    NOT NULL DEFAULT '',
    pair_token  TEXT    UNIQUE NOT NULL,
    paired_with TEXT    REFERENCES devices(id) ON DELETE SET NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_devices_pair_token  ON devices(pair_token);
CREATE INDEX IF NOT EXISTS idx_devices_paired_with ON devices(paired_with);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);
`

// migration2SQL adds the expires_at column for pairing code TTL (version 2).
// The column is nullable; existing rows have no expiry (NULL).
const migration2SQL = `ALTER TABLE devices ADD COLUMN expires_at DATETIME;`

// DB wraps a *sql.DB with Relayly-specific helpers.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database at path and runs migrations.
func Open(path string) (*DB, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", path)
	rawDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	// SQLite performs best with a single writer connection
	rawDB.SetMaxOpenConns(1)

	if err := rawDB.Ping(); err != nil {
		return nil, fmt.Errorf("pinging sqlite: %w", err)
	}

	db := &DB{rawDB}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// migrate runs schema migrations idempotently.
func (db *DB) migrate() error {
	// Version 1: base schema
	if _, err := db.Exec(initSQL); err != nil {
		return fmt.Errorf("migration v1: %w", err)
	}

	// Version 2: add expires_at to devices.
	// We check schema_migrations first to make it idempotent.
	var applied int
	row := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 2`)
	if err := row.Scan(&applied); err != nil {
		return fmt.Errorf("checking migration v2: %w", err)
	}
	if applied == 0 {
		if _, err := db.Exec(migration2SQL); err != nil {
			return fmt.Errorf("migration v2 alter: %w", err)
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES (2)`); err != nil {
			return fmt.Errorf("migration v2 record: %w", err)
		}
	}

	return nil
}
