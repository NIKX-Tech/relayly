// Package database — device pairing CRUD operations.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Device represents a registered device in the database.
type Device struct {
	ID          string
	Name        string
	PublicKey   string // legacy: the old client<->server Noise static key, unused since Protocol v1
	DeviceToken string
	StaticKey   string  // announced E2E identity key (docs/PROTOCOL.md §7.2), empty until announce_key
	PairedWith  *string // nil if not yet paired
	CreatedAt   time.Time
	LastSeen    *time.Time
	ExpiresAt   *time.Time // nil means no expiry (existing rows)
}

// PairedWithShort returns the first 8 characters of the paired device ID, or empty string.
func (d Device) PairedWithShort() string {
	if d.PairedWith == nil || len(*d.PairedWith) < 8 {
		if d.PairedWith == nil {
			return ""
		}
		return *d.PairedWith
	}
	return (*d.PairedWith)[:8]
}

// ErrNotFound is returned when a device lookup yields no result.
var ErrNotFound = errors.New("device not found")

// ErrAlreadyPaired is returned when trying to pair an already-paired device.
var ErrAlreadyPaired = errors.New("device already paired")

// ErrStaticKeyMismatch is returned by SetStaticKeyIfUnset when a device announces a
// static key that differs from the one already locked in (docs/PROTOCOL.md §7.2).
var ErrStaticKeyMismatch = errors.New("static key mismatch")

const deviceColumns = `id, name, public_key, device_token, static_key, paired_with, created_at, last_seen, expires_at`

// CreateDevice inserts a new device record.
func (db *DB) CreateDevice(d *Device) error {
	_, err := db.Exec(`
		INSERT INTO devices (id, name, public_key, device_token, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.PublicKey, d.DeviceToken, d.CreatedAt, d.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("CreateDevice: %w", err)
	}
	return nil
}

// GetDeviceByDeviceToken retrieves a device by its device token.
func (db *DB) GetDeviceByDeviceToken(token string) (*Device, error) {
	return db.scanDevice(
		db.QueryRow(`SELECT `+deviceColumns+` FROM devices WHERE device_token = ?`, token),
	)
}

// GetDeviceByID retrieves a device by its UUID.
func (db *DB) GetDeviceByID(id string) (*Device, error) {
	return db.scanDevice(
		db.QueryRow(`SELECT `+deviceColumns+` FROM devices WHERE id = ?`, id),
	)
}

// ListDevices returns all registered devices ordered by creation time.
func (db *DB) ListDevices() ([]*Device, error) {
	rows, err := db.Query(`SELECT ` + deviceColumns + ` FROM devices ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("ListDevices: %w", err)
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		d, err := db.scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// PairDevices links deviceA and deviceB as a symmetric pair, and clears both devices'
// device_token expiry (see deviceTokenTTL in internal/pairing/pairing.go): the TTL exists
// to garbage-collect devices registered but never actually used, not to force a
// currently-paired device to periodically re-register (which would mint a new device_id
// and silently orphan the peer's existing pin, see docs/PROTOCOL.md §7.1). A successful
// pairing is itself the "this device is real, not registration spam" signal the TTL is
// protecting against. Both must be unpaired. The operation is atomic.
func (db *DB) PairDevices(idA, idB string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Verify neither is already paired
	for _, id := range []string{idA, idB} {
		var paired sql.NullString
		row := tx.QueryRow(`SELECT paired_with FROM devices WHERE id = ?`, id)
		if err := row.Scan(&paired); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if paired.Valid {
			return ErrAlreadyPaired
		}
	}

	// Link both directions and clear device_token expiry now that pairing has
	// proven this is a real, intentionally-used device (see doc comment above).
	for thisID, peerID := range map[string]string{idA: idB, idB: idA} {
		if _, err := tx.Exec(
			`UPDATE devices SET paired_with = ?, expires_at = NULL WHERE id = ?`, peerID, thisID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpdatePublicKey sets the legacy Noise public key for a device. Kept only so migration
// v3 has something to backfill static_key from; nothing writes to it going forward.
func (db *DB) UpdatePublicKey(id, pubKey string) error {
	_, err := db.Exec(`UPDATE devices SET public_key = ? WHERE id = ?`, pubKey, id)
	return err
}

// SetStaticKeyIfUnset implements docs/PROTOCOL.md §7.2's server-side key locking: the
// first announce_key for a device is persisted; a later announcement with a different
// key is rejected with ErrStaticKeyMismatch instead of being applied.
func (db *DB) SetStaticKeyIfUnset(id, staticKey string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existing sql.NullString
	row := tx.QueryRow(`SELECT static_key FROM devices WHERE id = ?`, id)
	if err := row.Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if existing.Valid && existing.String != "" {
		if existing.String != staticKey {
			return ErrStaticKeyMismatch
		}
		return tx.Commit() // already locked to this exact key, nothing to do
	}

	if _, err := tx.Exec(`UPDATE devices SET static_key = ? WHERE id = ?`, staticKey, id); err != nil {
		return err
	}
	return tx.Commit()
}

// TouchLastSeen updates the last_seen timestamp for a device.
func (db *DB) TouchLastSeen(id string) error {
	_, err := db.Exec(`UPDATE devices SET last_seen = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

// DeleteDevice removes a device and its pairing links.
func (db *DB) DeleteDevice(id string) error {
	// Clear paired_with references pointing to this device
	if _, err := db.Exec(`UPDATE devices SET paired_with = NULL WHERE paired_with = ?`, id); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM devices WHERE id = ?`, id)
	return err
}

// scanDevice is a helper that reads a device from any *sql.Row or *sql.Rows.
func (db *DB) scanDevice(scanner interface {
	Scan(...any) error
}) (*Device, error) {
	d := &Device{}
	var staticKey sql.NullString
	var paired sql.NullString
	var lastSeen sql.NullTime
	var expiresAt sql.NullTime

	err := scanner.Scan(
		&d.ID, &d.Name, &d.PublicKey, &d.DeviceToken, &staticKey,
		&paired, &d.CreatedAt, &lastSeen, &expiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scanning device: %w", err)
	}

	if staticKey.Valid {
		d.StaticKey = staticKey.String
	}
	if paired.Valid {
		d.PairedWith = &paired.String
	}
	if lastSeen.Valid {
		d.LastSeen = &lastSeen.Time
	}
	if expiresAt.Valid {
		d.ExpiresAt = &expiresAt.Time
	}

	return d, nil
}
