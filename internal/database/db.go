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

// initSQL contains the initial schema SQL, read at Open() time.
// We read from the embedded string passed in (or bundled separately).
// To keep the package dependency-free from embed paths, the migration
// SQL is passed in explicitly by the caller or loaded from migrations/.
const initSQL = `
-- Relayly schema: initial migration
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

// migrate runs the embedded schema SQL idempotently (uses IF NOT EXISTS).
func (db *DB) migrate() error {
	_, err := db.Exec(initSQL)
	return err
}
