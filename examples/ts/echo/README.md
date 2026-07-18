# Relayly Echo Example (TypeScript / Node.js)

A minimal Node.js example that connects to a Relayly relay server, pairs with another device, and echoes every received message back with an `"Echo: "` prefix.

## Prerequisites

- Node.js 20.19+ (matches sdk/ts's own engine requirement)
- A running Relayly server (see the main repo README)

## Install

```sh
npm install
```

## Usage

### Device A: generate a pairing code

```sh
npx tsx index.ts
```

The code is printed to stdout. Share it with Device B.

### Device B: accept the code

```sh
npx tsx index.ts --code <code-from-device-a>
```

Once paired, Device B will echo every message it receives back to Device A (with `"Echo: "` prepended).

### Point to a different server

Set the `RELAYLY_URL` environment variable:

```sh
RELAYLY_URL=wss://relay.example.com/ws npx tsx index.ts
```

## Device identity

Each run registers itself via `POST /api/v1/devices` on first run and saves the returned credentials to `~/.relayly/echo-device.json`, reusing them on later runs. The device's private key is generated on first run and saved to `~/.relayly/echo.key` (mode 0600). Both persist across runs so the device identity is stable.
