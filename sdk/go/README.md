# relayly/sdk/go

Go client SDK for [Relayly](https://github.com/NIKX-Tech/relayly) — a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

## Install

```bash
go get github.com/NIKX-Tech/relayly/sdk/go@latest
```

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    relayly "github.com/NIKX-Tech/relayly/sdk/go"
)

func main() {
    // Load or generate a persistent device key
    key, err := relayly.LoadOrGenerateKey("~/.relayly/device.key")
    if err != nil {
        log.Fatal(err)
    }

    client, err := relayly.Connect(context.Background(), "wss://relay.example.com", relayly.Options{
        DeviceID:   "my-laptop",
        PrivateKey: key,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Receive messages
    go func() {
        for msg := range client.Messages() {
            fmt.Printf("[%s] %s\n", msg.From, msg.Payload)
        }
    }()

    // Pair with another device
    code, err := client.RequestPairCode(context.Background())
    fmt.Println("Share this code:", code.Short)

    peer, err := code.Wait(context.Background())
    fmt.Println("Paired with", peer.ID)

    // Send an encrypted message
    client.Send(context.Background(), peer.ID, []byte("hello!"))
}
```

## Pairing

```go
// Device A — request a code
code, err := client.RequestPairCode(ctx)
fmt.Println("Code:", code.Short)
fmt.Println("QR URL:", code.QRCodeURL("wss://relay.example.com"))

peer, err := code.Wait(ctx) // blocks until the other device pairs

// Device B — accept the code
peer, err := client.AcceptPair(ctx, "483921")
```

## Reconnection

The client reconnects automatically with exponential backoff (1 s → 60 s).

```go
relayly.Options{
    DeviceID:          "my-laptop",
    PrivateKey:        key,
    ReconnectDelay:    2 * time.Second,  // initial delay (default: 1s)
    MaxReconnectDelay: 30 * time.Second, // backoff ceiling (default: 60s)
    OnDisconnect:      func(err error) { log.Println("disconnected:", err) },
    OnReconnect:       func() { log.Println("reconnected") },
}
```

Set `ReconnectDelay: -1` to disable automatic reconnection.

## Key management

```go
// Generate a fresh key
key, err := relayly.GenerateKey()

// Save and load
key.SaveToFile("~/.relayly/device.key")
key, err = relayly.LoadKeyFromFile("~/.relayly/device.key")

// Load or generate in one call (recommended for long-lived devices)
key, err = relayly.LoadOrGenerateKey("~/.relayly/device.key")
```

## License

MIT
