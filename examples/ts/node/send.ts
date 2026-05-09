/**
 * Node.js example: connect, accept a pair code, send a message, listen for replies.
 *
 * Usage:
 *   npx tsx examples/node/send.ts wss://relay.example.com 483921 "Hello from Node!"
 *
 * Environment variables:
 *   RELAYLY_SERVER=wss://...
 *   RELAYLY_PAIR_CODE=483921
 */

import { RelaylyClient, generateKey, encodeBase64, keyPairFromPrivateKey } from '../../src/index';
import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { loadConfig } from './config';

// ─── Persistent key ─────────────────────────────────────────────────────────

function loadOrGenerateKey(keyFile: string) {
  if (existsSync(keyFile)) {
    const b64 = readFileSync(keyFile, 'utf-8').trim();
    return keyPairFromPrivateKey(b64);
  }
  const kp = generateKey();
  const dir = dirname(keyFile);
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }
  writeFileSync(keyFile, encodeBase64(kp.privateKey), { mode: 0o600 });
  console.log(`🔑 Generated new key, saved to ${keyFile}`);
  return kp;
}

// ─── Main ────────────────────────────────────────────────────────────────────

async function main() {
  const cfg = loadConfig();

  if (!cfg.serverUrl || !cfg.pairCode) {
    console.error('Usage: send.ts <server-url> <pair-code> [message]');
    console.error('Or use env vars: RELAYLY_SERVER, RELAYLY_PAIR_CODE');
    process.exit(1);
  }

  const keyPair = loadOrGenerateKey(cfg.keyPath);

  const client = new RelaylyClient(cfg.serverUrl, {
    deviceId: `node-${process.pid}`,
    keyPair,
    reconnectDelayMs: 0, // no reconnect for this one-shot example
  });

  console.log(`Connecting to ${cfg.serverUrl}…`);
  await client.connect();
  console.log('✓ Connected');

  // Accept the pair code from the other device
  console.log(`Accepting pair code ${cfg.pairCode}…`);
  const peer = await client.acceptPair(cfg.pairCode);
  console.log(`✓ Paired with: ${peer.id}`);

  // Send the message
  await client.send(peer.id, cfg.message);
  console.log(`✓ Sent: "${cfg.message}"`);

  // Wait up to 5 seconds for a reply
  console.log('Waiting for reply (5s)…');
  const reply = await Promise.race([
    new Promise<string>((resolve) => {
      client.on('message', (msg) => {
        if (msg.from === peer.id) resolve(msg.payload);
      });
    }),
    new Promise<null>((resolve) => setTimeout(() => resolve(null), 5000)),
  ]);

  if (reply) {
    console.log(`✓ Reply: "${reply}"`);
  } else {
    console.log('No reply received.');
  }

  client.disconnect();
  process.exit(0);
}

main().catch((err) => {
  console.error('Error:', err.message);
  process.exit(1);
});
