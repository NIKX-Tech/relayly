# relayly

Python SDK for [Relayly](https://github.com/NIKX-Tech/relayly) — a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

Async-first (asyncio), with full feature parity with the Go and TypeScript SDKs.

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

    client = await relayly.connect("wss://relay.example.com", relayly.Options(
        device_id="my-laptop",
        private_key=key,
    ))

    async for msg in client.messages():
        print(f"[{msg.from_device}]", msg.payload.decode())

asyncio.run(main())
```

## Pairing

```python
# Device A — request a code
code = await client.request_pair_code()
print("Share this code:", code.short)
print("QR URL:", code.qr_code_url("wss://relay.example.com"))

peer = await code.wait()  # blocks until the other device pairs
print("Paired with", peer.id)

# Device B — accept the code
peer = await client.accept_pair("483921")
```

## Sending messages

```python
await client.send(peer.id, b"hello!")
await client.send(peer.id, "hello!".encode())
```

## Reconnection

The client reconnects automatically with exponential backoff (1 s → 60 s).

```python
relayly.Options(
    device_id="my-laptop",
    private_key=key,
    reconnect_delay=2.0,       # initial delay in seconds (default: 1.0)
    max_reconnect_delay=30.0,  # backoff ceiling (default: 60.0)
    on_disconnect=lambda err: print("disconnected:", err),
    on_reconnect=lambda: print("reconnected"),
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
| `device_id` | `str` | — | Unique ID for this device. Required. |
| `private_key` | `PrivateKey` | — | X25519 private key. Required. |
| `ping_interval` | `float` | `30.0` | Keepalive ping interval (seconds). |
| `reconnect_delay` | `float` | `1.0` | Initial reconnect delay. Set to `-1` to disable. |
| `max_reconnect_delay` | `float` | `60.0` | Backoff ceiling (seconds). |
| `on_disconnect` | `Callable` | `None` | Called with the exception when connection drops. |
| `on_reconnect` | `Callable` | `None` | Called after a successful reconnect. |

## Requirements

- Python 3.11+
- `websockets >= 12.0`
- `PyNaCl >= 1.5.0`

## License

MIT
