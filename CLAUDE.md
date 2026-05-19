# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

All common tasks are in the Makefile. Run from the repo root:

```bash
make build       # compile ./relayly binary
make run         # build + start server (relay on :8080, admin on :8081)
make test        # go test -v -race ./...
make lint        # golangci-lint run ./...
make vet         # go vet ./...
make deps        # go mod download && go mod tidy
make docker-up   # start via docker compose
```

Run a single test package:
```bash
go test -v -run TestName ./internal/relay/
```

Start in dev mode (console logs, debug level):
```bash
./relayly start --dev
```

TypeScript SDK (`sdk/ts/`):
```bash
npm install
npm run build      # tsup → dist/ (ESM + CJS + .d.ts)
npm test           # vitest run
npm run typecheck  # tsc --noEmit
```

## Architecture

```
cmd/relayly/        Entry point — delegates to internal/cli
cmd/relayly-tester/ Standalone test/benchmark client binary
internal/
  cli/              Cobra commands: start, pair, link, status
  relay/            WebSocket hub + handler + per-client I/O pumps + rate limiter
  api/              REST API handlers (mounted at /api/ on relay port)
  database/         SQLite via modernc.org/sqlite (pure-Go, no CGo)
  noise/            Noise Protocol XX keypair load/create
  admin/            HTMX + Tailwind admin UI (embedded in binary, CDN Tailwind)
  config/           Viper config loading (YAML + env vars RELAYLY_*)
  pairing/          Device pairing code generation/validation
pkg/
  version/          Build-time version injection via ldflags
  client/           High-level Go client (Noise handshake + send/recv channels)
sdk/
  go/               Go client SDK (separate go.mod, go.work workspace)
  ts/               TypeScript client SDK (tweetnacl, tsup, vitest)
examples/go/        Reference implementations using the Go SDK
```

**Data flow:** `Handler` (HTTP→WS upgrade) → authenticates via token in SQLite → registers `Client` with the `Hub` → `Hub.Route()` forwards opaque encrypted frames to the paired device. The relay is payload-agnostic; all frames are end-to-end encrypted with Noise Protocol XX (X25519/ChaChaPoly).

**Connection protocol:** WebSocket endpoint is `ws://<host>/ws?device_id=<id>&token=<token>`. After upgrade, the client performs a 3-message Noise XX handshake (binary frames), then subsequent binary frames are forwarded verbatim to the paired device.

**REST API** (served on the relay port under `/api/v1/`):
- `GET  /api/v1/health` — status, version, uptime, connected device count
- `GET  /api/v1/devices` — list all registered devices
- `POST /api/v1/pair` — create a new device (`{"name": "..."}` → `{device_id, pair_token, expires_at}`)

**Rate limiting:** WebSocket upgrades are limited to 10 attempts per minute per remote IP (HTTP 429 on excess). Implemented in `internal/relay/ratelimit.go` using a token-bucket per IP with a cleanup goroutine.

**SQLite:** Single-writer, WAL mode, pure-Go driver. Schema and migrations are inlined in `internal/database/db.go` (versioned via `schema_migrations` table). Persistent state is only device records and pairings — no message storage.

**Config layering** (highest priority first): CLI flags → `RELAYLY_*` env vars → `config/relayly.local.yaml` (gitignored, for local overrides) → `config/relayly.yaml`. Key defaults: relay `:8080`, admin `127.0.0.1:8081`, DB `./data/relayly.db`, Noise key `./data/server.noise.key`.

**Admin UI:** Served on a separate port (default `127.0.0.1:8081`). Uses HTMX for live updates. Tailwind is loaded via CDN; admin UI must remain embedded in the Go binary with no separate build step.

**`pkg/client/` vs `sdk/go/`:** `pkg/client` is a reference implementation inside the main module — it uses `internal/noise` directly and is the basis for `cmd/relayly-tester`. `sdk/go` is the separately-versioned public SDK with its own `go.mod` that consumers import.

**Go workspace:** `sdk/go` has its own `go.mod` and participates in a `go.work` workspace at `go.work`. When working in `sdk/go`, use `go test ./...` from the workspace root or that directory.

**TypeScript SDK:** Uses `tweetnacl` for Noise-compatible crypto. Exports both a plain `RelaylyClient` and a React hook (`src/react.ts`).

## Architecture constraints

- **No CGo**: The SQLite driver must remain `modernc.org/sqlite` (pure-Go). Do not introduce CGo dependencies.
- **Admin UI stays embedded**: No external asset serving or separate build step for admin. Tailwind loads from CDN.
- **Server never sees plaintext**: All relay message payloads are opaque encrypted bytes. Do not add any inspection, logging, or transformation of message content.
- **No accounts or tracking**: The server stores only device IDs, names, tokens, and pair relationships — nothing else.
