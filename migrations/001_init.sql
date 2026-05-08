-- Relayly schema: initial migration
-- Using SQLite. All IDs are UUIDs stored as TEXT.

CREATE TABLE IF NOT EXISTS devices (
    id          TEXT PRIMARY KEY,
    name        TEXT    NOT NULL,
    -- Noise static public key (hex-encoded, 32 bytes = 64 hex chars)
    public_key  TEXT    NOT NULL DEFAULT '',
    -- One-time pairing token (base58, 32 bytes of entropy)
    pair_token  TEXT    UNIQUE NOT NULL,
    -- NULL until paired; references another device
    paired_with TEXT    REFERENCES devices(id) ON DELETE SET NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_devices_pair_token  ON devices(pair_token);
CREATE INDEX IF NOT EXISTS idx_devices_paired_with ON devices(paired_with);

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);
