# Chat Demo

> Two devices. One setup command. Fully encrypted. Live in under 5 minutes.

**Device A** → `[Noise-encrypted frame]` → **Relayly relay** → `[Noise-encrypted frame]` → **Device B**

> The relay only ever forwards opaque bytes — it never sees your plaintext.

---

## Prerequisites

- **Go** ≥ 1.24
- **Docker** with Compose

---

## Quickstart

### Step 1 — Start the relay server

From the **repository root**:

```bash
docker compose up --build -d
```

### Step 2 — Register the two devices

```bash
cd examples/go/chat
chmod +x setup.sh && ./setup.sh
```

The script registers `chat-device-a` and `chat-device-b` inside the running
container, links them together, and prints the exact commands for both devices.

### Step 3 — Open two terminals

**Important:** You must run these commands from the `examples/go/chat` directory. Paste the commands printed by `setup.sh` into two separate terminals.

#### **Terminal 1 · Device A**
```bash
# Make sure you are in examples/go/chat
go run . --role=a --device-id=<ID_A> --token=<TOKEN_A>
```

#### **Terminal 2 · Device B**
```bash
# Make sure you are in examples/go/chat
go run . --role=b --device-id=<ID_B> --token=<TOKEN_B>
```

Both devices connect, complete the Noise XX handshake, and drop into the chat
prompt:

```
🔌  Connecting to ws://localhost:8080 …
✅  Connected.
🔐  Noise XX handshake complete — transport is encrypted.

💬  Chat is live! Type a message and press Enter.
    Type /quit to exit.
────────────────────────────────────────────────
>
```

### Step 4 — Chat

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
| `--device-id` | *(required)* | Device ID printed by `setup.sh` |
| `--token` | *(required)* | Token printed by `setup.sh` |
| `--server` | `ws://localhost:8080` | Relay server WebSocket URL |
| `--role` | device ID | Label shown in the prompt |

---

## How the encryption works

The relay server **never sees plaintext** — here's why:

1. **Registration** — `setup.sh` calls `relayly pair` inside the container to
   create each device in the DB with a unique token. Only the token owner can
   connect as that device.

2. **Noise XX handshake** — after the WebSocket connection is established, the
   client performs a three-message
   [Noise XX](https://noiseprotocol.org/noise.html#handshake-patterns)
   exchange with the server
   (`Noise_XX_25519_ChaChaPoly_BLAKE2s`). Both sides authenticate each
   other's static public key. No secrets cross the wire in cleartext.

3. **Encrypted transport** — every message is encrypted with the Noise
   transport cipher state before it's written to the WebSocket. The relay
   server acts as a dumb forwarder: it reads the ciphertext frame and
   delivers it to the linked device. It has no key material to decrypt it.

4. **Blind relay** — even a fully compromised server cannot read your
   messages because it never held the symmetric session keys.

> **Demo note:** Each run generates a fresh ephemeral Noise keypair. A real
> application would persist the device's static keypair across restarts.
