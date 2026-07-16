/**
 * A thin CLI wrapper around sdk/ts's public API, driven by newline-delimited JSON
 * over stdin/stdout. Exists only for the interop harness (interop/harness/) to drive
 * a real RelaylyClient as a subprocess — no internal/test-only hooks, proving the
 * public API alone is enough for interop testing. Protocol matches
 * interop/clients/go/main.go exactly (see its doc comment for the full command/event
 * list).
 *
 * Node has no global WebSocket on the Node 18/20 CI matrix (only 22+), so this
 * polyfills globalThis.WebSocket with the `ws` package before importing relayly —
 * same fix, same reason, as sdk/ts/src/client.test.ts's self-pair integration test.
 */
import { WebSocket as NodeWebSocket } from 'ws';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
(globalThis as any).WebSocket = NodeWebSocket;

import * as readline from 'node:readline';
import { RelaylyClient, generateKey, encodeBase64, decodeBase64 } from 'relayly';
import { FilePeerKeyStore } from 'relayly/node';

function emit(obj: Record<string, unknown>): void {
  process.stdout.write(JSON.stringify(obj) + '\n');
}

function parseArgs(): Record<string, string> {
  const args: Record<string, string> = {};
  const argv = process.argv.slice(2);
  for (let i = 0; i < argv.length; i += 2) {
    args[argv[i]!.replace(/^--/, '')] = argv[i + 1]!;
  }
  return args;
}

async function main(): Promise<void> {
  const args = parseArgs();
  const keyPair = generateKey();

  const client = new RelaylyClient(args.server!, {
    deviceId: args['device-id']!,
    deviceToken: args['device-token']!,
    keyPair,
    peerStore: args['peer-store-path'] ? new FilePeerKeyStore(args['peer-store-path']) : undefined,
  });

  client.on('ready', (peerId) => emit({ event: 'ready_signal', peer_id: peerId }));
  client.on('peerStatus', (peerId, online) => emit({ event: 'peer_status', peer_id: peerId, online }));
  client.on('message', (msg) =>
    emit({ event: 'message', from: msg.from, payload_b64: encodeBase64(msg.rawPayload) }),
  );

  try {
    await client.connect();
  } catch (err) {
    emit({ event: 'connect_error', message: String(err) });
    process.exit(1);
  }

  emit({ event: 'ready' });

  const rl = readline.createInterface({ input: process.stdin });
  rl.on('line', (line) => {
    let cmd: Record<string, unknown>;
    try {
      cmd = JSON.parse(line);
    } catch {
      return;
    }
    handleCommand(client, cmd);
  });
}

function handleCommand(client: RelaylyClient, cmd: Record<string, unknown>): void {
  switch (cmd.cmd) {
    case 'request_pair_code':
      void (async () => {
        try {
          const code = await client.requestPairCode();
          emit({ event: 'pair_code', code: code.shortCode, expires_in: code.expiresIn });
          const peer = await client.waitForPairing();
          emit({ event: 'paired', peer_id: peer.id, peer_public_key_b64: encodeBase64(peer.publicKey) });
        } catch (err) {
          emit({ event: 'pair_error', message: String(err) });
        }
      })();
      break;

    case 'accept_pair':
      void (async () => {
        try {
          const peer = await client.acceptPair(cmd.code as string);
          emit({ event: 'paired', peer_id: peer.id, peer_public_key_b64: encodeBase64(peer.publicKey) });
        } catch (err) {
          emit({ event: 'pair_error', message: String(err) });
        }
      })();
      break;

    case 'send':
      void (async () => {
        try {
          await client.send(cmd.peer_id as string, decodeBase64(cmd.payload_b64 as string));
          emit({ event: 'sent' });
        } catch (err) {
          emit({ event: 'send_error', message: String(err) });
        }
      })();
      break;

    case 'close':
      client.disconnect();
      process.exit(0);
  }
}

main().catch((err) => {
  emit({ event: 'connect_error', message: String(err) });
  process.exit(1);
});
