# Task 02 — SDK convergence + interop CI (PR 3, milestone v0.5)

Rewrite the wire layer of all four SDKs (`sdk/go`, `sdk/ts`, `sdk/py`, `sdk/rust`) to
implement `docs/PROTOCOL.md` v1, and add the CI interop matrix that makes drift
impossible again. Requires Task 01 merged.

## Ground rules

- The spec is the contract. Do not port behavior from one SDK to another; implement each
  from the spec. Where the spec is ambiguous, stop and flag it — fix the spec, not the code.
- **Public APIs largely survive.** `Connect`, `Send`, `Messages`/message callback,
  `RequestPairCode` + `Wait`, `AcceptPair`, `GenerateKey`/`LoadOrGenerateKey`,
  `Options` keep their shapes. What changes is underneath, plus:
  - `Options` gains required `DeviceToken` (string).
  - New persisted state: pinned peer key (§7.1). Default location alongside the device
    key (e.g. `~/.relayly/peers.json`); overridable via `Options.PeerStorePath` or a
    storage callback (TS/browser: injectable async store, in-memory default with a
    loud documented warning).
  - `Send` before a completed handshake queues (bounded) or errors — pick ONE behavior,
    the same in all four SDKs, and document it. Recommendation: error `ErrNotReady`,
    plus an `OnReady`/awaitable signal when the session is up.

## Per-SDK wire changes (identical logic, idiomatic form)

1. Connect: append `?device_id&token` to the URL; drop the JSON auth frame; consume
   `welcome`, check `protocol_version == 1`; send `announce_key`.
2. Replace the NaCl box layer with **device-to-device Noise XX**
   (`Noise_XX_25519_ChaChaPoly_BLAKE2s`), envelope `0x01`/`0x02`, initiator rules and
   rekey-on-reconnect per §6, pinning + `pair_complete` cross-check per §7.
   Libraries: Go `flynn/noise` (already a server dep) · TS a maintained pure-TS/wasm noise
   impl (evaluate; must run in browser + Node) · Python `dissononce` or `noiseprotocol`
   (evaluate maintenance) · Rust `snow`. Never hand-roll primitives.
3. Control channel: JSON on text frames only; tolerate unknown types/fields; map `error`
   codes to typed SDK errors; surface `peer_status` via a callback/event.
4. Reconnect logic (keep existing backoff behavior) + re-handshake per §6.
5. Delete dead code from the old protocol (`msgTypeAuth`, `send` JSON frames, nonce
   fields, NaCl deps). Keys on disk stay X25519 32-byte base64 — existing device key
   files remain valid; document that peers must re-pair after upgrading (pre-1.0).

## Interop matrix (the actual point of this PR)

New `interop/` directory + `.github/workflows/interop.yml`, required check on PRs
touching `sdk/**`, `internal/**`, or `docs/PROTOCOL.md`.

Harness (compose or script): build server from the PR's source; for each pair in
{go↔ts, go↔py, go↔rust, ts↔py, ts↔rust, py↔rust} plus each SDK against itself:

1. `POST /api/v1/devices` twice; start client A (SDK 1) and client B (SDK 2).
2. A `RequestPairCode`; harness passes the code to B; B `AcceptPair`.
3. Assert both sides report the pairing; handshake completes; pinned keys match
   `pair_complete` keys.
4. A→B and B→A: send 3 payloads each (1 B, ~1 KiB, max-size-minus-envelope); assert
   byte-exact plaintext round-trip.
5. Kill B's connection; assert A gets `peer_status offline` then `online`; assert
   re-handshake; exchange one more message each way.
6. Negative: wrong pinned key injected into B's store → next handshake must hard-fail
   (§7.1, layer 1).
7. Negative: harness rewrites `peer_static_key` in the `pair_complete` delivered to B so it
   no longer matches what A's handshake actually authenticates → B's mandatory §7.2
   cross-check must hard-fail (layer 2 has its own test, not just layer 1's).
8. Negative: harness injects an unsolicited `0x01` msg1 into an already-paired, healthy
   session → the receiving SDK must NOT tear down its working session unless the injected
   handshake actually completes (§6 make-before-break); a normal message must still round
   trip on the existing session afterward.

Each SDK exposes a tiny CLI shim in `interop/clients/<lang>/` (register/connect/pair/
send/recv over stdio or files) so the harness stays language-agnostic. Emit a summary
table in the job output. Total runtime target: under 5 minutes.

## Also

- Update each `sdk/*/README.md` quick-start (token in Connect, pinning note).
- Root README: fix SDK snippets; add interop badge.
- CHANGELOG: breaking-change entry for 0.5.0 across all packages.

## Non-goals

No C++ (Task 03). No API redesigns beyond what the spec forces. No group messaging.
