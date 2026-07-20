# Chat Demo

Two devices. No setup script. Fully encrypted. Live in under a minute.

**Device A** → `[Noise-encrypted frame]` → **Relayly relay** → `[Noise-encrypted frame]` → **Device B**

> The relay only ever forwards opaque bytes, it never sees your plaintext.

---

## Prerequisites

- **Go** ≥ 1.24
- **Docker** with Compose

---

## Quickstart

### Step 1: start the relay server

From the **repository root**:

```bash
docker compose up --build -d
```

### Step 2: open two terminals, both in `examples/go/chat`

#### **Terminal 1**

```bash
go run .
```

This registers a device (saving its credentials to `~/.relayly/chat-device.json` for
next time), connects, and requests a pairing code:

```
╔════════════════════════════════════╗
║  Pairing code:  483921              ║
╚════════════════════════════════════╝
Run in your other terminal:
  go run . --server ws://localhost:8080/ws --code 483921

Waiting for the other device …
```

#### **Terminal 2**

Paste the command Terminal 1 printed:

```bash
go run . --server ws://localhost:8080/ws --code 483921
```

Both terminals complete the Noise XX handshake and drop into the chat prompt:

```
🔌  Connecting to ws://localhost:8080/ws …
✅  Connected.
🔐  Noise XX handshake complete, paired with <peer-id>. Transport is encrypted.

💬  Chat is live! Type a message and press Enter.
    Type /quit to exit.
────────────────────────────────────────────────
>
```

### Step 3: chat

Type in either window and press **Enter**:

```
> Hello from A! 👋
> [peer → you] Hey from B! 🔐
>
```

Type `/quit` or press `Ctrl-C` to exit.

---

## Flags

| Flag | Default | Description |
|---|---|---|
| `--server` | `ws://localhost:8080/ws` | Relay server WebSocket URL |
| `--code` | *(none)* | Pairing code from the other terminal. Omit to generate a new one instead. |

---

## How the encryption works

The relay server **never sees plaintext**, here's why:

1. **Registration**: the first run of `go run .` calls `POST /api/v1/devices` to
   register itself and saves the returned credentials to
   `~/.relayly/chat-device.json`, reusing them on later runs.

2. **Pairing**: the two devices exchange a short-lived 6-digit code through the
   relay (`RequestPairCode`/`AcceptPair`), then run a three-message
   [Noise XX](https://noiseprotocol.org/noise.html#handshake-patterns) handshake
   directly with each other (`Noise_XX_25519_ChaChaPoly_BLAKE2s`). The relay
   forwards the handshake bytes but is not a party to it.

3. **Encrypted transport**: every message is encrypted with the Noise transport
   cipher before `sdk/go` writes it to the WebSocket. The relay server acts as a
   dumb forwarder: it reads the ciphertext frame and delivers it to the linked
   device. It has no key material to decrypt it.

4. **Blind relay**: even a fully compromised server cannot read your messages
   because it never held the session keys.

> **Demo note:** each device's identity key persists at `~/.relayly/chat-device.key`
> across restarts, matching every other official SDK's default key-file location.
