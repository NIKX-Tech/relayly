# RFC-000: Protocol Reconciliation

- Status: **Accepted**
- Date: 2026-07-15
- Deciders: Relayly maintainers
- Affects: server (`internal/relay`, `internal/noise`, `internal/api`), all SDKs, `docs/PROTOCOL.md`

## Problem

As of v0.3.0, the repository contains **three divergent protocol designs** that were never
interop-tested against each other:

| Where | Auth | Encryption | Framing |
|---|---|---|---|
| README diagram (the vision) | — | Noise XX **device ↔ device**, relay sees only ciphertext | binary |
| Server (`internal/relay`) | `?device_id=&token=` query params | Noise XX **client ↔ server**; server **decrypts and re-encrypts every message** (`client.go` readPump/writePump) | binary |
| All four SDKs (go/ts/py/rust) | JSON `{"type":"auth"}` frame, no token | NaCl box (X25519 + XSalsa20-Poly1305) device ↔ device | JSON text frames |

Consequences:

1. **No SDK can connect to the real server.** The server rejects connections lacking
   `device_id`/`token` query params with HTTP 400 before the WebSocket upgrade.
2. **The server is not end-to-end encrypted**, contradicting the README, `docs/PROTOCOL.md`
   ("forwards encrypted binary frames verbatim"), and code comments ("never inspected by
   the relay"). The relay holds Noise cipher states and has plaintext access to all traffic.
3. The SDKs implement genuine E2E, but with unauthenticated connect and a wire format the
   server has never spoken.

## How it happened

Git history: the server + Noise XX + `docs/PROTOCOL.md` are the original design (initial
commit). The SDKs were developed outside the repo and merged in one commit
(`28912e4`, "migrate client SDKs into monorepo structure") without an integration test
against the running server. Python and Rust SDKs were then written by porting the Go SDK,
replicating the drift. Root cause: **no cross-component interop test in CI.**

## Decision

Adopt **Protocol v1** (normative spec: `docs/PROTOCOL.md`), which takes the best layer from
each design:

1. **Auth** (from server): HTTP-level `device_id` + token query params, provisioned via
   REST/CLI. Renamed for clarity: registration returns a `device_token`
   (was confusingly `pair_token`), endpoint becomes `POST /api/v1/devices`.
2. **Encryption** (from README vision): Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`)
   runs **between the two paired devices**, relayed by the server as opaque binary frames.
   The server holds **no cipher states** and structurally cannot read traffic. This is the
   property the project was founded on (censorship/blackout resilience, NLnet milestones).
3. **Framing** (new, decided 2026-07-15): WebSocket **text frames = JSON control channel**
   (pairing, errors, keepalive — necessarily server-mediated, protected by TLS in transit);
   **binary frames = opaque E2E data** with a 1-byte envelope (`0x01` handshake,
   `0x02` transport) relayed verbatim.
4. **Pairing** (from SDKs): in-band 6-digit codes over the control channel, no accounts —
   plus the existing REST/CLI registration for the device token. Both flows kept.
5. **Key locking** moves to two layers: client-side **peer key pinning** in every SDK
   (persist peer static key on first pairing, refuse mismatches) and server-side locking of
   each device's **announced** static public key as defense-in-depth cross-check.

## Rejected alternatives

- **Keep server design (client↔server Noise), fix SDKs to match.** Rejected: makes the
  relay a trusted decrypting hop, contradicting the zero-knowledge premise, the README,
  and the threat model (relays may run on seized/compromised community infrastructure).
- **Keep SDK design (NaCl box + JSON auth), rewrite server.** Rejected: unauthenticated
  connect invites resource abuse; XSalsa20 box lacks the handshake-based mutual auth,
  forward secrecy, and session semantics Noise XX already provides; and it discards the
  working, tested server auth layer.
- **All-binary envelope for control too.** Rejected for v1: text/binary split is trivially
  debuggable and maps cleanly onto browser WebSocket APIs.

## Consequences

- Breaking change to server and all SDKs. Acceptable: pre-1.0, no known production users,
  published packages will bump to 0.4.x with changelog notice.
- Server `internal/relay/client.go` loses its cipher states (net code deletion).
- CI gains a mandatory cross-language interop matrix; drift of this class becomes
  structurally impossible (see `docs/tasks/02-sdks-and-interop.md`).
- The C++ SDK (`docs/tasks/03-cpp-sdk.md`) is blocked until Protocol v1 lands, then implements
  the spec — not "whatever sdk/go sends."

## Timeline of record (retroactive milestones)

- **v0.1 — Core relay**: WS relay, server-side Noise XX, SQLite, CLI, admin UI, key locking.
- **v0.2 — Hardening + API**: pairing expiry, rate limiting, REST API, first PROTOCOL.md.
- **v0.3 — SDK expansion**: Go/TS migrated in (PR #28 lineage), Python + Rust added,
  publish workflows. ← protocol drift entered here, undetected for lack of interop CI.
- **v0.4 — Protocol v1**: this RFC + spec + server fix.
- **v0.5 — SDK convergence + interop CI.**
- **v0.6 — C++ SDK.**
- See `docs/ROADMAP.md` for v0.7 → v1.0.
