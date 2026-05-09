// Package main demonstrates the simplest possible Relayly usage:
// connect to a server, generate a pairing code, and echo back any messages received.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	relayly "github.com/NIKX-Tech/relayly/sdk/go"
)

func main() {
	cfg := LoadConfig()

	// Load or generate a persistent device key
	key, err := relayly.LoadOrGenerateKey(cfg.KeyPath)
	if err != nil {
		log.Fatalf("key error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to the relay server
	client, err := relayly.Connect(ctx, cfg.ServerURL, relayly.Options{
		DeviceID:   cfg.DeviceID,
		PrivateKey: key,
	})
	if err != nil {
		log.Fatalf("connect error: %v", err)
	}
	defer client.Close()

	// Request a pairing code to share with another device
	code, err := client.RequestPairCode(ctx)
	if err != nil {
		log.Fatalf("pair code error: %v", err)
	}

	fmt.Println("╔══════════════════════════════════╗")
	fmt.Printf("║  Pairing code:  %-16s  ║\n", code.Short)
	fmt.Println("╚══════════════════════════════════╝")
	fmt.Println("Share this code with your other device.")
	fmt.Printf("QR URL: %s\n\n", code.QRCodeURL(cfg.ServerURL))

	// Wait for the other device to accept
	fmt.Println("Waiting for peer to connect...")
	peer, err := code.Wait(ctx)
	if err != nil {
		log.Fatalf("pairing failed: %v", err)
	}
	fmt.Printf("✓ Paired with device: %s\n\n", peer.ID)

	// Echo loop — receive messages and echo them back
	fmt.Println("Listening for messages (will echo back). Ctrl+C to quit.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case msg, ok := <-client.Messages():
			if !ok {
				fmt.Println("Connection closed.")
				return
			}
			fmt.Printf("[%s] → %s\n", msg.From, msg.Payload)

			// Echo it back
			reply := fmt.Sprintf("echo: %s", msg.Payload)
			if err := client.Send(ctx, msg.From, []byte(reply)); err != nil {
				log.Printf("send error: %v", err)
			}

		case <-sig:
			fmt.Println("\nShutting down.")
			return
		}
	}
}
