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

// migrationsTableSQL creates the tracking table itself. This is the one statement that
// is safe to run unconditionally on every Open(): it never changes shape, so it can't
// collide with anything a later migration does. Every other migration, including
// version 1, is gated on schema_migrations so it runs exactly once ever, not once per
// process start (see migration3SQL's history for why that distinction matters: it
// renames a column that an ungated "version 1" used to unconditionally reference).
const migrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// migration1SQL is the base schema (version 1).
const migration1SQL = `
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
`

// migration2SQL adds the expires_at column for pairing code TTL (version 2).
// The column is nullable; existing rows have no expiry (NULL).
const migration2SQL = `ALTER TABLE devices ADD COLUMN expires_at DATETIME;`

// migration3SQL implements Protocol v1's rename and new key-locking column (version 3):
// pair_token becomes device_token (same UNIQUE NOT NULL constraint carries over), and
// static_key holds each device's announced E2E identity key (docs/PROTOCOL.md §7.2).
// Any row that already had a public_key (from the old client<->server Noise handshake)
// gets it copied into static_key, so already-registered devices keep an announced key.
const migration3SQL = `
ALTER TABLE devices RENAME COLUMN pair_token TO device_token;
ALTER TABLE devices ADD COLUMN static_key TEXT;
UPDATE devices SET static_key = public_key WHERE public_key IS NOT NULL AND public_key != '';
DROP INDEX IF EXISTS idx_devices_pair_token;
CREATE INDEX IF NOT EXISTS idx_devices_device_token ON devices(device_token);
`

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

// migrate runs schema migrations idempotently, and each one exactly once ever.
func (db *DB) migrate() error {
	if _, err := db.Exec(migrationsTableSQL); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{1, migration1SQL},
		{2, migration2SQL},
		{3, migration3SQL},
	}

	for _, m := range migrations {
		var applied int
		row := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version)
		if err := row.Scan(&applied); err != nil {
			return fmt.Errorf("checking migration v%d: %w", m.version, err)
		}
		if applied != 0 {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("migration v%d: %w", m.version, err)
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
			return fmt.Errorf("recording migration v%d: %w", m.version, err)
		}
	}

	return nil
}
