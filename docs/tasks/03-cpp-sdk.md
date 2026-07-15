# Task 03 — C++ SDK (PR 4, milestone v0.6)

Add `sdk/cpp`, the fifth official SDK. Requires Tasks 01 and 02 merged. This supersedes
the earlier `relayly-cpp-sdk-prompt.md`: the reference is now **`docs/PROTOCOL.md` v1**,
not sdk/go's bytes. sdk/go remains useful as a *shape* reference for API ergonomics only.

## Why

A C++20 consumer is coming (karshipta's MAVLink gateway) that must connect as a normal
relayly client: register, pair, exchange E2E frames — linking relayly like any native
dependency. Its payloads are protobuf Envelopes; to this SDK they are opaque bytes.

## Public API (idiomatic C++17+, mirror the other SDKs' shape)

- `relayly::Options`: `device_id`, `device_token`, `private_key`, optional `ping_interval`,
  `reconnect_delay`, `max_reconnect_delay`, `peer_store_path`, callbacks `on_message`,
  `on_ready`, `on_peer_status`, `on_reconnect`, `on_disconnect`, `on_error`.
- `relayly::Connect(server_url, options)` → `relayly::Client`.
- `Client::send(peer_id, span<const byte>)` — error (not silent queue) before the session
  is ready, matching the cross-SDK decision in Task 02.
- Incoming messages via `on_message` callback — **the SDK must not own the caller's main
  loop**; it plugs into an event-driven host that runs its own I/O. Run networking on an
  internal thread (or accept an executor); document threading of every callback.
- `Client::request_pair_code()` → object with `.short_code()`, `.expires_in()`, and a
  waitable completion (`std::future<Peer>` or callback), plus `Client::accept_pair(code)`.
- `relayly::GenerateKey()` / `LoadOrGenerateKey(path)` — same on-disk format as the other
  SDKs (base64 32-byte X25519, `0600`), so keys are portable across SDKs.

## Protocol conformance

Implement PROTOCOL.md exactly: query-param auth, `welcome`/version check,
`announce_key`, JSON control on text frames, `0x01`/`0x02` binary envelopes,
device-to-device `Noise_XX_25519_ChaChaPoly_BLAKE2s` with §6 initiator/rekey rules,
mandatory peer pinning + `pair_complete` cross-check per §7.

Crypto: **libsodium** for X25519/ChaCha20-Poly1305/BLAKE2 primitives via a vetted Noise
layer — evaluate `noise-c` vs implementing the (small) XX state machine over libsodium
with test vectors from the Noise spec and cross-checks against `flynn/noise` output.
Never hand-roll curve or cipher math. WebSocket: a maintained TLS-capable client lib
(evaluate IXWebSocket / Boost.Beast); justify the choice in the README.

## Packaging

CMake ≥3.20, installable target `relayly::relayly` via `find_package(relayly CONFIG)`,
clean under `FetchContent`. Pin/fetch deps in a way that works offline-ish (vendored or
FetchContent with hashes). CI job: build + tests on Linux and macOS, gcc and clang,
with sanitizers (ASan/UBSan) on at least one leg.

## Testing (definition of done)

1. Unit: envelope codec, control-message parse/serialize (tolerant of unknown fields),
   pin-store round-trip, key file compatibility (read a key written by sdk/go).
2. Noise vectors: XX transcript tests against known-good vectors.
3. **Join the interop matrix**: add `interop/clients/cpp/` shim; extend the Task 02
   workflow with cpp↔go, cpp↔ts, cpp↔py, cpp↔rust and cpp↔cpp. All legs green = done.
   Proving interop against the real server via the shared harness — no bespoke test relay.

## Docs

`sdk/cpp/README.md` matching the siblings (quick-start, threading model, dependency
rationale). Root README: add C++ to the SDK section + badge.

## Non-goals

No server changes, no protocol changes, no other-SDK changes, no MAVLink/karshipta
specifics (payloads are opaque), no Windows support in v0.6 (roadmap note if demanded).
