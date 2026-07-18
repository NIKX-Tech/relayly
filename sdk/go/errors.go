package relayly

import "errors"

// Errors surfaced by the relay's control-channel error codes (docs/PROTOCOL.md §5.1).
var (
	ErrInvalidCode   = errors.New("relayly: invalid pairing code")
	ErrCodeExpired   = errors.New("relayly: pairing code expired")
	ErrAlreadyPaired = errors.New("relayly: device already paired")
	ErrPeerOffline   = errors.New("relayly: peer is offline")
	ErrRateLimited   = errors.New("relayly: rate limited")
	ErrMalformed     = errors.New("relayly: malformed request")
	ErrInternal      = errors.New("relayly: internal server error")

	// ErrKeyMismatch is the server-side announced-key lock (§7.2) rejecting a
	// different key than the one already recorded for this device.
	ErrKeyMismatch = errors.New("relayly: announced static key does not match the server's record")
)

// Errors local to the SDK, not server error codes.
var (
	// ErrNotReady is returned by Send when the peer's Noise session isn't up yet —
	// notably during the brief window after a reconnect forces a re-handshake (§6).
	// RequestPairCode/AcceptPair/PairCode.Wait never return until the very first
	// handshake completes, so this should not occur on a freshly paired peer.
	ErrNotReady = errors.New("relayly: peer session is not ready")

	// ErrPeerKeyMismatch is the client-side pin check (§7.1, the real security
	// boundary) rejecting a peer whose authenticated static key differs from the one
	// already pinned for that peer ID. Unpinning is an explicit user action only;
	// this error is never auto-retried.
	ErrPeerKeyMismatch = errors.New("relayly: peer's authenticated key does not match the pinned key")
)

// errForCode maps a control-channel error's machine code (docs/PROTOCOL.md §5.1) to a
// typed sentinel, wrapped with the human-readable message from the server.
func errForCode(code, message string) error {
	var base error
	switch code {
	case "invalid_code":
		base = ErrInvalidCode
	case "code_expired":
		base = ErrCodeExpired
	case "already_paired":
		base = ErrAlreadyPaired
	case "peer_offline":
		base = ErrPeerOffline
	case "rate_limited":
		base = ErrRateLimited
	case "malformed":
		base = ErrMalformed
	case "key_mismatch":
		base = ErrKeyMismatch
	default:
		base = ErrInternal
	}
	if message == "" {
		return base
	}
	return &wireError{code: base, message: message}
}

// wireError wraps a typed sentinel with the server's human-readable message, so
// errors.Is(err, ErrInvalidCode) still works while %v/Error() shows the real detail.
type wireError struct {
	code    error
	message string
}

func (e *wireError) Error() string { return e.code.Error() + ": " + e.message }
func (e *wireError) Unwrap() error { return e.code }
