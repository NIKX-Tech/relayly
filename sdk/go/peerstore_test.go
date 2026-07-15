package relayly

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestPeerStore_FirstPinPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	ps, err := LoadPeerStore(path)
	if err != nil {
		t.Fatalf("LoadPeerStore: %v", err)
	}

	if err := ps.PinOrVerify("device-b", "key-b-base64"); err != nil {
		t.Fatalf("PinOrVerify: %v", err)
	}

	got, ok := ps.Get("device-b")
	if !ok || got != "key-b-base64" {
		t.Errorf("Get: want key-b-base64, got %q (ok=%v)", got, ok)
	}

	// Reload from disk to confirm it was actually persisted.
	reloaded, err := LoadPeerStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok = reloaded.Get("device-b")
	if !ok || got != "key-b-base64" {
		t.Errorf("Get after reload: want key-b-base64, got %q (ok=%v)", got, ok)
	}
}

func TestPeerStore_MatchingReannounceIsNoOp(t *testing.T) {
	ps, err := LoadPeerStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatalf("LoadPeerStore: %v", err)
	}
	if err := ps.PinOrVerify("device-b", "key-b"); err != nil {
		t.Fatalf("first pin: %v", err)
	}
	if err := ps.PinOrVerify("device-b", "key-b"); err != nil {
		t.Errorf("re-announcing the same key should not error, got %v", err)
	}
}

func TestPeerStore_MismatchRejectedAndOriginalKept(t *testing.T) {
	ps, err := LoadPeerStore(filepath.Join(t.TempDir(), "peers.json"))
	if err != nil {
		t.Fatalf("LoadPeerStore: %v", err)
	}
	if err := ps.PinOrVerify("device-b", "key-b"); err != nil {
		t.Fatalf("first pin: %v", err)
	}
	if err := ps.PinOrVerify("device-b", "key-b-different"); !errors.Is(err, ErrPeerKeyMismatch) {
		t.Errorf("want ErrPeerKeyMismatch, got %v", err)
	}

	got, ok := ps.Get("device-b")
	if !ok || got != "key-b" {
		t.Errorf("original pin should survive a rejected mismatch, got %q (ok=%v)", got, ok)
	}
}

func TestPeerStore_DefaultPath(t *testing.T) {
	var opts Options
	if got := opts.peerStorePath(); got != DefaultPeerStorePath {
		t.Errorf("want default %q, got %q", DefaultPeerStorePath, got)
	}

	opts.PeerStorePath = "/custom/path/peers.json"
	if got := opts.peerStorePath(); got != "/custom/path/peers.json" {
		t.Errorf("want override to win, got %q", got)
	}
}

func TestLoadPeerStore_MissingFileIsEmpty(t *testing.T) {
	ps, err := LoadPeerStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("LoadPeerStore on missing file should not error, got %v", err)
	}
	if _, ok := ps.Get("anyone"); ok {
		t.Error("fresh store should have no pins")
	}
}
