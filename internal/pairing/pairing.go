// Package pairing implements device registration and pairing token logic.
// Tokens are cryptographically random 32-byte values, base58-encoded for
// human-readability and QR-code compatibility.
package pairing

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nikx-one/relayly/internal/database"
)

// base58Alphabet is the standard Bitcoin base58 alphabet (no 0/O/I/l).
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// GeneratePairToken produces a cryptographically random, base58-encoded
// pairing token suitable for QR codes and manual entry.
func GeneratePairToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return base58Encode(b), nil
}

// NewDevice creates a Device struct with a fresh UUID and pair token.
// It does NOT persist it — call db.CreateDevice() for that.
func NewDevice(name string) (*database.Device, error) {
	token, err := GeneratePairToken()
	if err != nil {
		return nil, err
	}
	return &database.Device{
		ID:        uuid.NewString(),
		Name:      name,
		PairToken: token,
		CreatedAt: time.Now().UTC(),
	}, nil
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
