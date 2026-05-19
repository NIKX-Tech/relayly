# Relayly Wire Protocol

This document describes the actual protocol used between Relayly clients and the Relayly relay server, based on the server implementation.

---

## Transport

All connections are made over **WebSocket** (`ws://` or `wss://`). The server is payload-agnostic: after authentication and the Noise handshake it forwards encrypted binary frames verbatim between paired devices without inspecting content.

---

## WebSocket Connection URL

```
ws://host:port/ws?device_id=<uuid>&token=<pair-token>
```

| Parameter   | Description                                        |
|-------------|----------------------------------------------------|
| `device_id` | UUID of the device (obtained from `POST /api/v1/pair`) |
| `token`     | Pairing token returned by `POST /api/v1/pair`      |

Both parameters are **required**. Missing either returns `400 Bad Request`.

---

## Authentication (HTTP layer — before WebSocket upgrade)

Authentication happens at the HTTP level via query parameters, **before** the WebSocket upgrade is performed.

### Success

The server validates:

1. `device_id` and `token` are both present.
2. A device with the given `device_id` exists in the database.
3. The stored `pair_token` matches the supplied `token`.
4. The pairing code has not expired (`expires_at` is null or in the future).

On success the connection is upgraded to WebSocket.

### Failure responses

| HTTP Status | Body                    | Condition                        |
|-------------|-------------------------|----------------------------------|
| `400`       | `missing device_id or token` | Either query param absent   |
| `401`       | `unauthorized`          | Device not found or token mismatch |
| `401`       | `pairing code expired`  | `expires_at` is set and in the past |
| `429`       | `rate limit exceeded`   | More than 10 upgrade attempts per minute from the same IP |

---

## Noise XX Handshake (post-upgrade)

Once the WebSocket is open the server performs a **Noise Protocol XX** handshake as the **responder**. The client is the **initiator**. All three handshake messages are exchanged as WebSocket **binary** frames.

```
Client (initiator)          Server (responder)
        |                         |
        |--- msg1 (ephemeral) --->|   client sends ephemeral key
        |<-- msg2 (ephemeral+static+payload) ---|   server sends ephemeral + static key
        |--- msg3 (static+payload) -->|   client sends static key
        |                         |
        |   [transport phase]     |
        |<--- encrypted frames -->|
```

- **Algorithm suite**: Noise_XX_25519_ChaChaPoly_BLAKE2s (as configured by `github.com/flynn/noise`)
- After the handshake completes, two cipher states are derived:
  - **cs1** (initiator→responder): server uses this to decrypt frames from the client
  - **cs2** (responder→initiator): server uses this to encrypt frames sent to the client
- On the first successful handshake the server persists the client's static public key. Subsequent connections from the same device must present the same public key; a mismatch closes the connection.

If the handshake fails the server closes the WebSocket connection without sending an error frame.

---

## Transport Frames (post-handshake)

After the Noise handshake, all frames are **binary WebSocket messages** containing Noise-encrypted ciphertext.

- **Client → Server**: client encrypts with cs1 (its send cipher state); server decrypts with cs1.
- **Server → Client**: server encrypts with cs2; client decrypts with cs2.

The relay never inspects the decrypted plaintext. It decrypts only to verify the Noise MAC and then re-encrypts for the peer using the peer's cs2.

### Message routing

When the server receives a decrypted frame from device A, it looks up the device that A is paired with (stored in the `paired_with` DB column) and forwards the plaintext to that peer, encrypting it with the peer's cs2.

If the paired device is not currently connected, the frame is **silently dropped**. The client is responsible for retrying.

---

## Keepalive (WebSocket layer)

The server sends a WebSocket **ping** frame every 30 seconds (configurable via `websocket.ping_interval`). The client must respond with a **pong**. If no pong is received within the deadline (default 60 s), the connection is closed.

---

## Pairing Code Expiry

Pairing tokens are generated with a **5-minute TTL**. The `expires_at` timestamp is stored in the database. After expiry, a connection attempt with that token returns `401 pairing code expired`. There is no automatic renewal; a new device + token must be generated.

---

## REST API Endpoints

All `/api/*` responses include CORS headers:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type
```

Preflight `OPTIONS` requests return `204 No Content`.

### GET /api/v1/health

Returns server health and uptime.

**Response `200 OK`:**
```json
{
  "status": "ok",
  "version": "1.2.3",
  "uptime_seconds": 3600,
  "connected_devices": 4
}
```

### GET /api/v1/devices

Returns a JSON array of all registered devices.

**Response `200 OK`:**
```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "My Phone",
    "paired_with": "660e8400-e29b-41d4-a716-446655440001",
    "created_at": "2025-01-15T10:00:00Z",
    "last_seen": "2025-01-15T10:30:00Z"
  }
]
```

`paired_with` and `last_seen` may be `null`.

### POST /api/v1/pair

Registers a new device and returns a pairing token valid for 5 minutes.

**Request body:**
```json
{ "name": "My Phone" }
```

**Response `200 OK`:**
```json
{
  "device_id": "550e8400-e29b-41d4-a716-446655440000",
  "pair_token": "3vQB7Kx...",
  "expires_at": "2025-01-15T10:05:00Z"
}
```

Use `device_id` and `pair_token` as query parameters when opening the WebSocket connection. The token is a cryptographically random 32-byte value encoded in base58.

### GET /health

The relay server's built-in health endpoint (no CORS, no `/api/` prefix):

**Response `200 OK`:**
```json
{
  "status": "ok",
  "version": "1.2.3",
  "uptime_seconds": 3600,
  "connected_devices": 4
}
```

---

## Error Codes

| HTTP Status | Condition |
|-------------|-----------|
| `400` | Missing required query parameters or malformed request body |
| `401` | Invalid device ID, wrong token, or expired pairing code |
| `429` | Rate limit exceeded (>10 WebSocket upgrade attempts per minute per IP) |
| `500` | Internal server error (database or key generation failure) |

---

## Security Notes

- The server never stores or has persistent access to message plaintext; it only holds Noise static public keys.
- The Noise XX pattern provides **mutual authentication** and **forward secrecy**.
- Pairing tokens are single-use-by-convention (the server does not invalidate them after first use, but clients should treat them as such).
- TLS (`wss://`) is strongly recommended in production. Configure `tls.enabled`, `tls.cert`, and `tls.key` in `relayly.yaml`.
