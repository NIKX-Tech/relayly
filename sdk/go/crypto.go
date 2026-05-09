package relayly

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/nacl/box"
)

// PrivateKey is an X25519 private key used for NaCl box encryption.
type PrivateKey struct {
	raw [32]byte
}

// PublicKey is an X25519 public key.
type PublicKey struct {
	raw [32]byte
}

// GenerateKey generates a new random X25519 keypair and returns the private key.
// The corresponding public key can be derived via PrivateKey.PublicKey().
//
//	key, err := relayly.GenerateKey()
func GenerateKey() (PrivateKey, error) {
	_, privRaw, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return PrivateKey{}, fmt.Errorf("relayly: failed to generate keypair: %w", err)
	}
	return PrivateKey{raw: *privRaw}, nil
}

// PublicKey derives the X25519 public key from this private key using
// Curve25519 scalar base multiplication.
func (pk PrivateKey) PublicKey() (PublicKey, error) {
	var pub [32]byte
	priv := pk.raw
	scalarBaseMult(&pub, &priv)
	return PublicKey{raw: pub}, nil
}

// Base64 returns the base64-encoded public key string for transmission.
func (pub PublicKey) Base64() string {
	return encodeBase64(pub.raw[:])
}

// SaveToFile saves the private key to a file in base64 format.
// The directory is created if it doesn't exist.
//
//	err = key.SaveToFile("~/.relayly/device.key")
func (pk PrivateKey) SaveToFile(path string) error {
	path = expandHome(path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("relayly: failed to create key directory: %w", err)
	}
	encoded := encodeBase64(pk.raw[:])
	return os.WriteFile(path, []byte(encoded+"\n"), 0600)
}

// LoadKeyFromFile loads a private key from a file saved by SaveToFile.
//
//	key, err := relayly.LoadKeyFromFile("~/.relayly/device.key")
func LoadKeyFromFile(path string) (PrivateKey, error) {
	path = expandHome(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return PrivateKey{}, fmt.Errorf("relayly: failed to read key file %s: %w", path, err)
	}

	// Trim trailing whitespace/newline
	encoded := string(data)
	for len(encoded) > 0 && (encoded[len(encoded)-1] == '\n' || encoded[len(encoded)-1] == '\r' || encoded[len(encoded)-1] == ' ') {
		encoded = encoded[:len(encoded)-1]
	}

	raw, err := decodeBase64(encoded)
	if err != nil {
		return PrivateKey{}, fmt.Errorf("relayly: invalid key file %s: %w", path, err)
	}
	if len(raw) != 32 {
		return PrivateKey{}, fmt.Errorf("relayly: invalid key length in %s: expected 32 bytes, got %d", path, len(raw))
	}

	var key PrivateKey
	copy(key.raw[:], raw)
	return key, nil
}

// LoadOrGenerateKey loads a key from the given file path, or generates and saves
// a new one if the file doesn't exist. This is the recommended way to initialise
// a long-lived device key.
//
//	key, err := relayly.LoadOrGenerateKey("~/.relayly/device.key")
func LoadOrGenerateKey(path string) (PrivateKey, error) {
	key, err := LoadKeyFromFile(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return PrivateKey{}, err
	}

	key, err = GenerateKey()
	if err != nil {
		return PrivateKey{}, err
	}
	if err := key.SaveToFile(path); err != nil {
		return PrivateKey{}, err
	}
	return key, nil
}

// PrivateKeyFromBase64 parses a base64-encoded private key.
func PrivateKeyFromBase64(s string) (PrivateKey, error) {
	raw, err := decodeBase64(s)
	if err != nil {
		return PrivateKey{}, fmt.Errorf("relayly: invalid private key: %w", err)
	}
	if len(raw) != 32 {
		return PrivateKey{}, fmt.Errorf("relayly: invalid private key length: expected 32, got %d", len(raw))
	}
	var pk PrivateKey
	copy(pk.raw[:], raw)
	return pk, nil
}

// Encrypt encrypts plaintext for a recipient using NaCl box.
// Returns (ciphertext, nonce, error).
func (pk PrivateKey) Encrypt(plaintext []byte, recipient PublicKey) ([]byte, [24]byte, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nonce, fmt.Errorf("relayly: failed to generate nonce: %w", err)
	}

	ciphertext := box.Seal(nil, plaintext, &nonce, &recipient.raw, &pk.raw)
	return ciphertext, nonce, nil
}

// Decrypt decrypts a ciphertext from a sender using NaCl box.
func (pk PrivateKey) Decrypt(ciphertext, nonce []byte, sender PublicKey) ([]byte, error) {
	if len(nonce) != 24 {
		return nil, fmt.Errorf("relayly: invalid nonce length: expected 24, got %d", len(nonce))
	}
	var nonceArr [24]byte
	copy(nonceArr[:], nonce)

	plaintext, ok := box.Open(nil, ciphertext, &nonceArr, &sender.raw, &pk.raw)
	if !ok {
		return nil, fmt.Errorf("relayly: decryption failed — corrupted ciphertext or wrong key")
	}
	return plaintext, nil
}

// publicKeyFromBase64 parses a base64-encoded public key.
func publicKeyFromBase64(s string) (PublicKey, error) {
	raw, err := decodeBase64(s)
	if err != nil {
		return PublicKey{}, err
	}
	if len(raw) != 32 {
		return PublicKey{}, fmt.Errorf("relayly: invalid public key length: expected 32, got %d", len(raw))
	}
	var pk PublicKey
	copy(pk.raw[:], raw)
	return pk, nil
}

// encodeBase64 returns a standard base64 (no padding) encoding of b.
func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// decodeBase64 decodes a standard base64 string.
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// expandHome replaces a leading "~" with the user's home directory.
func expandHome(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}



