# relayly/sdk/go

Go client SDK for [Relayly](https://github.com/NIKX-Tech/relayly) - a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

Encryption is device-to-device Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`, via
[flynn/noise](https://github.com/flynn/noise)); the relay itself holds no key material.
See [`docs/PROTOCOL.md`](../../docs/PROTOCOL.md) for the full wire spec.

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
    // Load or generate a persistent device identity key
    key, err := relayly.LoadOrGenerateKey("~/.relayly/device.key")
    if err != nil {
        log.Fatal(err)
    }

    // deviceToken comes from POST /api/v1/devices (or `relayly pair` on the CLI)
    client, err := relayly.Connect(context.Background(), "wss://relay.example.com/ws", relayly.Options{
        DeviceID:    "my-laptop",
        DeviceToken: deviceToken,
        PrivateKey:  key,
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

    // Pair with another device — blocks until the Noise handshake completes too,
    // so the returned peer can be Sent to immediately.
    code, err := client.RequestPairCode(context.Background())
    fmt.Println("Share this code:", code.Short)

    peer, err := code.Wait(context.Background())
    fmt.Println("Paired with", peer.ID)

    // Send an encrypted message
    client.Send(context.Background(), peer.ID, []byte("hello!"))
}
```

## Pairing

**v1 links exactly one peer per device.** Pairing again replaces whatever was linked
before, it doesn't add a second one alongside it. Multi-peer support is a roadmap
item (`docs/ROADMAP.md`, v0.7) — don't build for N simultaneous peers against this
version.

```go
// Device A - request a code
code, err := client.RequestPairCode(ctx)
fmt.Println("Code:", code.Short)
fmt.Println("QR URL:", code.QRCodeURL("wss://relay.example.com"))

peer, err := code.Wait(ctx) // blocks until the other device pairs AND the handshake completes

// Device B - accept the code (also blocks until the handshake completes)
peer, err := client.AcceptPair(ctx, "483921")
```

`Send` returns `ErrNotReady` if a peer's session isn't up — in normal use this only
happens briefly after a reconnect forces a re-handshake (see `OnReady` below), never
right after `AcceptPair`/`code.Wait` return.

## Peer key pinning

Each peer's authenticated static key is pinned on first pairing and persisted to
`~/.relayly/peers.json` by default (override with `Options.PeerStorePath`). A later
handshake presenting a *different* key for the same peer ID hard-fails with
`ErrPeerKeyMismatch` — this pin, not the relay, is the real security boundary
(`docs/PROTOCOL.md` §7). Unpinning is never automatic; delete the peer's entry from
the store yourself if you intend to re-trust a peer under a new key.

## Reconnection

The client reconnects automatically with exponential backoff (1 s → 60 s), and
re-runs the Noise handshake per the reconnect rules in `docs/PROTOCOL.md` §6 (the
device with the lexicographically smaller ID re-initiates; the existing session keeps
working until the replacement actually completes).

```go
relayly.Options{
    DeviceID:          "my-laptop",
    DeviceToken:       deviceToken,
    PrivateKey:        key,
    ReconnectDelay:    2 * time.Second,  // initial delay (default: 1s)
    MaxReconnectDelay: 30 * time.Second, // backoff ceiling (default: 60s)
    OnDisconnect:      func(err error) { log.Println("disconnected:", err) },
    OnReconnect:       func() { log.Println("reconnected") },
    OnReady:           func(peerID string) { log.Println("session ready with", peerID) },
    OnPeerStatus:      func(peerID string, online bool) { log.Println(peerID, "online:", online) },
}
```

Set `ReconnectDelay: -1` to disable automatic reconnection.

## Key management

Device identity keys are unaffected by the Protocol v1 migration — the on-disk format
(32-byte X25519 key, base64) is unchanged, so existing key files remain valid.

```go
// Generate a fresh key
key, err := relayly.GenerateKey()

// Save and load
key.SaveToFile("~/.relayly/device.key")
key, err = relayly.LoadKeyFromFile("~/.relayly/device.key")

// Load or generate in one call (recommended for long-lived devices)
key, err = relayly.LoadOrGenerateKey("~/.relayly/device.key")
```

Peers must re-pair after upgrading from a pre-Protocol-v1 version of this SDK: the
wire protocol changed (device-to-device Noise XX replaces per-message NaCl box), so
old pairing state doesn't carry over, even though device key files do.

## License

MIT
