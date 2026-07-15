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
internal/
  cli/              Cobra commands: start, pair, link, status
  relay/            WebSocket hub + handler + per-client I/O pumps + rate limiter +
                     control channel (control.go) + pairing-code registry (paircodes.go)
  api/              REST API handlers (mounted at /api/ on relay port)
  database/         SQLite via modernc.org/sqlite (pure-Go, no CGo)
  admin/            HTMX + Tailwind admin UI (embedded in binary, CDN Tailwind)
  config/           Viper config loading (YAML + env vars RELAYLY_*)
  pairing/          Device token + in-band pairing code generation
pkg/
  version/          Build-time version injection via ldflags
sdk/
  go/               Go client SDK (separate go.mod, go.work workspace; still pre-v1
                     wire format until docs/tasks/02-sdks-and-interop.md lands)
  ts/               TypeScript client SDK (tweetnacl, tsup, vitest; same caveat)
examples/go/        Reference implementations (basic/pair-and-send/clipboard-sync use
                     sdk/go; chat/ hand-rolls the old protocol directly and is
                     currently broken, see issue #64)
```

**Data flow:** `Handler` (HTTP→WS upgrade) → authenticates via token in SQLite → registers `Client` with the `Hub`, sends `welcome` → text frames go to `handleControl` (`control.go`), binary frames go to `Hub.Route()`, which forwards them verbatim to the paired device's `Client` untouched. The server holds no key material and never decrypts binary frames; E2E is entirely device-to-device Noise XX (X25519/ChaChaPoly), per `docs/PROTOCOL.md`. `grep -r "CipherState" internal/relay` returns nothing.

**Connection protocol:** WebSocket endpoint is `ws://<host>/ws?device_id=<id>&token=<device_token>`. No in-band auth frame, no client<->server handshake: auth is the HTTP-layer query params. After upgrade the server sends `welcome`; text frames are JSON control messages, binary frames are a 1-byte-prefixed E2E envelope (`0x01` Noise handshake, `0x02` transport) relayed verbatim between the two paired devices. See `docs/PROTOCOL.md` for the full spec.

**REST API** (served on the relay port under `/api/v1/`):
- `GET  /api/v1/health` — status, version, uptime, connected device count
- `GET  /api/v1/devices` — list all registered devices
- `POST /api/v1/devices` — create a new device (`{"name": "..."}` → `{device_id, device_token, expires_at}`); `POST /api/v1/pair` is kept as a deprecated alias (same response shape, adds a `Deprecation: true` header)

**Rate limiting:** WebSocket upgrades are limited to 10 attempts per minute per remote IP (HTTP 429 on excess). Implemented in `internal/relay/ratelimit.go` using a token-bucket per IP with a cleanup goroutine.

**SQLite:** Single-writer, WAL mode, pure-Go driver. Schema and migrations are inlined in `internal/database/db.go` (versioned via `schema_migrations` table). Persistent state is only device records and pairings — no message storage.

**Config layering** (highest priority first): CLI flags → `RELAYLY_*` env vars → `config/relayly.local.yaml` (gitignored, for local overrides) → `config/relayly.yaml`. Key defaults: relay `:8080`, admin `127.0.0.1:8081`, DB `./data/relayly.db`. The server holds no static keypair of its own (removed along with `internal/noise` once E2E moved device-to-device).

**Admin UI:** Served on a separate port (default `127.0.0.1:8081`). Uses HTMX for live updates. Tailwind is loaded via CDN; admin UI must remain embedded in the Go binary with no separate build step.

**Go workspace:** `sdk/go` has its own `go.mod` and participates in a `go.work` workspace at `go.work`. When working in `sdk/go`, use `go test ./...` from the workspace root or that directory.

**TypeScript SDK:** Uses `tweetnacl` for Noise-compatible crypto. Exports both a plain `RelaylyClient` and a React hook (`src/react.ts`).

## Architecture constraints

- **No CGo**: The SQLite driver must remain `modernc.org/sqlite` (pure-Go). Do not introduce CGo dependencies.
- **Admin UI stays embedded**: No external asset serving or separate build step for admin. Tailwind loads from CDN.
- **Server never sees plaintext**: binary frames are an opaque E2E envelope the relay forwards verbatim (`internal/relay/hub.go`'s `Route`); it holds no cipher states (`docs/PROTOCOL.md`, `docs/rfc/000-protocol-reconciliation.md`). Do not add any inspection, logging, or transformation of message content.
- **No accounts or tracking**: The server stores only device IDs, names, tokens, and pair relationships — nothing else.
