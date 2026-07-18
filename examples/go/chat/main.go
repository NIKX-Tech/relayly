// Package main is a live chat demo for Relayly.
//
// It connects two devices to a Relayly server through sdk/go, pairs them with a
// short in-band code, and drops into a live chat loop where you can type messages
// in either terminal and see them arrive on the other side in real time.
//
// # Usage
//
//	# First terminal, request a pairing code and print it:
//	go run .
//
//	# Second terminal, accept the code printed by the first:
//	go run . --code <code-from-first-terminal>
//
// Everything between the two devices is encrypted device-to-device by the Noise XX
// protocol (25519 + ChaChaPoly + BLAKE2s), driven entirely by sdk/go. The relay
// server authenticates both sides but never sees plaintext message content.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	relayly "github.com/NIKX-Tech/relayly/sdk/go"
)

func main() {
	server := flag.String("server", "ws://localhost:8080/ws", "Relayly server URL")
	code := flag.String("code", "", "Pairing code from the other terminal (omit to generate a new one)")
	flag.Parse()

	key, err := relayly.LoadOrGenerateKey("~/.relayly/chat-device.key")
	if err != nil {
		log.Fatalf("key error: %v", err)
	}

	deviceID, deviceToken, err := registerOrLoadDevice(*server, "~/.relayly/chat-device.json", "chat-device")
	if err != nil {
		log.Fatalf("device registration error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println("\n⚡  Interrupted, closing connection …")
		cancel()
	}()

	fmt.Printf("🔌  Connecting to %s …\n", *server)
	client, err := relayly.Connect(ctx, *server, relayly.Options{
		DeviceID:    deviceID,
		DeviceToken: deviceToken,
		PrivateKey:  key,
	})
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Close()
	fmt.Println("✅  Connected.")

	var peer *relayly.Peer
	if *code != "" {
		fmt.Printf("🔐  Accepting pairing code %q …\n", *code)
		peer, err = client.AcceptPair(ctx, *code)
		if err != nil {
			log.Fatalf("pair error: %v", err)
		}
	} else {
		pc, err := client.RequestPairCode(ctx)
		if err != nil {
			log.Fatalf("pair code error: %v", err)
		}
		fmt.Println("╔════════════════════════════════════╗")
		fmt.Printf("║  Pairing code:  %-18s  ║\n", pc.Short)
		fmt.Println("╚════════════════════════════════════╝")
		fmt.Println("Run in your other terminal:")
		fmt.Printf("  go run . --server %s --code %s\n\n", *server, pc.Short)
		fmt.Println("Waiting for the other device …")

		peer, err = pc.Wait(ctx)
		if err != nil {
			log.Fatalf("pairing failed: %v", err)
		}
	}

	fmt.Printf("🔐  Noise XX handshake complete, paired with %s. Transport is encrypted.\n", peer.ID)
	fmt.Println()
	fmt.Println("💬  Chat is live! Type a message and press Enter.")
	fmt.Println("    Type /quit to exit.")
	fmt.Println(strings.Repeat("─", 48))

	go func() {
		for msg := range client.Messages() {
			fmt.Printf("\r\033[K[peer → you] %s\n> ", string(msg.Payload))
		}
		fmt.Println("\n⚠️   Connection closed by server.")
		cancel()
	}()

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

		if err := client.Send(ctx, peer.ID, []byte(line)); err != nil {
			fmt.Printf("⚠️   send error: %v\n", err)
			fmt.Print("> ")
			continue
		}
		fmt.Print("> ")
	}
}
