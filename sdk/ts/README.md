# relayly

TypeScript/JavaScript SDK for [Relayly](https://github.com/NIKX-Tech/relayly) - a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

Works in **Node.js**, **browsers**, and **React Native**. Optional React hooks included.

Encryption is device-to-device Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`, hand-written
over the audited [@noble](https://github.com/paulmillr) primitive libraries — no
maintained Noise library fits this exact cipher suite, see the "Why hand-written Noise?"
section below); the relay itself holds no key material. See
[`docs/PROTOCOL.md`](https://github.com/NIKX-Tech/relayly/blob/main/docs/PROTOCOL.md)
for the full wire spec.

## Install

```bash
npm install relayly
```

## Quick start

```ts
import { RelaylyClient, generateKey, encodeBase64, decodeBase64 } from 'relayly';

// Generate once and persist across sessions
const keyPair = generateKey();
localStorage.setItem('relayly_key', encodeBase64(keyPair.privateKey));

// deviceToken comes from POST /api/v1/devices
const client = new RelaylyClient('wss://relay.example.com/ws', {
  deviceId: 'my-laptop',
  deviceToken,
  keyPair,
});

client.on('message', (msg) => {
  console.log(`[${msg.from}]`, msg.payload);
});

await client.connect();
```

## Pairing

**v1 links exactly one peer per device.** Pairing again replaces whatever was linked
before, it doesn't add a second one alongside it. Multi-peer support is a roadmap
item (`docs/ROADMAP.md`, v0.7) — don't build for N simultaneous peers against this
version.

Devices pair using a short 6-digit code shared out-of-band (or via QR). Both
`acceptPair()` and `waitForPairing()` block until the Noise handshake actually
completes (not just until the code exchange), so the peer they resolve with is
immediately safe to `send()` to.

```ts
// Device A - request a code
const code = await client.requestPairCode();
console.log('Share this code:', code.shortCode);
// or display code.qrCodeUrl as a QR image
const peerFromA = await client.waitForPairing();

// Device B - accept the code
const peerFromB = await client.acceptPair('483921');
```

After pairing, both sides fire the `paired` event too.

## Peer key pinning

Each peer's authenticated static key is pinned on first pairing and checked on every
handshake after — this pin, not the relay, is the real security boundary
(`docs/PROTOCOL.md` §7). The default store is in-memory (logs a warning: it won't
survive a reload/restart). Under Node.js, use the filesystem-backed store instead,
which reads/writes the same schema every other official SDK uses:

```ts
import { RelaylyClient } from 'relayly';
import { FilePeerKeyStore } from 'relayly/node';

const client = new RelaylyClient(url, {
  deviceId, deviceToken, keyPair,
  peerStore: new FilePeerKeyStore(), // defaults to ~/.relayly/peers.json
});
```

A peer presenting a different key than its pin throws `PeerKeyMismatchError` — this is
never auto-retried; unpinning is an explicit action (remove the entry from the store).

## Sending messages

```ts
await client.send(peer.id, 'hello!');

// Raw bytes
await client.send(peer.id, new Uint8Array([1, 2, 3]));
```

`send()` throws `NotReadyError` if the peer's session isn't up yet — in normal use
this only happens briefly after a reconnect forces a re-handshake, listen for the
`ready` event to know when it recovers.

## Reconnection

The client reconnects automatically with exponential backoff, and re-runs the Noise
handshake per `docs/PROTOCOL.md` §6 (the device with the lexicographically smaller ID
re-initiates; the existing session keeps working until the replacement completes):

```ts
client.on('disconnected', (reason) => console.warn('Lost connection:', reason));
client.on('reconnecting', (attempt) => console.log('Attempt', attempt));
client.on('connected', () => console.log('Back online'));
client.on('ready', (peerId) => console.log('Session ready with', peerId));
client.on('peerStatus', (peerId, online) => console.log(peerId, 'online:', online));
```

Disable it by setting `reconnectDelayMs: 0` in options.

## React hooks

```tsx
import { useRelayly, usePairing } from 'relayly/react';

function Chat({ client, peerId }: { client: RelaylyClient; peerId: string }) {
  const { messages, send } = useRelayly(client, peerId);
  const { status, shortCode, qrCodeUrl, requestCode } = usePairing(client);

  if (status === 'waiting') return <QRCode value={qrCodeUrl!} />;

  return (
    <>
      {messages.map((m) => <p key={m.timestamp.toISOString()}>{m.payload}</p>)}
      <button onClick={() => send('hello')}>Send</button>
    </>
  );
}
```

React is a peer dependency - install it separately.

## Options

| Option | Type | Default | Description |
|---|---|---|---|
| `deviceId` | `string` | - | Unique ID for this device. Required. |
| `deviceToken` | `string` | - | From `POST /api/v1/devices`. Required. |
| `keyPair` | `KeyPair` | - | X25519 keypair. Use `generateKey()`. Required. |
| `peerStore` | `PeerKeyStore` | in-memory | Pinned peer key storage. Use `FilePeerKeyStore` from `relayly/node` under Node.js. |
| `pingIntervalMs` | `number` | `30000` | Keepalive ping interval. |
| `reconnectDelayMs` | `number` | `1000` | Initial reconnect delay. Set to `0` to disable. |
| `maxReconnectDelayMs` | `number` | `60000` | Backoff ceiling. |

## Why hand-written Noise?

No maintained JS/TS library implements `Noise_XX_25519_ChaChaPoly_BLAKE2s` with a raw,
driveable handshake state. `@chainsafe/libp2p-noise` is maintained but hardcodes
`Noise_XX_25519_ChaChaPoly_SHA256` (wrong hash for this protocol) and is wired into
libp2p's own transport-upgrade interface. This SDK instead implements the (small,
spec-defined) XX state machine directly over `@noble/curves`, `@noble/ciphers`, and
`@noble/hashes` — separately published, audited, pure-TS/JS libraries, the same
primitives `@chainsafe/libp2p-noise` itself is built from. It's verified byte-for-byte
against `flynn/noise` (the Go implementation already used server-side and in `sdk/go`)
using fixed keys and a deterministic random source, not just "trust me, I read the spec."

## Key management

Device identity keys are unaffected by the Protocol v1 migration — the on-disk format
(32-byte X25519 key, base64) is unchanged, so existing saved keys remain valid. Peers
must re-pair after upgrading: the wire protocol changed (device-to-device Noise XX
replaces per-message NaCl box), so old pairing state doesn't carry over.

## License

MIT
