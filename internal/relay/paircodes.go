package relay

import (
	"sync"
	"time"
)

// pairCodeEntry is a pending in-band pairing code (docs/PROTOCOL.md §5.3): a 6-digit
// code generated for a requesting device, waiting for a pair_accept from its peer.
type pairCodeEntry struct {
	requesterID string
	expiresAt   time.Time
}

// pairCodeRegistry tracks pairing codes currently awaiting a pair_accept. Codes are
// single-use (removed by the first Take, successful or not) and expire after their
// TTL. Expired entries are reaped lazily on the next Take rather than by a background
// sweeper: the expected volume, one entry per in-flight pairing attempt, makes a
// sweeper unnecessary for v1.
type pairCodeRegistry struct {
	mu      sync.Mutex
	entries map[string]pairCodeEntry
}

func newPairCodeRegistry() *pairCodeRegistry {
	return &pairCodeRegistry{entries: make(map[string]pairCodeEntry)}
}

// Put stores a freshly generated code for requesterID, valid for ttl.
func (r *pairCodeRegistry) Put(code, requesterID string, ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[code] = pairCodeEntry{requesterID: requesterID, expiresAt: time.Now().Add(ttl)}
}

// Take looks up code and, if present, removes it (single-use, regardless of outcome).
// ok is true only for a present, unexpired code. expired distinguishes "never existed"
// from "existed but expired," so the caller can return invalid_code vs. code_expired.
func (r *pairCodeRegistry) Take(code string) (requesterID string, expired bool, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, found := r.entries[code]
	if !found {
		return "", false, false
	}
	delete(r.entries, code)

	if time.Now().After(entry.expiresAt) {
		return "", true, false
	}
	return entry.requesterID, false, true
}
