package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/NIKX-Tech/relayly/internal/noise"
	"github.com/NIKX-Tech/relayly/pkg/client"
)

func main() {
	id := flag.String("id", "", "Device ID")
	token := flag.String("token", "", "Pairing Token")
	name := flag.String("name", "Device", "Display Name")
	server := flag.String("server", "ws://localhost:8080/ws", "Relay server URL")
	flag.Parse()

	if *id == "" || *token == "" {
		fmt.Println("Relayly Tester — secure E2EE message relay verification tool")
		fmt.Println("\nUsage:")
		fmt.Println("  go run cmd/relayly-tester/main.go -id <ID> -token <TOKEN> [-name <Name>]")
		os.Exit(1)
	}

	// Generate a unique keypair for this device
	kp, err := noise.GenerateKeypair()
	if err != nil {
		log.Fatalf("Failed to generate Noise keypair: %v", err)
	}

	c, err := client.New(client.Options{
		ServerURL: *server,
		DeviceID:  *id,
		Token:     *token,
		Keypair:   kp,
	})
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := c.Connect(ctx); err != nil {
			log.Fatalf("\n❌ Connection failed: %v", err)
		}
	}()

	fmt.Printf("🚀 [%s] Connecting to %s...\n", *name, *server)
	fmt.Println("   (Type a message and hit Enter to send. Type 'exit' to quit.)")

	// Receiver loop
	go func() {
		for msg := range c.Recv() {
			fmt.Printf("\n📩 [%s] Received: %s\n> ", *name, string(msg))
		}
	}()

	// Sender loop
	fmt.Print("> ")
	var input string
	for {
		// Read full line including spaces
		_, _ = fmt.Scanln(&input)
		if input == "exit" || ctx.Err() != nil {
			return
		}
		if input != "" {
			if err := c.Send([]byte(input)); err != nil {
				fmt.Printf("\n❌ Send failed: %v\n", err)
			}
		}
		fmt.Print("> ")
		input = ""
	}
}
