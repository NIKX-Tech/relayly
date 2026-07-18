# relayly

Rust SDK for [Relayly](https://github.com/NIKX-Tech/relayly) - a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

Async-first (Tokio). Encryption is device-to-device Noise XX
(`Noise_XX_25519_ChaChaPoly_BLAKE2s`) via the `snow` crate (see "Why snow?" below);
the relay itself holds no key material. See
[`docs/PROTOCOL.md`](https://github.com/NIKX-Tech/relayly/blob/main/docs/PROTOCOL.md)
for the full wire spec.

## Install

```toml
[dependencies]
relayly = "0.3"
tokio = { version = "1", features = ["full"] }
```

## Quick start

```rust
use relayly::{connect, load_or_generate_key, Options};
use std::path::Path;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let key = load_or_generate_key(Path::new("~/.relayly/device.key"))?;

    // device_token comes from POST /api/v1/devices
    let (client, mut messages) = connect("wss://relay.example.com/ws", Options {
        device_id: "my-laptop".into(),
        device_token,
        private_key: key,
        ..Default::default()
    }).await?;

    // Receive messages in a background task
    tokio::spawn(async move {
        while let Some(msg) = messages.recv().await {
            println!("[{}] {}", msg.from, String::from_utf8_lossy(&msg.payload));
        }
    });

    // Pair with another device
    let code = client.request_pair_code().await?;
    println!("Share this code: {}", code.short);
    let peer = code.wait().await?;

    // Send an encrypted message
    client.send(&peer.id, b"hello!").await?;

    Ok(())
}
```

## Pairing

**v1 links exactly one peer per device.** Pairing again replaces whatever was linked
before, it doesn't add a second one alongside it. Multi-peer support is a roadmap
item (`docs/ROADMAP.md`, v0.7). Don't build for N simultaneous peers against this
version.

Devices pair using a short 6-digit code shared out-of-band (or via QR). Both
`accept_pair()` and `code.wait()` block until the Noise handshake actually completes
(not just until the code exchange), so the peer they resolve with is immediately safe
to `send()` to.

```rust
// Device A - request a code
let code = client.request_pair_code().await?;
println!("Code: {}", code.short);
println!("QR URL: {}", code.qr_code_url("wss://relay.example.com"));

let peer = code.wait().await?; // blocks until the other device pairs

// Device B - accept the code
let peer = client.accept_pair("483921").await?;
```

## Peer key pinning

Each peer's authenticated static key is pinned on first pairing and checked on every
handshake after — this pin, not the relay, is the real security boundary
(`docs/PROTOCOL.md` §7). By default it's stored at `~/.relayly/peers.json`, the same
schema every other official SDK reads/writes, so a shared machine can keep one pin
store across languages:

```rust
Options {
    device_id: "my-laptop".into(),
    device_token,
    private_key: key,
    peer_store_path: Some(std::path::PathBuf::from("~/.relayly/peers.json")), // default
    ..Default::default()
}
```

A peer presenting a different key than its pin fails with `Error::PeerKeyMismatch` —
this is never auto-retried; unpinning is an explicit action (remove the entry from the
store, or use `relayly::PeerStore` directly).

## Sending messages

```rust
client.send(&peer.id, b"hello!").await?;
```

`send()` returns `Error::NotReady` if the peer's session isn't up yet — in normal use
this only happens briefly after a reconnect forces a re-handshake; use `on_ready` to
know when it recovers.

## Reconnection

The client reconnects automatically with exponential backoff, and re-runs the Noise
handshake per `docs/PROTOCOL.md` §6 (the device with the lexicographically smaller ID
re-initiates; the existing session keeps working until the replacement completes):

```rust
Options {
    device_id: "my-laptop".into(),
    device_token,
    private_key: key,
    reconnect_delay: Some(std::time::Duration::from_secs(2)),
    max_reconnect_delay: std::time::Duration::from_secs(30),
    on_disconnect: Some(Box::new(|reason| eprintln!("disconnected: {reason}"))),
    on_reconnect: Some(Box::new(|| println!("reconnected"))),
    on_ready: Some(Box::new(|peer_id| println!("session ready with {peer_id}"))),
    on_peer_status: Some(Box::new(|peer_id, online| println!("{peer_id} online: {online}"))),
    ..Default::default()
}
```

Set `reconnect_delay: None` to disable automatic reconnection.

## Key management

```rust
use std::path::Path;
use relayly::{generate_key, load_key_from_file, load_or_generate_key};

// Generate a fresh key
let key = generate_key();

// Save and load
key.save_to_file(Path::new("~/.relayly/device.key"))?;
let key = load_key_from_file(Path::new("~/.relayly/device.key"))?;

// Load or generate in one call (recommended)
let key = load_or_generate_key(Path::new("~/.relayly/device.key"))?;
```

## Options

| Field | Type | Default | Description |
|---|---|---|---|
| `device_id` | `String` | - | Unique ID for this device. Required. |
| `device_token` | `String` | - | From `POST /api/v1/devices`. Required. |
| `private_key` | `PrivateKey` | - | X25519 private key. Required. |
| `peer_store_path` | `Option<PathBuf>` | `~/.relayly/peers.json` | Pinned peer key storage path. |
| `ping_interval` | `Duration` | `30s` | Keepalive ping interval. |
| `reconnect_delay` | `Option<Duration>` | `Some(1s)` | Initial reconnect delay. `None` to disable. |
| `max_reconnect_delay` | `Duration` | `60s` | Backoff ceiling. |
| `on_disconnect` | `Option<Box<dyn Fn(&str)>>` | `None` | Called with reason when connection drops. |
| `on_reconnect` | `Option<Box<dyn Fn()>>` | `None` | Called after a successful reconnect. |
| `on_ready` | `Option<Box<dyn Fn(&str)>>` | `None` | Called whenever a peer's session becomes usable for `send()`. |
| `on_peer_status` | `Option<Box<dyn Fn(&str, bool)>>` | `None` | Called on the paired peer's online/offline transitions. |

## Why snow?

`docs/PROTOCOL.md` requires `Noise_XX_25519_ChaChaPoly_BLAKE2s`. The `snow` crate
parses this exact suite by name, is actively maintained (used in production by
Lightning Network and WireGuard-adjacent tooling), and is backed by RustCrypto's
audited `curve25519-dalek`/`chacha20poly1305`/`blake2` crates. This SDK's use of it is
verified byte-for-byte against `flynn/noise` (the Go implementation already used
server-side and in `sdk/go`) using fixed keys and a deterministic ephemeral key
(`snow`'s own `fixed_ephemeral_key_for_testing_only`), not just "the crate claims to
support it."

## Requirements

- Rust 1.75+
- `tokio-tungstenite = "0.23"`
- `snow = "0.9"`
- `x25519-dalek = "2"`

## License

MIT
