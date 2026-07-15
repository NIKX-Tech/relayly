package relayly

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultPeerStorePath is where a PeerStore lives when Options.PeerStorePath is unset.
const DefaultPeerStorePath = "~/.relayly/peers.json"

// pinnedPeer is one entry in the peer store's on-disk JSON. The schema is shared
// byte-for-byte across every official SDK (docs/tasks/02-sdks-and-interop.md), so the
// same file can in principle be read/written by clients written in different
// languages on the same machine.
type pinnedPeer struct {
	StaticKey string `json:"static_key"`
	PinnedAt  string `json:"pinned_at"` // RFC 3339
}

// PeerStore persists pinned peer static keys (docs/PROTOCOL.md §7.1): the client-side
// pin is the real security boundary. A peer's key is pinned the first time its Noise
// handshake completes; any later handshake presenting a different key for the same
// peer ID hard-fails with ErrPeerKeyMismatch. Unpinning is never automatic.
type PeerStore struct {
	mu    sync.Mutex
	path  string
	peers map[string]pinnedPeer // peer device ID -> pin
}

// LoadPeerStore loads the peer store at path, creating an empty one in memory if the
// file doesn't exist yet (it is created on first successful pin).
func LoadPeerStore(path string) (*PeerStore, error) {
	path = expandHome(path)
	ps := &PeerStore{path: path, peers: make(map[string]pinnedPeer)}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ps, nil
		}
		return nil, fmt.Errorf("relayly: reading peer store %s: %w", path, err)
	}
	if len(data) == 0 {
		return ps, nil
	}
	if err := json.Unmarshal(data, &ps.peers); err != nil {
		return nil, fmt.Errorf("relayly: invalid peer store %s: %w", path, err)
	}
	return ps, nil
}

// PinOrVerify implements §7.1: if peerID has no recorded pin yet, staticKey (base64)
// is pinned and persisted. If a pin already exists and matches, this is a no-op. If a
// pin already exists and differs, it returns ErrPeerKeyMismatch and leaves the
// original pin in place.
func (ps *PeerStore) PinOrVerify(peerID, staticKeyB64 string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if existing, ok := ps.peers[peerID]; ok {
		if existing.StaticKey != staticKeyB64 {
			return ErrPeerKeyMismatch
		}
		return nil
	}

	ps.peers[peerID] = pinnedPeer{StaticKey: staticKeyB64, PinnedAt: time.Now().UTC().Format(time.RFC3339)}
	return ps.saveLocked()
}

// Get returns the pinned static key (base64) for peerID, if any.
func (ps *PeerStore) Get(peerID string) (staticKeyB64 string, ok bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.peers[peerID]
	return p.StaticKey, ok
}

// saveLocked writes the store to disk atomically (write to a temp file, then rename).
// Caller must hold ps.mu.
func (ps *PeerStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(ps.path), 0700); err != nil {
		return fmt.Errorf("relayly: creating peer store directory: %w", err)
	}
	data, err := json.MarshalIndent(ps.peers, "", "  ")
	if err != nil {
		return fmt.Errorf("relayly: encoding peer store: %w", err)
	}

	tmp := ps.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("relayly: writing peer store: %w", err)
	}
	if err := os.Rename(tmp, ps.path); err != nil {
		return fmt.Errorf("relayly: saving peer store: %w", err)
	}
	return nil
}
