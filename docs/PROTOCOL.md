# Relayly Wire Protocol — v1 (normative)

This document is the **contract**. Server and every SDK are implemented and tested against
this spec, not against each other's source code. Any behavior not specified here is
undefined; implementations MUST NOT rely on it. Changes require an RFC.

Keywords MUST / SHOULD / MAY per RFC 2119. Protocol version: `1`.

---

## 1. Roles and layers

```
Device A  ── E2E: Noise XX (binary frames, opaque) ──  Device B
    \                                                   /
     \── control: JSON over TLS ── Relay ── JSON ──────/
```

- **Relay server**: authenticates devices, mediates pairing, forwards binary frames
  verbatim between paired devices. It MUST NOT hold any key material capable of
  decrypting relayed frames.
- **Devices**: hold static X25519 keypairs; run Noise XX with each other through the relay.

## 2. Device registration (out-of-band, REST/CLI)

`POST /api/v1/devices` with `{"name": "<display name>"}` returns:

```json
{ "device_id": "<uuid>", "device_token": "<opaque>", "expires_at": "..." }
```

The CLI `relayly pair <name>` wraps this endpoint (QR output unchanged).
`device_token` is a bearer credential for connecting; it is NOT related to E2E keys.

A freshly registered, not-yet-paired device's token expires 5 minutes after
registration (checked at connection time, per §3) - this exists to garbage-collect
devices that were registered but never used, not to bound how long a real pairing may
stay connected. **A successful pairing (§5.3) clears the token's expiry on both
devices**: pairing is itself proof this is a real, intentionally-used device, and a
device meant to stay paired indefinitely (a server-side relay client, an always-on
gateway) has no other way to renew an otherwise-expiring token without minting a new
`device_id` - which would silently orphan the peer's existing key pin (§7.1) and force
a human to re-pair. An unpaired device's token still expires as before if the pairing
window is missed.

> Migration note: this endpoint replaces `POST /api/v1/pair` and the field replaces
> `pair_token`. The old endpoint MAY be kept as a deprecated alias during v0.4.x.

## 3. Connecting

```
ws(s)://<host>:<port>/ws?device_id=<uuid>&token=<device_token>
```

Auth happens at the HTTP layer before upgrade. Failure responses (unchanged from v0):
`400` missing params · `401` unknown device / bad token / expired · `429` rate limited
(>10 attempts/min/IP).

On success the server upgrades and immediately sends a `welcome` control message (§5.1).
There is NO in-band auth frame and NO client↔server cryptographic handshake.

## 4. Frame discipline

| WebSocket frame type | Meaning | Who reads it |
|---|---|---|
| **Text** | JSON control message (§5) | client ↔ server |
| **Binary** | E2E envelope (§6), relayed **verbatim** | device ↔ device only |
| Ping/Pong | transport keepalive (server pings; clients that can, pong) | — |

The server MUST NOT parse, decrypt, modify, or reorder binary frames. One WebSocket
binary frame carries exactly one envelope. Frames exceeding the server's
`max_message_bytes` (config, default 65536) are rejected by closing the connection.

## 5. Control channel (JSON text frames)

Every control message has `"type"`. Unknown types MUST be ignored (forward compat).
Unknown fields MUST be ignored.

### 5.1 Server → client

- `welcome` — `{ "type":"welcome", "protocol_version":1, "device_id":"...",
  "peers":[{"id":"...","static_key":"<b64 or empty>"}] }`
  Sent once after upgrade. `peers` lists currently linked device(s); `static_key` is the
  peer's announced key if known (§7). A client whose implemented version ≠
  `protocol_version` MUST disconnect with an error to its caller.
- `pair_code` — `{ "type":"pair_code", "code":"483921", "expires_in":300 }`
- `pair_complete` — `{ "type":"pair_complete", "code":"483921", "peer_id":"...",
  "peer_static_key":"<b64>" }` Sent to **both** devices when a code is accepted.
- `peer_status` — `{ "type":"peer_status", "peer_id":"...", "online":true|false }`
  Sent on welcome and whenever the paired peer connects/disconnects.
- `pong` — `{ "type":"pong" }`
- `error` — `{ "type":"error", "code":"<machine_code>", "message":"<human>" }`
  Error codes (initial set): `invalid_code`, `code_expired`, `already_paired`,
  `peer_offline`, `rate_limited`, `malformed`, `internal`.

### 5.2 Client → server

- `announce_key` — `{ "type":"announce_key", "static_key":"<b64 32-byte X25519 pub>" }`
  MUST be the first client control message after `welcome`. See §7 (key locking).
- `pair_request` — `{ "type":"pair_request" }` Ask for a fresh 6-digit code.
- `pair_accept` — `{ "type":"pair_accept", "code":"483921" }`
- `ping` — `{ "type":"ping" }` (for runtimes that cannot send WS pings, e.g. browsers).

### 5.3 Pairing flow

1. A sends `pair_request` → server replies `pair_code` (code TTL: 300 s, single use).
2. The code travels human-to-human / QR (out of band).
3. B sends `pair_accept {code}`.
4. Server links A↔B in the DB, sends `pair_complete` to both, including each side's
   announced static key so the peer can cross-check the handshake (§7).
5. The **accepting device (B) is the Noise initiator** and starts the handshake (§6).

v1 supports exactly **one linked peer per device** (matches current DB schema). Multi-peer
routing is roadmap v0.7.

## 6. E2E channel (binary frames)

Envelope: `[1-byte type][payload]`.

| Byte | Name | Payload |
|---|---|---|
| `0x01` | `HANDSHAKE` | one Noise XX handshake message |
| `0x02` | `TRANSPORT` | one Noise transport ciphertext |

- Suite: **`Noise_XX_25519_ChaChaPoly_BLAKE2s`**, empty prologue, no PSK, no payloads
  inside handshake messages in v1.
- Initiator: the accepting device on first pairing (§5.3). On any **reconnect** of either
  side (signaled via `peer_status online:true`), the device with the **lexicographically
  smaller `device_id`** (byte-wise comparison of the canonical lowercase UUID string) MUST
  initiate a fresh handshake. The initiator role is decided fresh per handshake event by
  these two rules; it is NOT a fixed property of a device for the life of the pairing.
- Rekey is deliberately proactive, not only reactive to AEAD failure: v1 has no
  store-and-forward, so a dropped connection can silently desync the per-direction nonce
  counters, and re-handshaking on every reconnect closes that window immediately instead of
  waiting for a garbled frame.
- **Make-before-break:** a peer MUST accept a new `0x01` msg1 at any time, but MUST NOT
  discard its current transport session until the replacement handshake completes and
  authenticates successfully. A handshake attempt that times out or otherwise fails MUST be
  abandoned, leaving the prior session (if still healthy) in place. The relay is untrusted
  (§7); implementations SHOULD rate-limit unsolicited msg1s per peer so a malicious or
  compromised relay cannot force perpetual handshake churn by injecting `0x01` frames it can
  never complete.
- One envelope = one Noise message. Noise's internal nonce counter is used as-is; frames
  arrive in order (TCP/WS). A transport frame that fails AEAD MUST cause the receiver to
  discard the session and (if it is the designated initiator) re-handshake.
- Application payloads are the plaintext of `TRANSPORT` messages. The relay and this spec
  impose no structure on them (karshipta, for example, puts protobuf Envelopes here).

## 7. Key model and locking (two layers)

1. **Client-side pinning (mandatory, the real security boundary):** every SDK MUST persist
   the peer's static public key learned from the first completed XX handshake, keyed by
   `peer_id`, and MUST hard-fail (no auto-retry, surfaced error) if a later handshake
   presents a different key. Unpinning is an explicit user action only.
2. **Server-side announced-key locking (defense in depth):** the server stores the first
   `announce_key` per device and rejects (via `error` + close) any later announcement that
   differs. `pair_complete` carries the announced keys; after the handshake each SDK
   MUST verify `PeerStatic == peer_static_key` from `pair_complete` and hard-fail on
   mismatch. This detects third-party MitM; it does not protect against a malicious
   relay, layer 1 and the out-of-band code do.

Trust bootstrapping is TOFU strengthened by the out-of-band 6-digit code. This is an
accepted v1 tradeoff; code-bound handshakes (PSK) are a roadmap item.

## 8. Keepalive, limits, ordering

- Server sends WS pings every `ping_interval` (config); connections idle past `deadline`
  are closed. Browser clients use JSON `ping`/`pong` instead.
- Server MAY drop binary frames addressed to an offline peer (no store-and-forward in v1;
  roadmap v0.8). SDKs MUST NOT assume delivery; peers learn liveness via `peer_status`.
- Base64 in JSON fields is standard alphabet **with padding**.

## 9. Versioning

`welcome.protocol_version` is the negotiation mechanism (server-declared, take it or
leave). Breaking wire changes bump it and require an RFC. SDKs expose the version they
implement.

## 10. Conformance

An implementation conforms iff it passes the cross-language interop matrix in CI
(`docs/tasks/02-sdks-and-interop.md`): pair with a peer running a *different* implementation
through a real server build and round-trip plaintext both directions.
