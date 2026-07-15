# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] - sdk/go: Protocol v1

Part of the v0.5 "SDK convergence" milestone (`docs/tasks/02-sdks-and-interop.md`);
`sdk/ts`, `sdk/py`, `sdk/rust`, and the cross-language interop CI matrix land
separately, each as its own PR/entry.

**Breaking (sdk/go):**
- Wire protocol rewritten for Protocol v1: `Options` gains a required `DeviceToken`;
  `Connect` authenticates via query params (no more in-band JSON auth frame);
  encryption is device-to-device Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`, via
  `flynn/noise`) instead of per-message NaCl box. `PrivateKey.Encrypt`/`Decrypt` are
  removed (encryption is now a stateful session, not a per-message call).
- New peer key pinning (`docs/PROTOCOL.md` §7.1): persisted to `~/.relayly/peers.json`
  by default (`Options.PeerStorePath` to override), shared schema with every other
  official SDK. A peer presenting a different key than its pin hard-fails with the new
  `ErrPeerKeyMismatch`.
- `Send` returns the new `ErrNotReady` if a peer's Noise session isn't up yet (only
  expected right after a reconnect forces a re-handshake); `RequestPairCode`/
  `AcceptPair`/`PairCode.Wait` block until the handshake actually completes, so the
  existing "pair then Send" flow is otherwise unchanged.
- New `Options.OnReady` and `Options.OnPeerStatus` callbacks.
- `Message.Timestamp` is now local receipt time, not server-assigned (the new binary
  E2E envelope carries no timestamp field).
- Device key files are unaffected (same 32-byte X25519 base64 format); peers must
  re-pair after upgrading, since the pairing/session state itself doesn't carry over.

## [0.4.0] - 2026-07-15

**Breaking:** the relay's wire protocol changed. See
[RFC-000](docs/rfc/000-protocol-reconciliation.md) and the normative
[`docs/PROTOCOL.md`](docs/PROTOCOL.md) for the full story: the server, the four SDKs, and
the README each spoke a different, mutually incompatible protocol, discovered while
scoping the C++ SDK, and were never interop-tested against each other. This release makes
the server a true zero-knowledge relay; the SDKs converge on the same spec next.

### Changed
- The server holds **zero cipher states**. E2E encryption (Noise XX,
  `Noise_XX_25519_ChaChaPoly_BLAKE2s`) now runs device-to-device; the relay authenticates
  devices and mediates pairing but forwards binary frames verbatim and cannot decrypt
  them, closing the gap called out in `docs/rfc/000-protocol-reconciliation.md`.
- WebSocket frames now split by type: text frames carry a JSON control channel
  (`welcome`, `announce_key`, `pair_request`/`pair_code`/`pair_accept`/`pair_complete`,
  `peer_status`, `ping`/`pong`, `error`), binary frames carry a 1-byte-prefixed E2E
  envelope (`0x01` handshake, `0x02` transport) relayed verbatim.
- New in-band pairing flow: `pair_request`/`pair_accept` with a 6-digit code, alongside
  the existing out-of-band `relayly link`/admin UI flow (both call the same
  `db.PairDevices`, neither replaces the other).
- `pair_token` column/field renamed to `device_token` everywhere: DB column,
  `Device.DeviceToken`, and the REST response field.
- `POST /api/v1/pair` renamed to `POST /api/v1/devices`; the old path is kept as a
  deprecated alias (`Deprecation: true` response header), returning the same new
  `device_token` field.
- Two-layer key locking (`docs/PROTOCOL.md` §7): each device pins its peer's static key
  on first pairing (the real security boundary), the server separately locks each
  device's *announced* key as defense in depth.

### Removed
- `internal/noise`, `pkg/client`, `cmd/relayly-tester`: all three existed only to
  exercise the old client<->server Noise handshake. `sdk/go` becomes the reference Go
  client once it implements Protocol v1.
- `noise.key_path` config and the server's own static keypair, the server has no
  long-term identity key of its own anymore.

### Known issues
- The four official SDKs (`sdk/go`, `sdk/ts`, `sdk/py`, `sdk/rust`) still speak the old
  wire format and cannot connect to this server yet. Tracked in
  `docs/tasks/02-sdks-and-interop.md`.
- `examples/go/chat` hand-rolls the old protocol directly and is currently broken
  (issue #64), pending a rewrite on `sdk/go` once that's updated.
- Existing paired devices need to re-pair after upgrading; there is no migration for
  in-flight Noise sessions (there were none persisted server-side to migrate).

## [0.3.0] - 2026-05-29

### Added
- REST API on the relay port under `/api/v1/`:
  - `POST /api/v1/pair` — register a new device and receive `device_id` + `pair_token`
  - `GET /api/v1/devices` — list all registered devices
  - `GET /api/v1/health` — server status, version, uptime, connected device count
- CORS middleware on all API endpoints (supports browser and mobile clients)
- Per-IP token-bucket rate limiter on WebSocket upgrades (10 req/min, HTTP 429 on excess)
- Pairing code TTL — `expires_at` column on `devices` table (schema migration v2), pair codes expire after 5 minutes
- API handler test suite (`internal/api/handler_test.go`) covering pair, list devices, health, CORS preflight
- Go clipboard-sync example (`examples/go/clipboard-sync/`)
- TypeScript echo client example (`examples/ts/echo/`)
- Rewritten `docs/PROTOCOL.md` matching current Noise XX + WebSocket behaviour
- **Python SDK** (`sdk/py/`) — `pip install relayly`; async-first, full feature parity with the Go SDK (connect, pair, send/receive, `load_or_generate_key`)
- **Rust SDK** (`sdk/rust/`) — `relayly = "0.3"` on crates.io; Tokio async, NaCl box via `crypto_box`, same reconnect logic and API shape as other SDKs
- **Go SDK**: automatic reconnection with exponential backoff; new `Options` fields `ReconnectDelay`, `MaxReconnectDelay`, `OnDisconnect`, `OnReconnect`
- READMEs added for all three SDKs; npm package renamed from `relayly-client` to `relayly`

### Changed
- WebSocket upgrade handler wired through rate limiter before reaching the relay hub

## [0.2.0](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.1.0...relayly-v0.2.0) (2026-05-09)


### Features

* add String method to version package ([e4fb56b](https://github.com/NIKX-Tech/relayly/commit/e4fb56bc68b94bda6bc81b7a46efad6bff221ab3))
* implement Noise XX handshake, device pairing UI, and key locking security ([60c0b6a](https://github.com/NIKX-Tech/relayly/commit/60c0b6acce6fa8dd753c5dca909f0753f4df831e))

## [Unreleased]

### Added
- Unified monorepo structure.
- Professional GitHub workflows for CI/CD.
- Release automation via GoReleaser.
- Dependabot configuration for automated dependency updates.
- Security policies and contribution guidelines.
- Support for Noise Protocol XX handshakes.
- Embedded HTMX Admin UI.
- Go and TypeScript SDKs.

### Changed
- Shifted default branch from `dev` to `main`.
- Improved CI performance with caching.

### Fixed
- Fixed various `errcheck` linting errors in tests and CLI.
- Resolved Go toolchain version mismatches in CI.
