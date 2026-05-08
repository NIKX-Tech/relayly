# Relayly

[![CI](https://github.com/nikx-one/relayly/actions/workflows/ci.yml/badge.svg)](https://github.com/nikx-one/relayly/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

> **Lightweight, self-hosted WebSocket relay for local-first, end-to-end encrypted device communication.**

Relayly lets your own devices — phone, laptop, desktop — talk to each other
through a server you control, with all payloads encrypted using the
[Noise Protocol](https://noiseprotocol.org/) (Noise_XX_25519_ChaChaPoly_BLAKE2s).
The relay never sees plaintext.

---

## Features

- 🔐 **End-to-end encryption** via Noise Protocol XX handshake
- ⚡ **WebSocket relay** — zero-latency message forwarding between paired devices
- 🗄️ **SQLite storage** — pure-Go driver (`modernc.org/sqlite`), no CGo required
- 🐳 **Single binary + Docker** — `docker compose up` and you're running
- 🖥️ **Admin UI** — HTMX + Tailwind dashboard for device management (auto-refreshes)
- 🔑 **QR code pairing** — scan to pair, no manual token entry needed
- 🧰 **Reference client library** — `pkg/client` shows you exactly how to connect and E2EE

---

## Quick Start

### Docker (recommended)

```bash
git clone https://github.com/nikx-one/relayly.git
cd relayly

docker compose up --build -d

# Register your first device
docker exec relayly /relayly pair myphone
```

### Local build

```bash
# Prerequisites: Go 1.22+
go build -o relayly ./cmd/relayly

# Start the server
./relayly start

# In a new terminal: register a device
./relayly pair "My Phone"

# Check status
./relayly status
```

---

## Project Structure

```
relayly/
├── cmd/relayly/main.go           # Entry point
├── internal/
│   ├── cli/                      # Cobra CLI commands
│   │   ├── root.go               # Base command + help
│   │   ├── start.go              # `relayly start`
│   │   ├── status.go             # `relayly status`
│   │   └── pair.go               # `relayly pair <name>`
│   ├── config/config.go          # Viper config loader
│   ├── database/
│   │   ├── db.go                 # SQLite connection + migrations
│   │   └── pairing.go            # Device CRUD
│   ├── pairing/pairing.go        # Token generation + device creation
│   ├── noise/noise.go            # Noise Protocol XX helpers
│   ├── relay/
│   │   ├── hub.go                # In-memory session hub
│   │   ├── client.go             # WS client lifecycle (read/write pumps)
│   │   └── handler.go            # HTTP → WS upgrade + auth
│   └── admin/
│       ├── server.go             # Admin HTTP server + REST API
│       └── templates/            # Embedded HTMX + Tailwind UI
├── pkg/
│   ├── client/client.go          # Reference E2EE client library
│   └── version/version.go        # Build-time version info
├── config/relayly.yaml           # Default configuration
├── migrations/001_init.sql       # SQLite schema reference
├── Dockerfile                    # Multi-stage, distroless final image
├── docker-compose.yml
└── Makefile
```

---

## CLI Reference

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

## Configuration

All options can be set in `config/relayly.yaml` or via environment variables
(`RELAYLY_<KEY>`, e.g. `RELAYLY_PORT=9090`):

| Key | Default | Description |
|---|---|---|
| `host` | `0.0.0.0` | Listen address |
| `port` | `8080` | Relay WebSocket port |
| `db.path` | `./data/relayly.db` | SQLite file |
| `noise.key_path` | `./data/server.noise.key` | Server Noise keypair |
| `admin.enabled` | `true` | Enable admin UI |
| `admin.host` | `127.0.0.1` | Admin bind address |
| `admin.port` | `8081` | Admin port |
| `log.level` | `info` | `debug\|info\|warn\|error` |
| `log.format` | `json` | `json\|console` |
| `tls.enabled` | `false` | Enable TLS (or use reverse proxy) |

---

## WebSocket Connection Protocol

Clients connect to:
```
ws://<host>:<port>/ws?device_id=<uuid>&token=<pair-token>
```

### Noise XX Handshake (3 messages)
```
Client → Server  [msg1: ephemeral pubkey]
Server → Client  [msg2: encrypted server static + ephemeral]
Client → Server  [msg3: encrypted client static]
```

After handshake, all subsequent frames are **opaque encrypted binary** —
the relay never inspects them.

### E2EE Client (Go)
```go
kp, _ := noise.GenerateKeypair()
noise.SaveKeypair(kp, "~/.relayly/client.key")

c, _ := client.New(client.Options{
    ServerURL: "ws://your-server:8080/ws",
    DeviceID:  "your-device-id",
    Token:     "your-pair-token",
    Keypair:   kp,
})

ctx := context.Background()
go c.Connect(ctx)

// Send encrypted message
c.Send([]byte("hello from device A"))

// Receive decrypted message
msg := <-c.Recv()
fmt.Println(string(msg)) // → "hello from device B"
```

---

## Admin UI

Visit `http://localhost:8081` after starting the server.

- **Dashboard**: live connection count, uptime, device list
- **Devices**: full device management with one-click revoke
- Auto-refreshes every 5 seconds via HTMX

> ⚠️ The admin UI binds to `127.0.0.1` by default. Do not expose it publicly
> without authentication (reverse proxy with basic auth is recommended).

---

## Production Deployment

### Recommended: Caddy as reverse proxy

```caddy
relay.yourdomain.com {
    reverse_proxy localhost:8080
}
```

Caddy handles TLS automatically via Let's Encrypt. Relayly stays on plain HTTP
internally.

### Security checklist

- [ ] Run behind TLS (Caddy / nginx)
- [ ] Bind admin UI to `127.0.0.1` (default)
- [ ] Mount `/data` as a persistent volume (contains DB + keypair)
- [ ] Back up `/data/relayly.db` and `/data/server.noise.key`
- [ ] Rotate pair tokens by revoking + re-pairing via admin UI

---

### Testing & Verification

To verify that your relay is correctly routing encrypted messages between paired devices, you can use the built-in tester utility:

1.  **Register & Pair**: Register two devices via the CLI or Admin UI and ensure they are paired.
2.  **Run Client A**:
    ```bash
    go run cmd/relayly-tester/main.go -name "Device A" -id <ID_A> -token <TOKEN_A>
    ```
3.  **Run Client B**:
    ```bash
    go run cmd/relayly-tester/main.go -name "Device B" -id <ID_B> -token <TOKEN_B>
    ```
4.  **Send Messages**: Type a message in one terminal and it will appear in the other, fully encrypted end-to-end.

## Development

```bash
make deps     # Download dependencies
make build    # Build binary
make test     # Run tests
make vet      # Run go vet
make run      # Build + run locally
```

---

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details on our code of conduct, and the process for submitting pull requests to us.

---

## License

[MIT License](LICENSE) © NIKX Technologies B.V.
