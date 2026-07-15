<!-- markdownlint-disable MD033 -->
# Relayly

<img src="docs/images/logo.png" width="70" alt="Relayly Logo">

**Lightweight, self-hosted WebSocket relay for local-first device communication.** Working
toward device-to-device end-to-end encryption as [Protocol v1](docs/PROTOCOL.md); see the
status note below before relying on this for anything sensitive.

[![CI](https://img.shields.io/github/actions/workflow/status/NIKX-Tech/relayly/ci.yml?branch=main&style=flat-square&label=build)](https://github.com/NIKX-Tech/relayly/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://img.shields.io/ossf-scorecard/github.com/NIKX-Tech/relayly?label=openssf%20scorecard&style=flat-square)](https://securityscorecards.dev/projects/github.com/NIKX-Tech/relayly)
[![CodeQL](https://img.shields.io/github/actions/workflow/status/NIKX-Tech/relayly/codeql.yml?branch=main&label=codeql&style=flat-square)](https://github.com/NIKX-Tech/relayly/actions/workflows/codeql.yml)
[![License](https://img.shields.io/github/license/NIKX-Tech/relayly?style=flat-square&color=blue)](https://opensource.org/licenses/MIT)
<br>
[![Go Version](https://img.shields.io/github/go-mod/go-version/NIKX-Tech/relayly?style=flat-square)](https://github.com/NIKX-Tech/relayly)
[![Latest Release](https://img.shields.io/github/v/release/NIKX-Tech/relayly?style=flat-square)](https://github.com/NIKX-Tech/relayly/releases)
[![Repo Size](https://img.shields.io/github/repo-size/NIKX-Tech/relayly?style=flat-square)](https://github.com/NIKX-Tech/relayly)
[![GitHub Stars](https://img.shields.io/github/stars/NIKX-Tech/relayly?style=flat-square&color=yellow)](https://github.com/NIKX-Tech/relayly/stargazers)
[![Dependabot](https://img.shields.io/badge/dependabot-enabled-025e8a?logo=dependabot&style=flat-square)](https://github.com/NIKX-Tech/relayly/blob/main/.github/dependabot.yml)
<br>
[![Sponsor GitHub](https://img.shields.io/badge/sponsor-GitHub-EA4AAA?style=flat-square&logo=github-sponsors)](https://github.com/sponsors/NIKX-Tech)
[![Sponsor Open Collective](https://img.shields.io/badge/sponsor-Open%20Collective-00A0E0?style=flat-square&logo=opencollective)](https://opencollective.com/nikx-technologies/projects/relayly)
[![Go Reference](https://pkg.go.dev/badge/github.com/NIKX-Tech/relayly/sdk/go.svg)](https://pkg.go.dev/github.com/NIKX-Tech/relayly/sdk/go)
[![npm](https://img.shields.io/npm/v/relayly?style=flat-square&logo=npm&logoColor=white&label=npm)](https://www.npmjs.com/package/relayly)
[![PyPI](https://img.shields.io/pypi/v/relayly?style=flat-square&logo=python&logoColor=white&label=pypi)](https://pypi.org/project/relayly/)
[![Crates.io](https://img.shields.io/crates/v/relayly?style=flat-square&logo=rust&logoColor=white&label=crates.io)](https://crates.io/crates/relayly)
<br>
[![Website](https://img.shields.io/badge/website-relayly.app-4F46E5?style=flat-square&logo=google-chrome&logoColor=white)](https://relayly.app)
[![Discord](https://img.shields.io/badge/discord-join%20chat-5865F2?style=flat-square&logo=discord&logoColor=white)](https://discord.gg/cTFMfk6V7)

Relayly routes messages between your own devices (phone, laptop, desktop, etc.) through a
relay server you control. Each device authenticates to the relay and runs a [Noise
Protocol](https://noiseprotocol.org/) XX handshake with it.

> **Status:** today the relay itself terminates that Noise session, decrypting each
> message and re-encrypting it for the paired device, it is not yet a zero-knowledge
> relay, despite earlier claims in this README and in code comments. That gap, how it
> happened, and the fix are written up in [RFC-000](docs/rfc/000-protocol-reconciliation.md)
> and the normative [`docs/PROTOCOL.md`](docs/PROTOCOL.md) (device-to-device Noise XX,
> relay holds no cipher states). See [`docs/ROADMAP.md`](docs/ROADMAP.md) for where that
> work stands.

---

## 📖 Table of Contents

- [Features](#-features)
- [How it Works](#-how-it-works)
- [Quick Start](#-quick-start)
- [Official Client SDKs](#-official-client-sdks)
- [CLI Reference](#-cli-reference)
- [Configuration](#-configuration)
- [Admin UI](#-admin-ui)
- [WebSocket Connection Protocol](#-websocket-connection-protocol)
- [Production Deployment](#-production-deployment)
- [Security & Privacy](#-security--privacy)
- [Contributing](#-contributing)

---

## ✨ Features

| Feature | Detail |
|---|---|
| 🔐 **Transport Encryption** | Noise Protocol XX (X25519, ChaChaPoly) between each device and the relay today; device-to-device end-to-end encryption is landing as [Protocol v1](docs/PROTOCOL.md) |
| 📱 **Device Pairing** | 6-digit short code or QR code, no accounts required |
| ⚡ **Real-time Forwarding** | Low-latency WebSocket relaying with minimal server overhead |
| ♻️ **Auto-reconnect** | Exponential-backoff reconnection built into SDKs |
| 🗄️ **Zero-Config Storage** | Embedded SQLite storage, no external database required |
| 🐳 **Infrastructure Ready** | Pre-built Docker images and single portable binary |
| 🖥️ **Interactive Admin** | HTMX-powered dashboard for device and pairing management |
| 🔑 **Trustless Architecture** | Public Key Locking prevents server-side impersonation |

---

## ⚙️ How it Works

Relayly authenticates each device and mediates pairing between them. As implemented
today it is **not** a blind forwarder: each device runs its own Noise XX handshake
directly with the server, and the server decrypts and re-encrypts every message in
between.

```mermaid
sequenceDiagram
    participant A as Device A
    participant R as Relayly Server
    participant B as Device B

    Note over A,R: Noise XX Handshake (A <-> Server)
    A->>R: Handshake Message 1 (Ephemeral Pubkey)
    R->>A: Handshake Message 2 (Encrypted Static + Ephemeral)
    A->>R: Handshake Message 3 (Encrypted Static)

    Note over R,B: Noise XX Handshake (Server <-> B), independently
    B->>R: Handshake Message 1 (Ephemeral Pubkey)
    R->>B: Handshake Message 2 (Encrypted Static + Ephemeral)
    B->>R: Handshake Message 3 (Encrypted Static)

    Note over A,B: Transport: the server decrypts and re-encrypts each message
    A->>R: Ciphertext (A's session key)
    R->>B: Re-encrypted ciphertext (B's session key)
    B->>R: Ciphertext (B's session key)
    R->>A: Re-encrypted ciphertext (A's session key)
```

### Encryption Details

Relayly uses **Noise Protocol XX** for the handshake and transport encryption between
each device and the server. This provides:

- **Mutual Authentication**: each device and the server verify each other's static
  public keys during that device's own handshake.
- **Forward Secrecy**: session keys are ephemeral and discarded when a connection ends.
- **Not yet zero-knowledge**: the server holds live Noise cipher states for every
  connected device and briefly holds plaintext in memory to re-encrypt it for the
  paired device. It does not persist or log message content. True device-to-device
  end-to-end encryption, where the relay never holds key material capable of reading
  traffic, is the target of [Protocol v1](docs/PROTOCOL.md); see
  [RFC-000](docs/rfc/000-protocol-reconciliation.md) for why this changed and the plan
  to get there.

---

## 🚀 Quick Start

### 1. Server Setup (Docker)

The fastest way to get a relay running is via Docker:

```bash
git clone https://github.com/NIKX-Tech/relayly.git
cd relayly
docker compose up --build -d

# Register your first device
docker exec relayly /relayly pair "My Device"

# Want to test it? Try the Chat Demo:
# cd examples/go/chat && ./setup.sh
```

---

## 🎮 Interactive Examples

Check out the `examples/` directory for ready-to-run implementations:

| Example | Language | Description |
|---|---|---|
| [**Chat Demo**](examples/go/chat) | Go | **(Recommended)** Live E2EE chat between two terminals |
| [Clipboard Sync](examples/go/clipboard-sync) | Go | Sync clipboard across devices automatically |
| [Basic Echo](examples/go/basic) | Go | Simplest possible connect and message loop |
| [Pair & Send](examples/go/pair-and-send) | Go | CLI pairing and one-shot message exchange |
| [Node.js Send](examples/ts/node) | TypeScript | Connect, pair, and send from Node.js |
| [Echo Server](examples/ts/echo) | TypeScript | Minimal echo client in TypeScript |

### 2. Server Setup (Local)

```bash
# Build the binary (Requires Go 1.22+)
go build -o relayly ./cmd/relayly

# Start the server
./relayly start

# In another terminal, generate a pairing code
./relayly pair "My Phone"
```

---

## 📦 Official Client SDKs

Official SDKs for Go, TypeScript, and Python are in the `sdk/` directory and published to their respective package registries.

### Go SDK

```bash
go get github.com/NIKX-Tech/relayly/sdk/go
```

```go
import relayly "github.com/NIKX-Tech/relayly/sdk/go"

key, _ := relayly.LoadOrGenerateKey("~/.relayly/device.key")

client, _ := relayly.Connect(ctx, "wss://your-server/ws", relayly.Options{
    DeviceID:   "my-laptop",
    PrivateKey: key,
})
defer client.Close()

code, _ := client.RequestPairCode(ctx)
fmt.Println("Code:", code.Short)

peer, _ := client.AcceptPair(ctx, "483921")
client.Send(ctx, peer.ID, []byte("hello!"))
msg := <-client.Messages()
```

[pkg.go.dev/github.com/NIKX-Tech/relayly/sdk/go](https://pkg.go.dev/github.com/NIKX-Tech/relayly/sdk/go)

### TypeScript / JavaScript SDK

```bash
npm install relayly
```

```typescript
import { RelaylyClient, generateKey } from 'relayly';

const client = new RelaylyClient('wss://your-server', {
  deviceId: 'my-laptop',
  keyPair: generateKey(),
});

await client.connect();
client.on('message', (msg) => console.log(msg.payload));
```

[npmjs.com/package/relayly](https://www.npmjs.com/package/relayly) - works in Node.js, browsers, and React Native.

### Python SDK

```bash
pip install relayly
```

```python
import asyncio, relayly

async def main():
    key = relayly.load_or_generate_key("~/.relayly/device.key")
    client = await relayly.connect("wss://your-server", relayly.Options(
        device_id="my-laptop",
        private_key=key,
    ))
    async for msg in client.messages():
        print(msg.payload.decode())

asyncio.run(main())
```

[pypi.org/project/relayly](https://pypi.org/project/relayly/)

### Rust SDK

```toml
[dependencies]
relayly = "0.3"
tokio = { version = "1", features = ["full"] }
```

```rust
use relayly::{connect, load_or_generate_key, Options};
use std::path::Path;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let key = load_or_generate_key(Path::new("~/.relayly/device.key"))?;
    let (client, mut messages) = connect("wss://your-server", Options {
        device_id: "my-laptop".into(),
        private_key: key,
        ..Default::default()
    }).await?;

    tokio::spawn(async move {
        while let Some(msg) = messages.recv().await {
            println!("[{}] {}", msg.from, String::from_utf8_lossy(&msg.payload));
        }
    });

    let code = client.request_pair_code().await?;
    println!("Share this code: {}", code.short);
    let peer = code.wait().await?;
    client.send(&peer.id, b"hello!").await?;
    Ok(())
}
```

[crates.io/crates/relayly](https://crates.io/crates/relayly)

---

## 💻 CLI Reference

| Command | Description |
|---|---|
| `relayly start` | Start relay + admin servers |
| `relayly start --config path/to/relayly.yaml` | Use custom config |
| `relayly pair <name>` | Register device, print QR code |
| `relayly pair <name> --no-qr` | Print token only |
| `relayly link <id1> <id2>` | Pair two devices for relaying |
| `relayly status` | Show connected devices + uptime |
| `relayly status --format=json` | Machine-readable output |

---

## 🔧 Configuration

All options can be set in `config/relayly.yaml` or via environment variables (`RELAYLY_<KEY>`, e.g. `RELAYLY_PORT=9090`):

| Key | Default | Description |
|---|---|---|
| `host` | `0.0.0.0` | Listen address |
| `port` | `8080` | Relay WebSocket port |
| `db.path` | `./data/relayly.db` | SQLite file |
| `noise.key_path` | `./data/server.noise.key` | Server Noise keypair |
| `admin.enabled` | `true` | Enable admin UI |
| `admin.host` | `127.0.0.1` | Admin bind address |
| `admin.port` | `8081` | Admin port |
| `log.level` | `info` | `debug|info|warn|error` |
| `log.format` | `json` | `json|console` |
| `tls.enabled` | `false` | Enable TLS (or use reverse proxy) |

---

## 🖥️ Admin UI

Visit `http://localhost:8081` after starting the server.

- **Dashboard**: Live connection count, uptime, device list.
- **Devices**: Full device management with one-click revoke.
- Auto-refreshes every 5 seconds via HTMX.

> ⚠️ The admin UI binds to `127.0.0.1` by default. Do not expose it publicly without authentication.

---

## 🔌 WebSocket Connection Protocol

> This section describes the connection protocol **as implemented today**: each device
> runs Noise XX with the server itself. `docs/PROTOCOL.md` defines Protocol v1 (Noise XX
> device-to-device, zero cipher states on the relay); this section will be rewritten once
> that server work (`docs/tasks/01-server.md`) lands.

Clients connect to:
`ws://<host>:<port>/ws?device_id=<uuid>&token=<pair-token>`

### Noise XX Handshake (3 messages, client <-> server)

1. **Client → Server**: [msg1: ephemeral pubkey]
2. **Server → Client**: [msg2: encrypted server static + ephemeral]
3. **Client → Server**: [msg3: encrypted client static]

After handshake, subsequent frames are binary ciphertext under that connection's Noise
session keys. The relay decrypts each frame from the sender and re-encrypts it for the
paired device's own session, it does not persist or log the plaintext, but it does
transiently hold it in memory, so this is not currently a zero-knowledge relay.

---

## 🚢 Production Deployment

### Recommended: Caddy as reverse proxy

```caddy
relay.yourdomain.com {
    reverse_proxy localhost:8080
}
```

### Security checklist

- [ ] Run behind TLS (Caddy / nginx)
- [ ] Bind admin UI to `127.0.0.1` (default)
- [ ] Mount `/data` as a persistent volume (contains DB + keypair)
- [ ] Back up `/data/relayly.db` and `/data/server.noise.key`

---

## 🛡️ Security & Privacy

Relayly is built on the principle of **Privacy by Design**:

- **Zero Data Harvesting**: No accounts, emails, or tracking.
- **Public Key Locking**: Once a device connects, the server "locks" it to that public key. Even a compromised server cannot swap keys without manual admin intervention.
- **Auditability**: Small, dependency-light codebase written in memory-safe Go.

---

## 🏗 Project Architecture

```text
relayly/
├── cmd/relayly/      # Main server entry point
├── internal/         # Private server logic (Relay, Database, Admin)
├── sdk/              # Official Client SDKs (Go, TS)
├── examples/         # Reference implementations
├── docs/             # Protocol specs & architecture deep-dives
├── .github/          # Unified CI/CD workflows
└── Dockerfile        # Optimized production image
```

---

## 💬 Community

Have questions or want to show off what you're building? Join our [Discord Server](https://discord.gg/cTFMfk6V7) to connect with other developers and get real-time support.

---

## 🤝 Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details on our code of conduct, and the process for submitting pull requests to us.

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.

© [NIKX Technologies B.V.](https://github.com/NIKX-Tech)
