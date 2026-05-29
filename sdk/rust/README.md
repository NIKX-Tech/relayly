# relayly

Rust SDK for [Relayly](https://github.com/NIKX-Tech/relayly) - a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

Async-first (Tokio), with full feature parity with the Go, TypeScript, and Python SDKs.

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

    let (client, mut messages) = connect("wss://relay.example.com", Options {
        device_id: "my-laptop".into(),
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

```rust
// Device A - request a code
let code = client.request_pair_code().await?;
println!("Code: {}", code.short);
println!("QR URL: {}", code.qr_code_url("wss://relay.example.com"));

let peer = code.wait().await?; // blocks until the other device pairs

// Device B - accept the code
let peer = client.accept_pair("483921").await?;
```

## Reconnection

The client reconnects automatically with exponential backoff (1s to 60s).

```rust
Options {
    device_id: "my-laptop".into(),
    private_key: key,
    reconnect_delay: Some(std::time::Duration::from_secs(2)),
    max_reconnect_delay: std::time::Duration::from_secs(30),
    on_disconnect: Some(Box::new(|reason| eprintln!("disconnected: {reason}"))),
    on_reconnect: Some(Box::new(|| println!("reconnected"))),
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
| `private_key` | `PrivateKey` | - | X25519 private key. Required. |
| `ping_interval` | `Duration` | `30s` | Keepalive ping interval. |
| `reconnect_delay` | `Option<Duration>` | `Some(1s)` | Initial reconnect delay. `None` to disable. |
| `max_reconnect_delay` | `Duration` | `60s` | Backoff ceiling. |
| `on_disconnect` | `Option<Box<dyn Fn(&str)>>` | `None` | Called with reason when connection drops. |
| `on_reconnect` | `Option<Box<dyn Fn()>>` | `None` | Called after a successful reconnect. |

## License

MIT
