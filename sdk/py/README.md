# relayly

Python SDK for [Relayly](https://github.com/NIKX-Tech/relayly) - a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

Async-first (asyncio). Encryption is device-to-device Noise XX
(`Noise_XX_25519_ChaChaPoly_BLAKE2s`) via `noiseprotocol` (see "Why noiseprotocol?"
below); the relay itself holds no key material. See
[`docs/PROTOCOL.md`](https://github.com/NIKX-Tech/relayly/blob/main/docs/PROTOCOL.md)
for the full wire spec.

## Install

```bash
pip install relayly
```

## Quick start

```python
import asyncio
import relayly

async def main():
    key = relayly.load_or_generate_key("~/.relayly/device.key")

    # device_token comes from POST /api/v1/devices
    client = await relayly.connect("wss://relay.example.com/ws", relayly.Options(
        device_id="my-laptop",
        device_token=device_token,
        private_key=key,
    ))

    async for msg in client.messages():
        print(f"[{msg.from_device}]", msg.payload.decode())

asyncio.run(main())
```

## Pairing

**v1 links exactly one peer per device.** Pairing again replaces whatever was linked
before, it doesn't add a second one alongside it. Multi-peer support is a roadmap
item (`docs/ROADMAP.md`, v0.7) — don't build for N simultaneous peers against this
version.

Devices pair using a short 6-digit code shared out-of-band (or via QR). Both
`accept_pair()` and `code.wait()` block until the Noise handshake actually completes
(not just until the code exchange), so the peer they resolve with is immediately safe
to `send()` to.

```python
# Device A - request a code
code = await client.request_pair_code()
print("Share this code:", code.short)
print("QR URL:", code.qr_code_url("wss://relay.example.com"))

peer = await code.wait()  # blocks until the other device pairs
print("Paired with", peer.id)

# Device B - accept the code
peer = await client.accept_pair("483921")
```

## Peer key pinning

Each peer's authenticated static key is pinned on first pairing and checked on every
handshake after — this pin, not the relay, is the real security boundary
(`docs/PROTOCOL.md` §7). By default it's stored at `~/.relayly/peers.json`, the same
schema every other official SDK reads/writes, so a shared machine can keep one pin
store across languages:

```python
client = await relayly.connect(url, relayly.Options(
    device_id=device_id,
    device_token=device_token,
    private_key=key,
    peer_store_path="~/.relayly/peers.json",  # this is the default
))
```

A peer presenting a different key than its pin raises `PeerKeyMismatchError` — this is
never auto-retried; unpinning is an explicit action (remove the entry from the store,
or use `relayly.PeerStore` directly).

## Sending messages

```python
await client.send(peer.id, b"hello!")
await client.send(peer.id, "hello!".encode())
```

`send()` raises `NotReadyError` if the peer's session isn't up yet — in normal use this
only happens briefly after a reconnect forces a re-handshake; use `on_ready` to know
when it recovers.

## Reconnection

The client reconnects automatically with exponential backoff, and re-runs the Noise
handshake per `docs/PROTOCOL.md` §6 (the device with the lexicographically smaller ID
re-initiates; the existing session keeps working until the replacement completes):

```python
relayly.Options(
    device_id="my-laptop",
    device_token=device_token,
    private_key=key,
    reconnect_delay=2.0,       # initial delay in seconds (default: 1.0)
    max_reconnect_delay=30.0,  # backoff ceiling (default: 60.0)
    on_disconnect=lambda err: print("disconnected:", err),
    on_reconnect=lambda: print("reconnected"),
    on_ready=lambda peer_id: print("session ready with", peer_id),
    on_peer_status=lambda peer_id, online: print(peer_id, "online:", online),
)
```

Set `reconnect_delay=-1` to disable automatic reconnection.

## Key management

```python
# Generate a fresh key
key = relayly.generate_key()

# Save and load manually
key.save_to_file("~/.relayly/device.key")
key = relayly.load_key_from_file("~/.relayly/device.key")

# Load or generate in one call (recommended)
key = relayly.load_or_generate_key("~/.relayly/device.key")
```

## Options

| Option | Type | Default | Description |
|---|---|---|---|
| `device_id` | `str` | - | Unique ID for this device. Required. |
| `device_token` | `str` | - | From `POST /api/v1/devices`. Required. |
| `private_key` | `PrivateKey` | - | X25519 private key. Required. |
| `peer_store_path` | `str` | `~/.relayly/peers.json` | Pinned peer key storage path. |
| `ping_interval` | `float` | `30.0` | Keepalive ping interval (seconds). |
| `reconnect_delay` | `float` | `1.0` | Initial reconnect delay. Set to `-1` to disable. |
| `max_reconnect_delay` | `float` | `60.0` | Backoff ceiling (seconds). |
| `on_disconnect` | `Callable` | `None` | Called with the exception when connection drops. |
| `on_reconnect` | `Callable` | `None` | Called after a successful reconnect. |
| `on_ready` | `Callable` | `None` | Called whenever a peer's session becomes usable for `send()`. |
| `on_peer_status` | `Callable` | `None` | Called on the paired peer's online/offline transitions. |

## Why noiseprotocol?

`docs/PROTOCOL.md` requires `Noise_XX_25519_ChaChaPoly_BLAKE2s`. Unlike TypeScript
(where no maintained library fit and the state machine had to be hand-written), Python
has `noiseprotocol` (PyPI, import name `noise`), which supports this exact suite by
name (`NoiseConnection.from_name(b'Noise_XX_25519_ChaChaPoly_BLAKE2s')`) and delegates
its DH/cipher/hash backends to `cryptography` — an actively maintained, audited
library. The Noise-pattern orchestration code in `noiseprotocol` itself hasn't been
released since 2020, but it's a thin, near-literal transcription of the spec sitting on
top of `cryptography`'s primitives, which is exactly where the real risk would be. This
SDK's use of it is verified byte-for-byte against `flynn/noise` (the Go implementation
already used server-side and in `sdk/go`) using fixed keys and a deterministic random
source, not just "the library claims to support it."

## Requirements

- Python 3.11+
- `websockets >= 12.0`
- `noiseprotocol >= 0.3.1`
- `cryptography >= 41.0.0`

## License

MIT
