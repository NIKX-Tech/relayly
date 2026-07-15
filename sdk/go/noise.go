package relayly

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	flnoise "github.com/flynn/noise"
)

// noiseCipherSuite is Noise_XX_25519_ChaChaPoly_BLAKE2s (docs/PROTOCOL.md §6) — this
// must match the relay server and every other SDK byte for byte, or handshakes will
// simply fail to complete.
var noiseCipherSuite = flnoise.NewCipherSuite(flnoise.DH25519, flnoise.CipherChaChaPoly, flnoise.HashBLAKE2s)

// E2E envelope types (docs/PROTOCOL.md §6): binary WebSocket frames are one byte of
// envelope type followed by the Noise message or transport ciphertext.
const (
	envelopeHandshake byte = 0x01
	envelopeTransport byte = 0x02
)

// encodeEnvelope prefixes payload with its 1-byte envelope type.
func encodeEnvelope(kind byte, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = kind
	copy(out[1:], payload)
	return out
}

// decodeEnvelope splits a binary WebSocket frame into its envelope type and payload.
// ok is false for an empty frame.
func decodeEnvelope(frame []byte) (kind byte, payload []byte, ok bool) {
	if len(frame) < 1 {
		return 0, nil, false
	}
	return frame[0], frame[1:], true
}

// toDHKey adapts our on-disk PrivateKey representation to flynn/noise's DHKey.
func toDHKey(pk PrivateKey) flnoise.DHKey {
	pub, _ := pk.PublicKey() // scalarBaseMult never errors
	priv := make([]byte, 32)
	copy(priv, pk.raw[:])
	pubBytes := make([]byte, 32)
	copy(pubBytes, pub.raw[:])
	return flnoise.DHKey{Private: priv, Public: pubBytes}
}

type sessionStatus int

const (
	statusHandshaking sessionStatus = iota
	statusReady
	statusFailed
)

// noiseSession drives exactly one Noise XX handshake (docs/PROTOCOL.md §6), as either
// role, and once it completes, encrypts/decrypts transport messages for that session.
// It does not itself implement the make-before-break replacement policy — see peer.go
// for the wrapper that decides when a new noiseSession may replace an existing one.
type noiseSession struct {
	mu     sync.Mutex
	status sessionStatus
	err    error // set when status == statusFailed

	initiator bool
	hs        *flnoise.HandshakeState // non-nil while handshaking
	gotMsg1   bool                    // responder only: have we processed msg1 yet?

	sendCS *flnoise.CipherState // non-nil once ready
	recvCS *flnoise.CipherState

	peerStatic []byte // authenticated peer static key, set once ready

	readyCh chan struct{} // closed exactly once, whenever status leaves statusHandshaking
}

// newInitiatorSession starts a handshake as the Noise initiator and returns the first
// message (msg1) to send as an envelopeHandshake frame.
func newInitiatorSession(priv PrivateKey) (*noiseSession, []byte, error) {
	hs, err := flnoise.NewHandshakeState(flnoise.Config{
		CipherSuite:   noiseCipherSuite,
		Random:        rand.Reader,
		Pattern:       flnoise.HandshakeXX,
		Initiator:     true,
		StaticKeypair: toDHKey(priv),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("relayly: starting noise handshake: %w", err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("relayly: writing noise msg1: %w", err)
	}
	return &noiseSession{initiator: true, status: statusHandshaking, hs: hs, readyCh: make(chan struct{})}, msg1, nil
}

// newResponderSession starts a handshake as the Noise responder, ready to receive msg1.
func newResponderSession(priv PrivateKey) (*noiseSession, error) {
	hs, err := flnoise.NewHandshakeState(flnoise.Config{
		CipherSuite:   noiseCipherSuite,
		Random:        rand.Reader,
		Pattern:       flnoise.HandshakeXX,
		Initiator:     false,
		StaticKeypair: toDHKey(priv),
	})
	if err != nil {
		return nil, fmt.Errorf("relayly: starting noise handshake: %w", err)
	}
	return &noiseSession{initiator: false, status: statusHandshaking, hs: hs, readyCh: make(chan struct{})}, nil
}

// handleHandshakeMessage feeds one received envelopeHandshake payload into the state
// machine. reply is non-nil when a response message must be sent back. done is true
// once the handshake has completed (successfully or not); check err in that case.
func (s *noiseSession) handleHandshakeMessage(data []byte) (reply []byte, done bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status != statusHandshaking {
		return nil, true, errors.New("relayly: handshake message received after handshake finished")
	}

	if s.initiator {
		// The only message an initiator ever receives is msg2; writing msg3
		// completes the handshake for the initiator (flynn/noise convention).
		if _, _, _, err = s.hs.ReadMessage(nil, data); err != nil {
			s.failLocked(fmt.Errorf("relayly: reading noise msg2: %w", err))
			return nil, true, s.err
		}
		var cs1, cs2 *flnoise.CipherState
		reply, cs1, cs2, err = s.hs.WriteMessage(nil, nil)
		if err != nil {
			s.failLocked(fmt.Errorf("relayly: writing noise msg3: %w", err))
			return nil, true, s.err
		}
		// cs1 = initiator->responder (our send), cs2 = responder->initiator (our recv).
		s.finishLocked(cs1, cs2)
		return reply, true, nil
	}

	// Responder.
	if !s.gotMsg1 {
		if _, _, _, err = s.hs.ReadMessage(nil, data); err != nil {
			s.failLocked(fmt.Errorf("relayly: reading noise msg1: %w", err))
			return nil, true, s.err
		}
		s.gotMsg1 = true
		reply, _, _, err = s.hs.WriteMessage(nil, nil)
		if err != nil {
			s.failLocked(fmt.Errorf("relayly: writing noise msg2: %w", err))
			return nil, true, s.err
		}
		return reply, false, nil
	}

	var cs1, cs2 *flnoise.CipherState
	_, cs1, cs2, err = s.hs.ReadMessage(nil, data)
	if err != nil {
		s.failLocked(fmt.Errorf("relayly: reading noise msg3: %w", err))
		return nil, true, s.err
	}
	// Responder sends with cs2, receives/decrypts with cs1.
	s.finishLocked(cs2, cs1)
	return nil, true, nil
}

func (s *noiseSession) finishLocked(sendCS, recvCS *flnoise.CipherState) {
	s.sendCS = sendCS
	s.recvCS = recvCS
	s.peerStatic = s.hs.PeerStatic()
	s.status = statusReady
	close(s.readyCh)
}

func (s *noiseSession) failLocked(err error) {
	if s.status == statusFailed {
		return
	}
	s.status = statusFailed
	s.err = err
	close(s.readyCh)
}

// ready reports whether the handshake has completed successfully.
func (s *noiseSession) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status == statusReady
}

// waitReady blocks until the handshake finishes (successfully or not) or ctx is done.
func (s *noiseSession) waitReady(ctx context.Context) error {
	select {
	case <-s.readyCh:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// encrypt returns ciphertext for plaintext using the session's send cipher state.
func (s *noiseSession) encrypt(plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != statusReady {
		return nil, ErrNotReady
	}
	return s.sendCS.Encrypt(nil, nil, plaintext)
}

// decrypt returns plaintext for ciphertext using the session's receive cipher state.
func (s *noiseSession) decrypt(ciphertext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != statusReady {
		return nil, ErrNotReady
	}
	return s.recvCS.Decrypt(nil, nil, ciphertext)
}
