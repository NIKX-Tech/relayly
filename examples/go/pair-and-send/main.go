// Package main shows how to accept a pairing code from another device,
// then send a one-shot encrypted message and exit.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	relayly "github.com/NIKX-Tech/relayly/sdk/go"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <server-url> <pair-code>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s wss://relay.example.com 483921\n", os.Args[0])
		os.Exit(1)
	}

	serverURL := os.Args[1]
	pairCode := os.Args[2]

	// Load or generate a persistent device key
	key, err := relayly.LoadOrGenerateKey("~/.relayly/device.key")
	if err != nil {
		log.Fatalf("key error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to the relay server
	client, err := relayly.Connect(ctx, serverURL, relayly.Options{
		DeviceID:   "my-phone",
		PrivateKey: key,
	})
	if err != nil {
		log.Fatalf("connect error: %v", err)
	}
	defer client.Close()

	// Accept the pairing code from the other device
	fmt.Printf("Accepting pair code %s...\n", pairCode)
	peer, err := client.AcceptPair(ctx, pairCode)
	if err != nil {
		log.Fatalf("pair error: %v", err)
	}
	fmt.Printf("✓ Paired with: %s\n", peer.ID)

	// Send a hello message
	message := "Hello from Go! 👋"
	if err := client.Send(ctx, peer.ID, []byte(message)); err != nil {
		log.Fatalf("send error: %v", err)
	}
	fmt.Printf("✓ Sent: %q\n", message)

	// Wait briefly for a reply
	fmt.Println("Waiting for reply (5 seconds)...")
	select {
	case msg, ok := <-client.Messages():
		if !ok {
			fmt.Println("Connection closed.")
			return
		}
		fmt.Printf("✓ Reply from %s: %s\n", msg.From, msg.Payload)
	case <-time.After(5 * time.Second):
		fmt.Println("No reply received.")
	}
}
