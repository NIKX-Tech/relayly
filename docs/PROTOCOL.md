# Relayly Wire Protocol

This document describes the WebSocket message protocol used between Relayly clients and the Relayly relay server.

---

## Transport

- All connections are made over **WebSocket** (ws:// or wss://)
- All frames are **UTF-8 JSON** objects with a mandatory `type` field
- The server routes messages between paired devices; it never has access to plaintext payloads

---

## Authentication

Every client must authenticate immediately after connecting.

**Client → Server**
```json
{
  "type": "auth",
  "device_id": "my-laptop",
  "public_key": "<base64-encoded X25519 public key>"
}
```

**Server → Client** (success)
```json
{
  "type": "auth_ok",
  "session_id": "<server-assigned session ID>"
}
```

**Server → Client** (failure)
```json
{
  "type": "error",
  "code": "auth_failed",
  "message": "Invalid device ID or public key"
}
```

---

## Pairing

Pairing links two devices together so they can exchange encrypted messages.

### Step 1 — Request a pair code (initiating device)

**Client → Server**
```json
{
  "type": "pair_request"
}
```

**Server → Client**
```json
{
  "type": "pair_code",
  "code": "483921",
  "expires_in": 300
}
```

The `code` is a 6-digit numeric string. The client should display this (or generate a QR code from it) for the user to share with the other device.

### Step 2 — Accept the pair code (accepting device)

**Client → Server**
```json
{
  "type": "pair_accept",
  "code": "483921"
}
```

**Server → Client** (both devices receive this)
```json
{
  "type": "pair_complete",
  "peer_id": "<device ID of the other party>",
  "peer_public_key": "<base64-encoded X25519 public key of peer>"
}
```

Once pairing is complete, both devices have each other's public key and can communicate with end-to-end encryption without further server involvement in the key exchange.

---

## Sending Messages

**Client → Server**
```json
{
  "type": "send",
  "to": "<peer device ID>",
  "payload": "<base64-encoded encrypted ciphertext>",
  "nonce": "<base64-encoded 24-byte nonce>"
}
```

- `payload` is encrypted using NaCl `box.Seal` with the recipient's public key and the sender's private key
- `nonce` is a randomly generated 24-byte nonce, unique per message

**Server → Client** (relay to recipient)
```json
{
  "type": "message",
  "from": "<sender device ID>",
  "payload": "<base64-encoded encrypted ciphertext>",
  "nonce": "<base64-encoded 24-byte nonce>",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

The server never decrypts the payload — it only routes the encrypted blob.

---

## Keepalive

**Client → Server**
```json
{ "type": "ping" }
```

**Server → Client**
```json
{ "type": "pong" }
```

Clients should send a ping every 30 seconds to keep the connection alive through proxies and firewalls.

---

## Error Codes

| Code | Meaning |
|---|---|
| `auth_failed` | Authentication rejected |
| `not_authenticated` | Action requires authentication first |
| `invalid_code` | Pair code not found or expired |
| `peer_not_found` | Target device is not connected |
| `peer_not_paired` | Target device is not paired with this device |
| `message_too_large` | Payload exceeds server limit (default 64 KiB) |
| `rate_limited` | Too many messages in a short period |
| `internal_error` | Server-side error |

---

## Encryption Details

Relayly uses **NaCl authenticated public-key box encryption**:

- **Algorithm**: X25519 key exchange + XSalsa20-Poly1305 AEAD
- **Key size**: 32 bytes (256-bit)
- **Nonce size**: 24 bytes (192-bit), randomly generated per message
- **Library (Go)**: `golang.org/x/crypto/nacl/box`
- **Library (JS)**: `tweetnacl`

The shared secret is derived from the sender's private key and the recipient's public key using X25519 Diffie-Hellman. This means neither party needs to share a secret out-of-band — only public keys are exchanged through the server during pairing.
