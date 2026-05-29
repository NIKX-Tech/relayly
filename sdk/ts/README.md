# relayly

TypeScript/JavaScript SDK for [Relayly](https://github.com/NIKX-Tech/relayly) - a self-hosted, end-to-end encrypted WebSocket relay for local-first apps.

Works in **Node.js**, **browsers**, and **React Native**. Optional React hooks included.

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

const client = new RelaylyClient('wss://relay.example.com', {
  deviceId: 'my-laptop',
  keyPair,
});

client.on('message', (msg) => {
  console.log(`[${msg.from}]`, msg.payload);
});

await client.connect();
```

## Pairing

Devices pair using a short 6-digit code shared out-of-band (or via QR).

```ts
// Device A - request a code
const code = await client.requestPairCode();
console.log('Share this code:', code.shortCode);
// or display code.qrCodeUrl as a QR image

// Device B - accept the code
const peer = await client.acceptPair('483921');
```

After pairing, both sides fire the `paired` event automatically.

## Sending messages

```ts
await client.send(peer.id, 'hello!');

// Raw bytes
await client.sendBytes(peer.id, new Uint8Array([1, 2, 3]));
```

## Reconnection

The client reconnects automatically with exponential backoff:

```ts
client.on('disconnected', (reason) => console.warn('Lost connection:', reason));
client.on('reconnecting', (attempt) => console.log('Attempt', attempt));
client.on('connected', () => console.log('Back online'));
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
| `keyPair` | `KeyPair` | - | X25519 keypair. Use `generateKey()`. Required. |
| `pingIntervalMs` | `number` | `30000` | Keepalive ping interval. |
| `reconnectDelayMs` | `number` | `1000` | Initial reconnect delay. Set to `0` to disable. |
| `maxReconnectDelayMs` | `number` | `60000` | Backoff ceiling. |

## License

MIT
