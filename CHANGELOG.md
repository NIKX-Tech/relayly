# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.7.2](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.7.1...relayly-v0.7.2) (2026-08-03)


### Bug Fixes

* **sdk/cpp:** stop passing WebSocket close defaults across a DLL boundary ([#108](https://github.com/NIKX-Tech/relayly/issues/108)) ([#109](https://github.com/NIKX-Tech/relayly/issues/109)) ([f9ee9c4](https://github.com/NIKX-Tech/relayly/commit/f9ee9c401e6b0c6ff1c31d7948848f7ba52d5d0a))

## [0.7.1](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.7.0...relayly-v0.7.1) (2026-07-26)


### Bug Fixes

* bump stale SDK package versions, fix stale docs ([5e1d3a8](https://github.com/NIKX-Tech/relayly/commit/5e1d3a8a811df92d16de1056b72b06f3c96abef4))

## [0.7.0](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.6.2...relayly-v0.7.0) (2026-07-26)


### Features

* **release:** give release-please a conventional commit to parse ([7bc4ed5](https://github.com/NIKX-Tech/relayly/commit/7bc4ed57fe13b2e9d6244723408bfbc825660220))

## [0.6.2](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.6.1...relayly-v0.6.2) (2026-07-23)


### Bug Fixes

* **ci:** pin npm upgrade to the 11.x line, not [@latest](https://github.com/latest) ([#89](https://github.com/NIKX-Tech/relayly/issues/89)) ([9ec9566](https://github.com/NIKX-Tech/relayly/commit/9ec9566866db71933655518513e1019d0bde32e9))

## [0.6.1](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.6.0...relayly-v0.6.1) (2026-07-23)


### Bug Fixes

* **ci:** publish sdk/ts to npm via OIDC trusted publishing, not a token ([#88](https://github.com/NIKX-Tech/relayly/issues/88)) ([76880b2](https://github.com/NIKX-Tech/relayly/commit/76880b25448415165474e2818228cafe72558d94))
* **ci:** trigger npm publish from the release tag, not a post-hoc check ([#86](https://github.com/NIKX-Tech/relayly/issues/86)) ([5436f1b](https://github.com/NIKX-Tech/relayly/commit/5436f1bedef203120cbf4e14991906e26efe08e6))

## [0.6.0](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.2.0...relayly-v0.6.0) (2026-07-20)


### ⚠ BREAKING CHANGES

* the wire protocol changed (see docs/PROTOCOL.md and docs/rfc/000-protocol-reconciliation.md). A device on the old protocol cannot talk to a device on the new one; all paired devices need to upgrade together.

### Features

* add live chat demo and fix relay transport encryption ([3246d96](https://github.com/NIKX-Tech/relayly/commit/3246d96d5f6201a193ff9e8d02f7b1df561c2842))
* add pairing expiry, rate limiting, REST API, and new examples ([e03bea5](https://github.com/NIKX-Tech/relayly/commit/e03bea52fbfa6abbeffa333f06e7df577c2b5597))
* add Python SDK and Go SDK auto-reconnect for v0.3.0 ([7f1662c](https://github.com/NIKX-Tech/relayly/commit/7f1662c97eaccf625a4159845bcff896cff2acd1))
* add Rust SDK, fix duplicate Go badge, update CHANGELOG and README for v0.3.0 ([6cfc628](https://github.com/NIKX-Tech/relayly/commit/6cfc628c697085cf4fd2840fea92c30f5ee3918e))
* fix admin favicon, add API tests, and cut v0.3.0 changelog ([263a02b](https://github.com/NIKX-Tech/relayly/commit/263a02b349531a613fc6ee22dbd82b39bd65032c))
* Protocol v1 across the server and all five official SDKs ([dbb005d](https://github.com/NIKX-Tech/relayly/commit/dbb005db14055cc6d4b3171304aceb09ae2d7f0f))
* rebrand admin UI with logo, polished layout, and page-routing fix ([94d1a47](https://github.com/NIKX-Tech/relayly/commit/94d1a479db9386d38ee6112d5be468875985e55b))
* SDK expansion — Go reconnect, Python, Rust, and TypeScript SDKs for v0.3.0 ([a8c8ce9](https://github.com/NIKX-Tech/relayly/commit/a8c8ce9bf9868f027a4224f3d5f7f8058d102f3f))


### Bug Fixes

* TypeScript examples were broken, and the SDK still said relayly-client ([#81](https://github.com/NIKX-Tech/relayly/issues/81)) ([9c7436b](https://github.com/NIKX-Tech/relayly/commit/9c7436b9dd6c0fef9bc412aa7ed525b962566689))
* update readme badges ([c636d6e](https://github.com/NIKX-Tech/relayly/commit/c636d6e7adf98d552329afa2bff50293b4fe1c7d))
* update the README tagline to match the protocol-first framing ([b84a61f](https://github.com/NIKX-Tech/relayly/commit/b84a61fd317104605f59678d02bed9e46fa446d1))
* use to_bytes() instead of as_bytes() on SecretKey in Rust SDK ([9ab78e7](https://github.com/NIKX-Tech/relayly/commit/9ab78e7298a5fbbbfe134e59575e6672188bd2a6))

## [Unreleased] - sdk/cpp: the fifth official SDK

Closes out `docs/tasks/03-cpp-sdk.md`, the last item from the original v0.6 scoping —
a C++20 SDK speaking Protocol v1 from day one, joining the cross-language interop
matrix in the same PR.

- New `sdk/cpp/` (the `relayly` CMake package, `relayly::relayly`): device-to-device
  Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`) hand-written over libsodium (X25519,
  ChaCha20-Poly1305) plus a vendored copy of the official BLAKE2 reference
  implementation (`BLAKE2/BLAKE2`) for the hash libsodium doesn't ship — verified
  byte-for-byte against the same `flynn/noise` reference vectors already used by
  `sdk/go`/`sdk/ts`/`sdk/py`/`sdk/rust`. WebSocket transport via IXWebSocket.
  Dependencies (IXWebSocket, libsodium-cmake, nlohmann/json, Catch2 for tests) are
  fetched via CMake `FetchContent`; `relayly` is built as a shared library so its
  private dependencies don't need re-exporting to consumers. See `sdk/cpp/README.md`
  for the full dependency rationale and threading model.
- Public API: `Client::Connect`/`Send`/`RequestPairCode`/`AcceptPair`/`Close`,
  `PrivateKey`/`PublicKey` (key files are byte-compatible with every other SDK's
  base64 format), `PeerStore` (same shared `~/.relayly/peers.json` schema), and
  `relayly::Error` with a typed `ErrorCode`.
- A self-pair integration test (`sdk/cpp/tests/self_pair_test.cpp`) builds and runs
  the real `cmd/relayly` server binary and drives two `Client` instances through
  register/connect/pair/send both ways — the same category of test that caught real
  wiring bugs in three of the four previous SDK PRs.
- New `interop/clients/cpp/` shim joins the interop matrix (`interop/harness/`):
  `cpp` is now one of five SDK names in the pairwise/negative-scenario loops, adding
  5 new pairs (cpp×go, cpp×ts, cpp×py, cpp×rust, cpp×cpp) to the existing 10. New
  `.github/workflows/cpp.yml` (Linux/macOS × gcc/clang, ASan/UBSan on one leg);
  `.github/workflows/interop.yml` gained a C++ toolchain step.

## [Unreleased] - interop: cross-language CI matrix

Closes out the v0.5 "SDK convergence" milestone (`docs/tasks/02-sdks-and-interop.md`):
a required CI check (`.github/workflows/interop.yml`) that proves the four official
SDKs actually interoperate with each other and the server, so the kind of drift this
whole milestone fixed can't silently reappear.

- New `interop/clients/<lang>/` — a small CLI shim per SDK (Go, TypeScript, Python,
  Rust), each using only that SDK's public API (no test-only hooks), driven by
  newline-delimited JSON over stdin/stdout.
- New `interop/harness/` (Go) drives the matrix: builds and starts the real server,
  launches shim pairs through an in-process WebSocket proxy, and runs the full
  register/pair/roundtrip/reconnect flow across all 10 SDK pairs (4 self + 6 cross),
  plus three negative scenarios per SDK (wrong pinned key, server-announced-key
  rewrite, mid-session rekey safety) each run once in the receiving/victim role. Run
  locally with `cd interop/harness && go run .`.
- `internal/relay/ratelimit.go`'s per-IP WebSocket upgrade limit (10/minute) is now
  overridable via `RELAYLY_WS_RATE_LIMIT_MAX`/`RELAYLY_WS_RATE_LIMIT_WINDOW_SECONDS`
  (falls back to the existing 10/minute default if unset) — found because the
  interop harness drives dozens of connections from one IP (127.0.0.1) well within a
  minute, tripping the limit that was previously untunable.

## [Unreleased] - sdk/rust: Protocol v1

Part of the v0.5 "SDK convergence" milestone (`docs/tasks/02-sdks-and-interop.md`);
this closes it out for the four official SDKs — the cross-language interop CI matrix
lands separately.

**Breaking (sdk/rust, the `relayly` crate):**
- Wire protocol rewritten for Protocol v1: `Options` gains a required `device_token`;
  `connect()` authenticates via query params (no more in-band JSON auth frame);
  encryption is device-to-device Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`)
  instead of per-message NaCl box. Replaces the `crypto_box` dependency with `snow`
  (over `x25519-dalek` for key management) — `snow` is actively maintained and
  supports this exact suite by name; verified byte-for-byte against `flynn/noise` (see
  sdk/rust/README.md's "Why snow?").
- `PrivateKey::encrypt`/`decrypt` are removed (encryption is now a stateful Noise
  session, not a per-message call).
- New peer key pinning (`docs/PROTOCOL.md` §7.1): `Options.peer_store_path` (defaults
  to `~/.relayly/peers.json`, the same schema shared with every other official SDK). A
  peer presenting a different key than its pin fails with the new
  `Error::PeerKeyMismatch`.
- `send()` returns the new `Error::NotReady` if a peer's Noise session isn't up yet
  (only expected right after a reconnect forces a re-handshake); `request_pair_code()`/
  `accept_pair()`/`PairCode::wait()` block until the handshake actually completes, so
  the existing pairing flow is otherwise unchanged.
- New `Options.on_ready` and `Options.on_peer_status` callbacks.
- `Message.timestamp` is now local receipt time, not server-assigned (the new binary
  E2E envelope carries no timestamp field), matching the other three SDKs.
- New `.github/workflows/rust.yml` runs `cargo test` and `cargo clippy -D warnings`
  (including a self-pair integration test against the real compiled server) — sdk/rust
  had no CI test job before this.

## [Unreleased] - sdk/py: Protocol v1

Part of the v0.5 "SDK convergence" milestone (`docs/tasks/02-sdks-and-interop.md`);
`sdk/rust` and the cross-language interop CI matrix land separately, each as its own
PR/entry.

**Breaking (sdk/py, the `relayly` PyPI package):**
- Wire protocol rewritten for Protocol v1: `Options` gains a required `device_token`;
  `connect()` authenticates via query params (no more in-band JSON auth frame);
  encryption is device-to-device Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`) instead
  of per-message NaCl box. Replaces the `PyNaCl` dependency with `noiseprotocol` (over
  `cryptography`'s X25519/ChaCha20-Poly1305) — the only Python Noise library matching
  this exact suite; verified byte-for-byte against `flynn/noise` (see sdk/py/README.md's
  "Why noiseprotocol?").
- `_crypto.py`'s `PrivateKey.encrypt`/`decrypt` are removed (encryption is now a
  stateful Noise session, not a per-message call).
- New peer key pinning (`docs/PROTOCOL.md` §7.1): `Options.peer_store_path` (defaults
  to `~/.relayly/peers.json`, the same schema shared with every other official SDK). A
  peer presenting a different key than its pin raises the new `PeerKeyMismatchError`.
- `send()` raises the new `NotReadyError` if a peer's Noise session isn't up yet (only
  expected right after a reconnect forces a re-handshake); `request_pair_code()`/
  `accept_pair()`/`PairCode.wait()` block until the handshake actually completes, so
  the existing pairing flow is otherwise unchanged.
- New `Options.on_ready` and `Options.on_peer_status` callbacks.
- `Message.timestamp` is now local receipt time, not server-assigned (the new binary
  E2E envelope carries no timestamp field).
- New `.github/workflows/py.yml` runs the test suite (including a self-pair
  integration test against the real compiled server) on Python 3.11/3.12/3.13 — sdk/py
  had no CI test job before this.

## [Unreleased] - fix: WebSocket ping/deadline config decoded as nanoseconds

`config/relayly.yaml` shipped `websocket.ping_interval`/`websocket.deadline` as bare
integers (`30`, `60`), which decode straight into the `time.Duration` fields as
nanoseconds, not seconds. Any deployment that actually loads this file (running the
binary from the repo root, Docker, systemd with a matching `WorkingDirectory`) got an
effective ~60ns read deadline, silently dropping every WebSocket connection right after
the upgrade, before `welcome` could be written, with no warning logged (a deadline
timeout isn't an "unexpected close"). Fixed by quoting the values as duration strings
(`"30s"`, `"60s")`; added a regression test (`internal/config/config_test.go`) that
loads the real shipped file and asserts both durations parse to at least a second.
Found while debugging why `sdk/ts`'s self-pair integration test hung against a
manually-started server using this config.

## [Unreleased] - sdk/ts: Protocol v1

Part of the v0.5 "SDK convergence" milestone (`docs/tasks/02-sdks-and-interop.md`);
`sdk/py`, `sdk/rust`, and the cross-language interop CI matrix land separately, each
as its own PR/entry.

**Breaking (sdk/ts, the `relayly` npm package):**
- Wire protocol rewritten for Protocol v1: `RelaylyClientOptions` gains a required
  `deviceToken`; the client authenticates via query params (no more in-band JSON auth
  frame); encryption is device-to-device Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`)
  instead of per-message NaCl box. Replaces the `tweetnacl`/`tweetnacl-util`
  dependencies with `@noble/curves`, `@noble/ciphers`, `@noble/hashes` — no maintained
  JS Noise library fit this exact cipher suite, so the XX state machine is
  hand-written over those primitive libraries and verified byte-for-byte against
  `flynn/noise` (see `sdk/ts/README.md`'s "Why hand-written Noise?").
- `crypto.ts`'s `encrypt`/`decrypt` are removed (encryption is now a stateful Noise
  session, not a per-message function call).
- New peer key pinning (`docs/PROTOCOL.md` §7.1): an injectable `PeerKeyStore`
  (`RelaylyClientOptions.peerStore`), in-memory by default (logs a warning: pins don't
  survive a reload/restart). A new `relayly/node` entry point exports
  `FilePeerKeyStore`, reading/writing the same `~/.relayly/peers.json` schema shared
  with every other official SDK. A peer presenting a different key than its pin
  throws the new `PeerKeyMismatchError`.
- `send()` throws the new `NotReadyError` if a peer's Noise session isn't up yet (only
  expected right after a reconnect forces a re-handshake); `acceptPair()`/
  `waitForPairing()` block until the handshake actually completes, so the existing
  pairing flow is otherwise unchanged.
- New `ready` and `peerStatus` client events.
- `RelayMessage.timestamp` is now local receipt time, not server-assigned (the new
  binary E2E envelope carries no timestamp field).
- The `relayly/react` entry point is now actually built and published (`exports`/
  build script previously only covered the main entry, despite the hooks existing in
  source) — fixed as part of adding the parallel `relayly/node` entry.

## [Unreleased] - sdk/go: Protocol v1

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
