// Package database_test contains integration tests for the SQLite layer.
// Each test uses an in-memory database so no disk I/O or cleanup is needed.
package database_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/NIKX-Tech/relayly/internal/database"
)

// openMem opens a fresh in-memory SQLite database for testing.
func openMem(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("openMem: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// fixture returns a minimal valid Device ready for insertion.
func fixture(id, name, token string) *database.Device {
	return &database.Device{
		ID:          id,
		Name:        name,
		DeviceToken: token,
		CreatedAt:   time.Now().UTC(),
	}
}

// ── CreateDevice ──────────────────────────────────────────────────────────────

func TestCreateDevice_Success(t *testing.T) {
	db := openMem(t)
	d := fixture("id-1", "laptop", "token-abc")
	if err := db.CreateDevice(d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateDevice_DuplicateID(t *testing.T) {
	db := openMem(t)
	d := fixture("id-dup", "phone", "tok-1")
	if err := db.CreateDevice(d); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Same ID → should fail (PRIMARY KEY constraint)
	d2 := fixture("id-dup", "other", "tok-2")
	if err := db.CreateDevice(d2); err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
}

func TestCreateDevice_DuplicateToken(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("id-a", "a", "same-token")); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := db.CreateDevice(fixture("id-b", "b", "same-token")); err == nil {
		t.Fatal("expected UNIQUE constraint error for token, got nil")
	}
}

// ── GetDeviceByID ─────────────────────────────────────────────────────────────

func TestGetDeviceByID_Found(t *testing.T) {
	db := openMem(t)
	d := fixture("abc-123", "desktop", "tok-xyz")
	if err := db.CreateDevice(d); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := db.GetDeviceByID("abc-123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "desktop" {
		t.Errorf("Name: want 'desktop', got %q", got.Name)
	}
	if got.DeviceToken != "tok-xyz" {
		t.Errorf("DeviceToken: want 'tok-xyz', got %q", got.DeviceToken)
	}
	if got.PairedWith != nil {
		t.Errorf("PairedWith should be nil for unpaired device")
	}
}

func TestGetDeviceByID_NotFound(t *testing.T) {
	db := openMem(t)
	_, err := db.GetDeviceByID("does-not-exist")
	if !errors.Is(err, database.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ── GetDeviceByDeviceToken ────────────────────────────────────────────────────

func TestGetDeviceByDeviceToken_Found(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("id-t", "tablet", "unique-token-99")); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := db.GetDeviceByDeviceToken("unique-token-99")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "id-t" {
		t.Errorf("ID: want 'id-t', got %q", got.ID)
	}
}

func TestGetDeviceByDeviceToken_NotFound(t *testing.T) {
	db := openMem(t)
	_, err := db.GetDeviceByDeviceToken("ghost-token")
	if !errors.Is(err, database.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ── ListDevices ───────────────────────────────────────────────────────────────

func TestListDevices_Empty(t *testing.T) {
	db := openMem(t)
	devices, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("want 0 devices, got %d", len(devices))
	}
}

func TestListDevices_Multiple(t *testing.T) {
	db := openMem(t)
	for i, name := range []string{"a", "b", "c"} {
		id := "id-" + name
		tok := "tok-" + name
		d := fixture(id, name, tok)
		// stagger created_at so order is deterministic
		d.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := db.CreateDevice(d); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	devices, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("want 3, got %d", len(devices))
	}
	// ORDER BY created_at
	if devices[0].Name != "a" || devices[2].Name != "c" {
		t.Errorf("unexpected order: %v", []string{devices[0].Name, devices[1].Name, devices[2].Name})
	}
}

// ── PairDevices ───────────────────────────────────────────────────────────────

func TestPairDevices_Success(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("dev-A", "A", "tok-A")); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := db.CreateDevice(fixture("dev-B", "B", "tok-B")); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := db.PairDevices("dev-A", "dev-B"); err != nil {
		t.Fatalf("pair: %v", err)
	}

	// Verify symmetric link
	a, _ := db.GetDeviceByID("dev-A")
	b, _ := db.GetDeviceByID("dev-B")

	if a.PairedWith == nil || *a.PairedWith != "dev-B" {
		t.Errorf("A.PairedWith: want 'dev-B', got %v", a.PairedWith)
	}
	if b.PairedWith == nil || *b.PairedWith != "dev-A" {
		t.Errorf("B.PairedWith: want 'dev-A', got %v", b.PairedWith)
	}
}

func TestPairDevices_AlreadyPaired(t *testing.T) {
	db := openMem(t)
	for _, d := range []*database.Device{
		fixture("p1", "p1", "t1"),
		fixture("p2", "p2", "t2"),
		fixture("p3", "p3", "t3"),
	} {
		if err := db.CreateDevice(d); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if err := db.PairDevices("p1", "p2"); err != nil {
		t.Fatalf("first pair: %v", err)
	}
	// p1 is already paired — should fail
	if err := db.PairDevices("p1", "p3"); !errors.Is(err, database.ErrAlreadyPaired) {
		t.Errorf("want ErrAlreadyPaired, got %v", err)
	}
}

func TestPairDevices_NotFound(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("real", "real", "tok-real")); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := db.PairDevices("real", "ghost")
	if !errors.Is(err, database.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ── UpdatePublicKey ───────────────────────────────────────────────────────────

func TestUpdatePublicKey(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("kdev", "k", "ktok")); err != nil {
		t.Fatalf("create: %v", err)
	}
	const pubKey = "deadbeefdeadbeef"
	if err := db.UpdatePublicKey("kdev", pubKey); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := db.GetDeviceByID("kdev")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PublicKey != pubKey {
		t.Errorf("PublicKey: want %q, got %q", pubKey, got.PublicKey)
	}
}

// ── SetStaticKeyIfUnset ───────────────────────────────────────────────────────

func TestSetStaticKeyIfUnset_FirstAnnouncementPersists(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("sk1", "s", "sktok")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.SetStaticKeyIfUnset("sk1", "key-a"); err != nil {
		t.Fatalf("first announce: %v", err)
	}
	got, err := db.GetDeviceByID("sk1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.StaticKey != "key-a" {
		t.Errorf("StaticKey: want %q, got %q", "key-a", got.StaticKey)
	}
}

func TestSetStaticKeyIfUnset_SameKeyIsIdempotent(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("sk2", "s", "sktok2")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.SetStaticKeyIfUnset("sk2", "key-a"); err != nil {
		t.Fatalf("first announce: %v", err)
	}
	if err := db.SetStaticKeyIfUnset("sk2", "key-a"); err != nil {
		t.Errorf("re-announcing the same key should not error, got %v", err)
	}
}

func TestSetStaticKeyIfUnset_MismatchRejected(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("sk3", "s", "sktok3")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.SetStaticKeyIfUnset("sk3", "key-a"); err != nil {
		t.Fatalf("first announce: %v", err)
	}
	if err := db.SetStaticKeyIfUnset("sk3", "key-b"); !errors.Is(err, database.ErrStaticKeyMismatch) {
		t.Errorf("want ErrStaticKeyMismatch, got %v", err)
	}
	// The original key must still be in place after the rejected attempt.
	got, err := db.GetDeviceByID("sk3")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.StaticKey != "key-a" {
		t.Errorf("StaticKey should remain %q after mismatch, got %q", "key-a", got.StaticKey)
	}
}

func TestSetStaticKeyIfUnset_NotFound(t *testing.T) {
	db := openMem(t)
	err := db.SetStaticKeyIfUnset("ghost", "key-a")
	if !errors.Is(err, database.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ── migrations on reopen ──────────────────────────────────────────────────────

// TestMigrate_ReopenAlreadyMigratedFile is a regression test: migration v1 used to
// run its CREATE INDEX unconditionally on every Open(), including against a database
// that migration v3 had already renamed pair_token away from, on the next process
// start (e.g. after a restart) that failed with "no such column: pair_token". Every
// migration must now run exactly once ever, on a real file reopened more than once.
func TestMigrate_ReopenAlreadyMigratedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relayly.db")

	db1, err := database.Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := db1.CreateDevice(fixture("dev-1", "one", "tok-1")); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopening the same, already-migrated file (simulating a server restart) must
	// not re-run any migration's SQL.
	db2, err := database.Open(path)
	if err != nil {
		t.Fatalf("second open (reopen after migrations already ran): %v", err)
	}
	defer db2.Close()

	got, err := db2.GetDeviceByID("dev-1")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got.DeviceToken != "tok-1" {
		t.Errorf("DeviceToken survived reopen: want tok-1, got %q", got.DeviceToken)
	}

	// A third open, for good measure — nothing about this should be a one-shot fix.
	db3, err := database.Open(path)
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	defer db3.Close()
}

func TestNewDevice_StaticKeyStartsEmpty(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("mig1", "m", "migtok")); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := db.GetDeviceByID("mig1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.StaticKey != "" {
		t.Errorf("fresh device should have empty StaticKey until announce_key, got %q", got.StaticKey)
	}
}

// ── TouchLastSeen ─────────────────────────────────────────────────────────────

func TestTouchLastSeen(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("ldev", "l", "ltok")); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := db.TouchLastSeen("ldev"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, err := db.GetDeviceByID("ldev")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastSeen == nil {
		t.Fatal("LastSeen should not be nil after touch")
	}
	if got.LastSeen.Before(before) {
		t.Errorf("LastSeen %v is before expected floor %v", got.LastSeen, before)
	}
}

// ── DeleteDevice ──────────────────────────────────────────────────────────────

func TestDeleteDevice_Unpaired(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("del1", "d1", "dtok1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.DeleteDevice("del1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := db.GetDeviceByID("del1")
	if !errors.Is(err, database.ErrNotFound) {
		t.Errorf("want ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteDevice_ClearsPairedWith(t *testing.T) {
	db := openMem(t)
	if err := db.CreateDevice(fixture("da", "a", "ta")); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := db.CreateDevice(fixture("db", "b", "tb")); err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := db.PairDevices("da", "db"); err != nil {
		t.Fatalf("pair: %v", err)
	}

	// Delete device A — B's PairedWith should become nil
	if err := db.DeleteDevice("da"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	b, err := db.GetDeviceByID("db")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if b.PairedWith != nil {
		t.Errorf("B.PairedWith should be nil after A deleted, got %v", *b.PairedWith)
	}
}
