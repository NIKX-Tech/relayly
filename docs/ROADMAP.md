# Relayly Roadmap

Mission anchor: communication between your own devices that survives hostile networks —
internet blackouts, censorship, untrusted infrastructure. The relay must be a dumb,
disposable, zero-knowledge pipe; all trust lives at the endpoints. Every milestone below
is judged against that sentence.

## Done (retroactive record)

- **v0.1 — Core relay** · WS relay, server-side Noise XX, SQLite, CLI, admin UI,
  public-key locking. The founding implementation.
- **v0.2 — Hardening + API** · Pairing expiry, rate limiting, REST API, chat demo,
  first PROTOCOL.md.
- **v0.3 — SDK expansion** · Go/TS SDKs into monorepo (PR #28 lineage), Python + Rust
  SDKs, publish workflows (PyPI/crates.io/npm), auto-reconnect.
  *Post-mortem: protocol drift between SDKs and server entered here undetected — no
  interop CI existed. See RFC-000.*
- **v0.4 — Protocol v1** · RFC-000 + normative PROTOCOL.md; server rewritten to a true
  zero-knowledge relay (control channel, verbatim binary relay, announced-key locking,
  `device_token` rename). → `docs/tasks/01-server.md`
- **v0.5 — SDK convergence + interop CI** · All four original SDKs (Go, TS, Python,
  Rust) rewritten to the spec (device↔device Noise XX, pinning); cross-language
  interop matrix became a required CI check. → `docs/tasks/02-sdks-and-interop.md`
- **v0.6 — C++ SDK** · `sdk/cpp` for native consumers (karshipta gateway), joins the
  interop matrix as its fifth SDK. → `docs/tasks/03-cpp-sdk.md`
- **v0.7 — C++ SDK Windows support** · `sdk/cpp` builds, links, and passes its full
  unit test suite on Windows (MSVC); unblocks karshipta gateway's own Windows build.
  Landed via an automatic release-please version bump rather than a planned milestone
  push, ahead of where "Multi-peer" was originally numbered below - renumbered that
  and everything after it up by one rather than reuse v0.7 for two unrelated things.

## The road to v1.0

- **v0.8 — Multi-peer** · Devices link to N peers, not 1: DB schema (pairs table),
  routing, per-peer Noise sessions in SDKs, `Send(peer_id, …)` already has the right
  shape. Unlocks "all my devices," the original product idea, and karshipta fleets.
- **v0.9 — Offline resilience** · (a) Store-and-forward: relay queues *ciphertext* for
  offline peers (bounded, TTL, still zero-knowledge — this is where E2E pays off, a
  hop-by-hop design could never do this honestly). (b) LAN mode: mDNS/DNS-SD discovery +
  direct device↔device WS on the local network with the same Noise layer — two phones in
  a blacked-out apartment need no relay at all. This milestone IS the Iran scenario.
- **v0.10 — Key lifecycle + protocol v1.1** · Key rotation with signed handover; explicit
  re-pair/unpin UX; pairing-code-bound handshakes (PSK from the 6-digit code) to close
  the TOFU window; protocol version negotiation exercised for real.
- **v1.0 — Trust it** · External security review of protocol + server (budgetable in an
  NLnet application — they fund audits via their partners); fuzzing (control-channel
  JSON, envelope parser, Noise state machines) in CI; threat-model document; stability
  promise: protocol v1.x frozen, SDK semver, LTS posture for the server binary.

## Beyond 1.0 (unordered)

- **Group messaging (protocol v2)** · Pairwise Noise does not scale to groups; this is
  sender-keys / MLS (RFC 9420) territory. Deliberately parked until 1:1 is audited —
  the friend's social-app use case lands here.
- **Relay federation / multi-relay failover** · SDKs accept a relay list; sessions
  survive relay death (they already can — E2E state lives in the endpoints).
- **Embedded profile** · The IoT origin: a C SDK subset for constrained devices
  (ESP32-class), possibly CoAP/raw-TCP transport under the same Noise layer.
- **Transports beyond WS** · Bluetooth LE / Wi-Fi Direct bridging for true
  infrastructure-free hops.
- **karshipta integration reference** · Official example: gateway ↔ console over relayly
  with protobuf payloads.

## Grant narrative (NLnet)

v0.4–v0.6 demonstrate exactly what funders want to see: a discovered foundational flaw,
a public RFC, a spec-first fix, and structural prevention (interop CI). v0.8 delivers the
blackout use case; v1.0's audit is a classic NLnet-fundable milestone. Keep RFC-000 and
this file linkable from the application.
