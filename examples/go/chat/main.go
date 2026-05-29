// Package main is a live chat demo for Relayly.
//
// It connects two "devices" to a locally running Relayly server, performs the
// Noise XX handshake, and then enters a live chat loop where you can type
// messages in either terminal and see them arrive on the other side in real time.
//
// # Pre-requisites
//
// Both devices must be registered and linked in the server's database before
// connecting.  The helper script examples/go/chat/setup.sh does this for you.
// See the README for exact steps.
//
// # Usage
//
//	go run . --role=a --device-id=<id> --token=<token>
//	go run . --role=b --device-id=<id> --token=<token>
//
// Everything between the two devices is encrypted at the transport layer by
// the Noise XX protocol (25519 + ChaChaPoly + BLAKE2s). The relay server
// authenticates both sides but never sees plaintext message content.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	flnoise "github.com/flynn/noise"
	"github.com/gorilla/websocket"
)

// ─── Noise cipher suite — must match the server ──────────────────────────────
// Noise_XX_25519_ChaChaPoly_BLAKE2s
var cipherSuite = flnoise.NewCipherSuite(
	flnoise.DH25519,
	flnoise.CipherChaChaPoly,
	flnoise.HashBLAKE2s,
)

func main() {
	// ── Flags ────────────────────────────────────────────────────────────────
	role := flag.String("role", "", `"a" or "b" — just a label shown in prompts`)
	deviceID := flag.String("device-id", "", "device_id printed by setup.sh (required)")
	token := flag.String("token", "", "token printed by setup.sh (required)")
	server := flag.String("server", "ws://localhost:8080", "Relayly server URL")
	flag.Parse()

	if *deviceID == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "error: --device-id and --token are required")
		fmt.Fprintln(os.Stderr, "  Run ./setup.sh first to register the devices, then copy the values it prints.")
		os.Exit(1)
	}
	if *role == "" {
		*role = *deviceID // fall back to device ID as the display name
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel on Ctrl-C / SIGTERM for a clean shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println("\n⚡  Interrupted — closing connection …")
		cancel()
	}()

	// ── Connect ──────────────────────────────────────────────────────────────
	wsURL := fmt.Sprintf("%s/ws?device_id=%s&token=%s", *server, *deviceID, *token)
	fmt.Printf("🔌  Connecting to %s …\n", *server)

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	fmt.Println("✅  Connected.")

	// ── Noise XX handshake (client role = initiator) ──────────────────────
	// The server performs the XX responder role; we are always the initiator.
	// We load or generate a keypair and persist it to a temporary file so that
	// restarting the client doesn't cause a "public key mismatch" on the server.
	keyPath := fmt.Sprintf("%s/relayly-%s.key", os.TempDir(), *deviceID)
	clientKP, err := loadOrCreateKey(keyPath)
	if err != nil {
		log.Fatalf("key error: %v", err)
	}

	hs, err := flnoise.NewHandshakeState(flnoise.Config{
		CipherSuite:   cipherSuite,
		Random:        rand.Reader,
		Pattern:       flnoise.HandshakeXX,
		Initiator:     true,
		StaticKeypair: clientKP,
	})
	if err != nil {
		log.Fatalf("handshake init: %v", err)
	}

	// Message 1: client → server
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		log.Fatalf("handshake msg1: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, msg1); err != nil {
		log.Fatalf("send msg1: %v", err)
	}

	// Message 2: server → client
	_, raw2, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("recv msg2: %v", err)
	}
	if _, _, _, err := hs.ReadMessage(nil, raw2); err != nil {
		log.Fatalf("handshake msg2: %v", err)
	}

	// Message 3: client → server  (handshake completes, cipher states returned)
	msg3, cs1, cs2, err := hs.WriteMessage(nil, nil)
	if err != nil {
		log.Fatalf("handshake msg3: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, msg3); err != nil {
		log.Fatalf("send msg3: %v", err)
	}

	// cs1 = encrypt (client → server), cs2 = decrypt (server → client)
	encCS, decCS := cs1, cs2

	fmt.Println("🔐  Noise XX handshake complete — transport is encrypted.")
	fmt.Println()
	fmt.Println("💬  Chat is live! Type a message and press Enter.")
	fmt.Println("    Type /quit to exit.")
	fmt.Println(strings.Repeat("─", 48))

	// ── Incoming message goroutine ────────────────────────────────────────
	go func() {
		for {
			_, frame, err := conn.ReadMessage()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					fmt.Println("\n⚠️   Connection closed by server.")
					cancel()
					return
				}
			}

			// Decrypt the frame using the receive cipher state.
			plaintext, err := decCS.Decrypt(nil, nil, frame)
			if err != nil {
				// Could be a ping/control frame or a corrupted packet — skip.
				continue
			}
			fmt.Printf("\r\033[K[peer → you] %s\n> ", string(plaintext))
		}
	}()

	// ── Stdin → send loop ─────────────────────────────────────────────────
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}
		if strings.EqualFold(line, "/quit") {
			fmt.Println("👋  Goodbye!")
			return
		}

		// Encrypt with the send cipher state.
		ciphertext, err := encCS.Encrypt(nil, nil, []byte(line))
		if err != nil {
			fmt.Printf("⚠️   encrypt error: %v\n", err)
			fmt.Print("> ")
			continue
		}

		if err := conn.WriteMessage(websocket.BinaryMessage, ciphertext); err != nil {
			fmt.Printf("⚠️   send error: %v\n", err)
			return
		}
		fmt.Print("> ")
	}
}

// ─── Key persistence helpers ──────────────────────────────────────────────────

// loadOrCreateKey loads a Noise keypair from path or generates a new one.
func loadOrCreateKey(path string) (flnoise.DHKey, error) {
	data, err := os.ReadFile(path)
	if err == nil && len(data) == 64 {
		return flnoise.DHKey{
			Private: data[:32],
			Public:  data[32:],
		}, nil
	}

	// Generate new
	kp, err := cipherSuite.GenerateKeypair(rand.Reader)
	if err != nil {
		return flnoise.DHKey{}, err
	}

	// Save
	buf := make([]byte, 64)
	copy(buf[:32], kp.Private)
	copy(buf[32:], kp.Public)
	if err := os.WriteFile(path, buf, 0600); err != nil {
		fmt.Printf("⚠️   Warning: failed to save key to %s: %v\n", path, err)
	}

	return kp, nil
}
