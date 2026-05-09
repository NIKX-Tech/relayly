package relayly

import (
	"bytes"
	"testing"
)

func TestKeyGeneration(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	pub, err := key.PublicKey()
	if err != nil {
		t.Fatalf("failed to derive public key: %v", err)
	}

	if len(pub.raw) != 32 {
		t.Errorf("expected 32-byte public key, got %d", len(pub.raw))
	}
}

func TestEncryptionDecryption(t *testing.T) {
	// Generate keys for Alice and Bob
	aliceKey, _ := GenerateKey()
	alicePub, _ := aliceKey.PublicKey()

	bobKey, _ := GenerateKey()
	bobPub, _ := bobKey.PublicKey()

	plaintext := []byte("hello bob, this is alice")

	// Alice encrypts for Bob
	ciphertext, nonce, err := aliceKey.Encrypt(plaintext, bobPub)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Bob decrypts from Alice
	decrypted, err := bobKey.Decrypt(ciphertext, nonce[:], alicePub)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted content does not match plaintext.\nGot:  %s\nWant: %s", decrypted, plaintext)
	}
}

func TestBase64Keys(t *testing.T) {
	key, _ := GenerateKey()
	pub, _ := key.PublicKey()

	b64 := pub.Base64()
	if b64 == "" {
		t.Error("base64 encoding returned empty string")
	}

	pub2, err := publicKeyFromBase64(b64)
	if err != nil {
		t.Fatalf("failed to parse public key from base64: %v", err)
	}

	if pub.raw != pub2.raw {
		t.Error("reconstructed public key does not match original")
	}
}
