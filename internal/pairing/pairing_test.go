package pairing_test

import (
	"strings"
	"testing"

	"github.com/nikx-one/relayly/internal/pairing"
)

func TestGeneratePairToken(t *testing.T) {
	t.Parallel()

	token, err := pairing.GeneratePairToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Tokens must only contain base58 characters
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	for _, ch := range token {
		if !strings.ContainsRune(alphabet, ch) {
			t.Errorf("token contains invalid character: %q", ch)
		}
	}
}

func TestGeneratePairToken_Uniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		token, err := pairing.GeneratePairToken()
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if _, dup := seen[token]; dup {
			t.Fatalf("duplicate token at iteration %d: %s", i, token)
		}
		seen[token] = struct{}{}
	}
}

func TestNewDevice(t *testing.T) {
	t.Parallel()

	d, err := pairing.NewDevice("test-device")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Name != "test-device" {
		t.Errorf("expected name 'test-device', got %q", d.Name)
	}
	if d.ID == "" {
		t.Error("ID must not be empty")
	}
	if d.PairToken == "" {
		t.Error("PairToken must not be empty")
	}
}
