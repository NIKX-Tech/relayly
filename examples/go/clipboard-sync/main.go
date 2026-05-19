// clipboard-sync: share clipboard content between devices via a Relayly relay.
//
// Usage:
//
//	# On the first device — request a pairing code and print it:
//	clipboard-sync --server ws://localhost:8080
//
//	# On the second device — accept the code printed by the first device:
//	clipboard-sync --server ws://localhost:8080 --code <code-from-first-device>
//
// After pairing both devices poll the clipboard every 500 ms.
// When the local clipboard changes the new content is sent to the peer.
// When a message arrives from the peer the local clipboard is updated.
//
// Platform notes:
//   - macOS: uses pbpaste / pbcopy
//   - Other platforms: reading/writing the clipboard prints a warning and is a no-op.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	relayly "github.com/NIKX-Tech/relayly/sdk/go"
)

const (
	pollInterval = 500 * time.Millisecond
	keyPath      = "~/.relayly/clipboard.key"
	deviceIDEnv  = "RELAYLY_DEVICE_ID"
)

func main() {
	server := flag.String("server", "ws://localhost:8080", "Relay server URL")
	code := flag.String("code", "", "Pairing code from peer device (omit to generate a new one)")
	flag.Parse()

	// Load or generate a persistent device key.
	key, err := relayly.LoadOrGenerateKey(keyPath)
	if err != nil {
		log.Fatalf("key error: %v", err)
	}

	// Use a stable device ID from the environment, or derive one from the key.
	deviceID := os.Getenv(deviceIDEnv)
	if deviceID == "" {
		pub, err := key.PublicKey()
		if err != nil {
			log.Fatalf("deriving public key: %v", err)
		}
		// Use first 16 chars of the base64 public key as a stable device ID.
		b64 := pub.Base64()
		if len(b64) > 16 {
			b64 = b64[:16]
		}
		deviceID = "clipboard-" + b64
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to the relay server.
	client, err := relayly.Connect(ctx, *server, relayly.Options{
		DeviceID:   deviceID,
		PrivateKey: key,
	})
	if err != nil {
		log.Fatalf("connect error: %v", err)
	}
	defer client.Close()

	fmt.Printf("Connected as device %q\n", deviceID)

	var peer *relayly.Peer

	if *code != "" {
		// Accepting side: use the code the other device printed.
		fmt.Printf("Accepting pairing code %q…\n", *code)
		p, err := client.AcceptPair(ctx, *code)
		if err != nil {
			log.Fatalf("pair error: %v", err)
		}
		peer = p
		fmt.Printf("Paired with %s\n", peer.ID)
	} else {
		// Initiating side: request a fresh pair code.
		pc, err := client.RequestPairCode(ctx)
		if err != nil {
			log.Fatalf("pair code error: %v", err)
		}
		fmt.Println("╔════════════════════════════════════╗")
		fmt.Printf("║  Pairing code:  %-18s  ║\n", pc.Short)
		fmt.Println("╚════════════════════════════════════╝")
		fmt.Println("Run on your other device:")
		fmt.Printf("  clipboard-sync --server %s --code %s\n\n", *server, pc.Short)
		fmt.Println("Waiting for peer to connect…")

		p, err := pc.Wait(ctx)
		if err != nil {
			log.Fatalf("pairing failed: %v", err)
		}
		peer = p
		fmt.Printf("Paired with %s\n", peer.ID)
	}

	fmt.Println("Clipboard sync active. Press Ctrl+C to quit.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// Poll the clipboard and send changes to the peer.
	go func() {
		last := readClipboard()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
				current := readClipboard()
				if current != last && current != "" {
					last = current
					if err := client.Send(ctx, peer.ID, []byte(current)); err != nil {
						log.Printf("send error: %v", err)
					}
				}
			}
		}
	}()

	// Receive clipboard content from the peer.
	for {
		select {
		case msg, ok := <-client.Messages():
			if !ok {
				fmt.Println("Connection closed.")
				return
			}
			text := string(msg.Payload)
			fmt.Printf("[peer] clipboard updated (%d bytes)\n", len(text))
			writeClipboard(text)

		case <-sig:
			fmt.Println("\nShutting down.")
			return
		}
	}
}

// readClipboard returns the current clipboard text, or "" on error / unsupported platform.
func readClipboard() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// writeClipboard sets the clipboard text. On non-macOS it prints a warning instead.
func writeClipboard(text string) {
	if runtime.GOOS != "darwin" {
		fmt.Printf("[warning] clipboard write not supported on %s; received: %s\n", runtime.GOOS, text)
		return
	}
	cmd := exec.Command("pbcopy")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("pbcopy stdin error: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("pbcopy start error: %v", err)
		return
	}
	if _, err := fmt.Fprint(stdin, text); err != nil {
		log.Printf("pbcopy write error: %v", err)
	}
	_ = stdin.Close()
	_ = cmd.Wait()
}
