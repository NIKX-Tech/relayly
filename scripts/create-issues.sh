#!/usr/bin/env bash
# Creates Relayly milestones + issues via gh CLI.
# Usage: GH_REPO=NIKX-Tech/relayly ./create-issues.sh
# Requires: gh auth login (repo scope). Idempotency: NOT guaranteed — run once.
set -euo pipefail
REPO="${GH_REPO:-NIKX-Tech/relayly}"

ms() { # ms <title> <description> -> prints milestone number
  gh api "repos/$REPO/milestones" -f title="$1" -f description="$2" --jq .number
}
close_ms() { gh api -X PATCH "repos/$REPO/milestones/$1" -f state=closed >/dev/null; }

echo "== Retroactive milestones (created closed, for the record) =="
M01=$(ms "v0.1 — Core relay" "WS relay, server-side Noise XX, SQLite, CLI, admin UI, key locking. Founding implementation.")
close_ms "$M01"
M02=$(ms "v0.2 — Hardening + API" "Pairing expiry, rate limiting, REST API, chat demo, first PROTOCOL.md.")
close_ms "$M02"
M03=$(ms "v0.3 — SDK expansion" "Go/TS SDKs into monorepo (see PR #28), Python + Rust SDKs, publish workflows, auto-reconnect. Post-mortem: protocol drift between SDKs and server entered here — no interop CI. See docs/rfc/000-protocol-reconciliation.md.")
close_ms "$M03"

echo "== Active milestones =="
M04=$(ms "v0.4 — Protocol v1" "RFC-000 + normative spec; server becomes a true zero-knowledge relay. tasks/01-server.md. Ships as 2 PRs.")
M05=$(ms "v0.5 — SDK convergence + interop CI" "All 4 SDKs implement PROTOCOL.md v1; cross-language interop matrix required in CI. tasks/02-sdks-and-interop.md. 1 PR.")
M06=$(ms "v0.6 — C++ SDK" "sdk/cpp per tasks/03-cpp-sdk.md; joins interop matrix. 1 PR.")

issue() { gh issue create --repo "$REPO" --title "$1" --milestone "$2" --label "$3" --body "$4" >/dev/null && echo "  ✓ $1"; }

echo "== v0.4 issues (PR 1: spec · PR 2: server) =="
issue "RFC-000: protocol reconciliation decision record" "v0.4 — Protocol v1" "docs" \
"Land docs/rfc/000-protocol-reconciliation.md: the three divergent designs (README vision / server / SDKs), root cause (SDKs merged in 28912e4 without interop tests), the decision (E2E device-to-device Noise XX over an opaque relay), and rejected alternatives. Ships in PR 1."
issue "PROTOCOL.md v1: normative wire spec" "v0.4 — Protocol v1" "docs" \
"Rewrite docs/PROTOCOL.md as the normative contract: device_token auth (query params), text=JSON control / binary=E2E envelope framing, Noise_XX_25519_ChaChaPoly_BLAKE2s device-to-device, pairing control flow, two-layer key locking, versioning via welcome.protocol_version. Ships in PR 1."
issue "Docs honesty pass: relay currently CAN read traffic" "v0.4 — Protocol v1" "docs" \
"internal/relay/client.go decrypts (readPump) and re-encrypts (writePump) every message; README/PROTOCOL.md/comments claiming 'never inspected' are false until the server fix lands. Correct the claims in PR 1 so the repo never misstates its security properties; PR 2 makes the original claim true."
issue "Server: remove cipher states — relay binary frames verbatim" "v0.4 — Protocol v1" "server" \
"Delete decCS/encCS/serverKey and handshake() from internal/relay/client.go; binary frames route untouched. Acceptance: grep -r CipherState internal/relay is empty; property test asserts byte-identical relay. tasks/01-server.md. PR 2."
issue "Server: JSON control channel (welcome, announce_key, pairing, peer_status)" "v0.4 — Protocol v1" "server" \
"Implement PROTOCOL.md §5 on text frames: welcome w/ protocol_version, announce_key with server-side key locking (new devices.static_key column), pair_request/pair_code/pair_accept/pair_complete via internal/pairing, peer_status, ping/pong, typed error codes. PR 2."
issue "Server: rename pair_token→device_token, POST /api/v1/pair→/api/v1/devices" "v0.4 — Protocol v1" "server" \
"DB column rename + migration, REST endpoint rename with deprecated alias (Deprecation header), CLI updated (UX unchanged), admin UI field labels. PR 2."
issue "Server: spec-conformance integration test (raw WS clients)" "v0.4 — Protocol v1" "server,testing" \
"Two hand-rolled raw-websocket test clients (NOT sdk/go): register, connect, announce, pair by code, full Noise XX through the relay (flynn/noise), bidirectional transport, reconnect + peer_status + re-handshake per §6. Plus negative cases (key_mismatch, expired code, oversized frame). PR 2."

echo "== v0.5 issues (PR 3) =="
for L in "Go:flynn/noise" "TS:browser+Node capable noise lib" "Python:evaluate noiseprotocol/dissononce" "Rust:snow"; do
  LANGN="${L%%:*}"; LIB="${L#*:}"
  issue "$LANGN SDK: implement Protocol v1 (Noise XX E2E, token auth, pinning)" "v0.5 — SDK convergence + interop CI" "sdk" \
"Rewrite the wire layer per docs/PROTOCOL.md from the spec (not by porting another SDK): query-param auth + DeviceToken option, welcome/version check, announce_key, 0x01/0x02 envelopes, device-to-device Noise XX ($LIB), §6 initiator/rekey rules, §7 peer pinning + pair_complete cross-check, typed errors, ErrNotReady send semantics. Remove NaCl box + old JSON frames. tasks/02-sdks-and-interop.md."
done
issue "CI: cross-language interop matrix (required check)" "v0.5 — SDK convergence + interop CI" "testing,ci" \
"interop/ harness + .github/workflows/interop.yml: real server build; all SDK pairs incl. self-pairs; register→pair→handshake→byte-exact round-trips→reconnect/re-handshake→3 negative tests (layer-1 pinning mismatch, layer-2 spoofed pair_complete key, injected msg1 must not break an existing healthy session per §6 make-before-break). Required on PRs touching sdk/**, internal/**, docs/PROTOCOL.md. <5 min. This is the structural fix for RFC-000's root cause."

echo "== v0.6 issues (PR 4) =="
issue "C++ SDK: sdk/cpp implementing Protocol v1" "v0.6 — C++ SDK" "sdk" \
"Per tasks/03-cpp-sdk.md: C++17+ API mirroring sibling SDKs (callback-based, does not own the caller's loop), libsodium-backed Noise XX, CMake find_package(relayly CONFIG) + FetchContent, key-file compatibility with other SDKs, ASan/UBSan CI leg."
issue "Interop matrix: add C++ legs" "v0.6 — C++ SDK" "sdk,testing" \
"interop/clients/cpp shim; extend matrix with cpp↔{go,ts,py,rust,cpp}. All green = v0.6 done."

echo "== Backlog (no milestone) =="
issue "v0.7 groundwork: multi-peer routing design" "v0.7 — Multi-peer" "design" \
"N linked peers per device: pairs table schema, hub routing, per-peer Noise sessions. See docs/ROADMAP.md." 2>/dev/null || \
gh issue create --repo "$REPO" --title "v0.7 groundwork: multi-peer routing design" --label design \
  --body "N linked peers per device: pairs table schema, hub routing, per-peer Noise sessions. See docs/ROADMAP.md." >/dev/null
gh issue create --repo "$REPO" --title "Deferred: group messaging (protocol v2, MLS/sender-keys)" --label "design" \
  --body "Pairwise Noise does not extend to groups. Parked until 1:1 is audited (docs/ROADMAP.md, Beyond 1.0). Tracking issue for the social-app use case." >/dev/null && echo "  ✓ group messaging tracker"

echo "Done. Create milestones v0.7+ when v0.6 closes (see docs/ROADMAP.md)."
