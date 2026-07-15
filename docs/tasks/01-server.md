# Task 01 — Server: become a true zero-knowledge relay (PR 2, milestone v0.4)

Implements Protocol v1 on the server. Read `docs/PROTOCOL.md` (the spec) and
`docs/rfc/000-protocol-reconciliation.md` (why) before touching code. The spec wins over
any existing code or comment when they disagree.

## Outcome

After this PR, the server: authenticates via `device_id` + `device_token` query params
(unchanged), speaks the JSON control channel on text frames, relays binary frames
**verbatim** between paired devices, and holds **zero cipher states**. `grep -r
"CipherState" internal/relay` returns nothing.

## Changes

### Remove (this is mostly a deletion PR — enjoy it)

- `internal/relay/client.go`: fields `decCS`, `encCS`, `serverKey`; the `handshake()`
  method; the decrypt in `readPump` and encrypt in `writePump`. Binary frames go from
  socket to `hub.Route` to peer socket untouched.
- Server-side Noise session usage in `internal/relay/handler.go` (the `serverKey`
  parameter threading). The `internal/noise` package itself: keep only if the CLI still
  needs keypair generation for migration purposes; otherwise delete it and the
  `noise.key_path` config, with a CHANGELOG note. Prefer deletion.

### Keep

- HTTP-layer auth, expiry check, rate limiting, duplicate-connection eviction, hub
  routing, admin UI, WS ping/deadline handling, `max_message_bytes` read limit.

### Add

1. **Frame-type dispatch** in `readPump`: `websocket.TextMessage` → control handler;
   `websocket.BinaryMessage` → route to peer verbatim (drop silently if unpaired or
   peer offline, per spec §8).
2. **Control channel** (`internal/relay/control.go`, new): implement §5 exactly —
   `welcome` on connect (with `protocol_version:1`, current peer + announced key),
   `announce_key` handling with locking (§7.2: first announcement persisted via a new
   `devices.static_key` column; mismatch → `error{code:"key_mismatch"}` + close),
   `pair_request`/`pair_accept` → `pair_code`/`pair_complete` using the existing
   `internal/pairing` package (300 s TTL, single use, both sides notified),
   `peer_status` on peer connect/disconnect, `ping`→`pong`, `error` codes from §5.1.
3. **DB migration**: add `static_key TEXT` to devices; rename `pair_token` →
   `device_token` (column + all Go references).
4. **REST rename**: `POST /api/v1/devices` returning `device_token` (spec §2). Keep
   `POST /api/v1/pair` as a deprecated alias emitting a `Deprecation` header. Update CLI
   (`relayly pair` keeps its name/UX, calls the new endpoint).
5. **Honesty pass**: fix every comment/doc claiming the relay "never inspects" traffic so
   it is true *and* accurate about what the control channel does see (metadata: who pairs
   with whom, when, frame sizes). Update README's protocol section and sequence diagram
   to match §1 exactly.

## Tests (definition of done)

- Unit: control-channel message handling incl. every error code; key-lock mismatch path;
  pairing TTL/single-use; frame dispatch (text vs binary); verbatim relay
  (byte-identical in/out, property test with random payloads up to max size).
- Integration (Go, `internal/relay` tests): two raw-websocket test clients implementing
  the spec by hand (NOT sdk/go — it is still on the old protocol until PR 3): register via
  REST, connect, announce keys, pair via code, run a real Noise XX handshake through the
  relay using `flynn/noise`, exchange transport messages both ways, assert plaintext.
  Then: reconnect one side, assert `peer_status`, re-handshake per §6 initiator rule,
  exchange again.
- Negative: binary frame before pairing is dropped; oversized frame closes conn;
  second `announce_key` with a different key closes with `key_mismatch`; expired/used
  code → `invalid_code`/`code_expired`.

## Non-goals

Do not touch `sdk/**` (PR 3), do not add multi-peer routing or store-and-forward
(roadmap), do not change the admin UI beyond renamed fields.
