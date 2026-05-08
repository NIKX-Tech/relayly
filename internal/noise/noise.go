// Package noise wraps the flynn/noise library to provide Noise Protocol XX
// handshake helpers for Relayly's server-side authentication layer.
//
// Protocol: Noise_XX_25519_ChaChaPoly_BLAKE2s
//
// The XX pattern allows mutual authentication — both the client and the server
// prove ownership of their static keypairs. After a successful handshake, the
// resulting Noise transport state is held by the client to encrypt payloads;
// the relay server only validates the handshake and then forwards opaque frames.
package noise

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	flnoise "github.com/flynn/noise"
)

// CipherSuite is the Noise cipher suite used throughout Relayly.
var CipherSuite = flnoise.NewCipherSuite(
	flnoise.DH25519,
	flnoise.CipherChaChaPoly,
	flnoise.HashBLAKE2s,
)

// Keypair holds a Noise static keypair.
type Keypair struct {
	flnoise.DHKey
}

// PublicKeyHex returns the public key as a hex string.
func (kp *Keypair) PublicKeyHex() string {
	return hex.EncodeToString(kp.Public)
}

// GenerateKeypair creates a fresh Noise static keypair.
func GenerateKeypair() (*Keypair, error) {
	key, err := CipherSuite.GenerateKeypair(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating noise keypair: %w", err)
	}
	return &Keypair{key}, nil
}

// SaveKeypair writes the keypair to path in binary format:
// 32 bytes private key || 32 bytes public key.
func SaveKeypair(kp *Keypair, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}
	data := make([]byte, 64)
	copy(data[:32], kp.Private)
	copy(data[32:], kp.Public)
	return os.WriteFile(path, data, 0o600)
}

// LoadKeypair reads a keypair from path (as written by SaveKeypair).
func LoadKeypair(path string) (*Keypair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading keypair: %w", err)
	}
	if len(data) != 64 {
		return nil, fmt.Errorf("invalid keypair file: expected 64 bytes, got %d", len(data))
	}
	return &Keypair{flnoise.DHKey{
		Private: data[:32],
		Public:  data[32:],
	}}, nil
}

// LoadOrCreateKeypair loads the keypair from path, generating one if absent.
func LoadOrCreateKeypair(path string) (*Keypair, bool, error) {
	kp, err := LoadKeypair(path)
	if err == nil {
		return kp, false, nil // loaded existing
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	// Generate and persist
	kp, err = GenerateKeypair()
	if err != nil {
		return nil, false, err
	}
	return kp, true, SaveKeypair(kp, path)
}

// NewServerHandshake creates a Noise XX HandshakeState for the server role.
// The caller must perform the three-message XX exchange:
//
//	msg1 = client → server  (read by server)
//	msg2 = server → client  (written by server)
//	msg3 = client → server  (read by server)
//
// After msg3 the handshake is complete and both parties have:
//   - Authenticated each other's static keys
//   - Derived symmetric send/recv transport keys
func NewServerHandshake(serverKey *Keypair) (*flnoise.HandshakeState, error) {
	config := flnoise.Config{
		CipherSuite:   CipherSuite,
		Random:        rand.Reader,
		Pattern:       flnoise.HandshakeXX,
		Initiator:     false, // server is the responder
		StaticKeypair: serverKey.DHKey,
	}
	return flnoise.NewHandshakeState(config)
}

// NewClientHandshake creates a Noise XX HandshakeState for the client role.
// clientKey is the client's static keypair; serverPub (optional) is the
// server's known public key for pinning.
func NewClientHandshake(clientKey *Keypair, serverPub []byte) (*flnoise.HandshakeState, error) {
	config := flnoise.Config{
		CipherSuite:   CipherSuite,
		Random:        rand.Reader,
		Pattern:       flnoise.HandshakeXX,
		Initiator:     true,
		StaticKeypair: clientKey.DHKey,
	}
	if len(serverPub) > 0 {
		config.PeerStatic = serverPub
	}
	return flnoise.NewHandshakeState(config)
}
