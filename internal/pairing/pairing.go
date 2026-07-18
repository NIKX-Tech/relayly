// Package pairing implements device registration and pairing token logic.
// Tokens are cryptographically random 32-byte values, base58-encoded for
// human-readability and QR-code compatibility.
package pairing

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/google/uuid"
)

// base58Alphabet is the standard Bitcoin base58 alphabet (no 0/O/I/l).
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// GenerateDeviceToken produces a cryptographically random, base58-encoded device
// token (docs/PROTOCOL.md §2), the bearer credential used to open the WebSocket
// connection. It is unrelated to a device's E2E static key.
func GenerateDeviceToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return base58Encode(b), nil
}

// deviceTokenTTL is how long a freshly registered device's token remains valid before
// a connection attempt is rejected with "pairing code expired" (a legacy error string,
// this is really about the device token, not an in-band pairing code, see PairingCodeTTL
// below for that distinct concept).
const deviceTokenTTL = 5 * time.Minute

// NewDevice creates a Device struct with a fresh UUID and device token.
// The token expires after deviceTokenTTL (5 minutes).
// It does NOT persist it — call db.CreateDevice() for that.
func NewDevice(name string) (*database.Device, error) {
	token, err := GenerateDeviceToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(deviceTokenTTL)
	return &database.Device{
		ID:          uuid.NewString(),
		Name:        name,
		DeviceToken: token,
		CreatedAt:   now,
		ExpiresAt:   &expiresAt,
	}, nil
}

// PairingCodeTTL is how long an in-band pairing code (docs/PROTOCOL.md §5.3) stays
// valid before a pair_accept must be rejected with code_expired.
const PairingCodeTTL = 5 * time.Minute

// GeneratePairingCode produces a random 6-digit numeric code (e.g. "048392") for the
// in-band pair_request/pair_accept flow. It is not persisted here: the caller (the
// relay's pairing-code registry) is responsible for tracking its TTL and single use.
func GeneratePairingCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	n := binary.BigEndian.Uint32(b[:]) % 1_000_000
	return fmt.Sprintf("%06d", n), nil
}

// base58Encode encodes arbitrary bytes using the base58 alphabet.
func base58Encode(input []byte) string {
	// Count leading zeros
	leadingZeros := 0
	for _, b := range input {
		if b != 0 {
			break
		}
		leadingZeros++
	}

	// Convert byte slice to a big integer, then divide repeatedly by 58.
	// We work with a uint64 accumulator in 8-byte chunks for efficiency.
	encoded := make([]byte, 0, len(input)*137/100+leadingZeros)
	for i := 0; i < len(input); i += 8 {
		end := i + 8
		if end > len(input) {
			end = len(input)
		}
		chunk := make([]byte, 8)
		copy(chunk[8-(end-i):], input[i:end])
		num := binary.BigEndian.Uint64(chunk)
		for num > 0 {
			mod := num % 58
			encoded = append(encoded, base58Alphabet[mod])
			num /= 58
		}
	}

	for i := 0; i < leadingZeros; i++ {
		encoded = append(encoded, base58Alphabet[0])
	}

	// Reverse
	for l, r := 0, len(encoded)-1; l < r; l, r = l+1, r-1 {
		encoded[l], encoded[r] = encoded[r], encoded[l]
	}

	return string(encoded)
}
