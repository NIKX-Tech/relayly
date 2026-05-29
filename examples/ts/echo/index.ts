/**
 * echo — TypeScript Node.js example for Relayly
 *
 * Connects to a Relayly relay server, pairs with another device,
 * and echoes every received message back with an "Echo: " prefix.
 *
 * Usage:
 *   # Install deps first (from this directory):
 *   npm install
 *   npx tsx index.ts
 *
 *   # To accept a code from another device:
 *   npx tsx index.ts --code <pair-code>
 *
 * Environment variables:
 *   RELAYLY_URL   — relay server URL (default: ws://localhost:8080)
 *
 * The device key is stored at ~/.relayly/echo.key and reused across runs.
 */

import { RelaylyClient, generateKey, encodeBase64, keyPairFromPrivateKey } from 'relayly-client';
import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join, dirname } from 'node:path';

// ─── Config ──────────────────────────────────────────────────────────────────

const SERVER_URL = process.env['RELAYLY_URL'] ?? 'ws://localhost:8080';
const KEY_PATH = join(homedir(), '.relayly', 'echo.key');

// Parse --code <value> from argv
function parseCode(): string | undefined {
  const idx = process.argv.indexOf('--code');
  return idx !== -1 ? process.argv[idx + 1] : undefined;
}

// ─── Key management ──────────────────────────────────────────────────────────

function loadOrGenerateKey(keyFile: string) {
  if (existsSync(keyFile)) {
    const b64 = readFileSync(keyFile, 'utf-8').trim();
    return keyPairFromPrivateKey(b64);
  }
  const kp = generateKey();
  const dir = dirname(keyFile);
  if (!existsSync(dir)) mkdirSync(dir, { recursive: true, mode: 0o700 });
  writeFileSync(keyFile, encodeBase64(kp.privateKey), { mode: 0o600 });
  console.log(`Generated new key, saved to ${keyFile}`);
  return kp;
}

// ─── Main ────────────────────────────────────────────────────────────────────

async function main(): Promise<void> {
  const pairCode = parseCode();
  const keyPair = loadOrGenerateKey(KEY_PATH);

  const deviceId = `echo-node-${process.pid}`;
  const client = new RelaylyClient(SERVER_URL, { deviceId, keyPair });

  console.log(`Connecting to ${SERVER_URL} as ${deviceId}…`);
  await client.connect();
  console.log('Connected.');

  let peerId: string;

  if (pairCode) {
    // Accepting side: use the code the other device printed.
    console.log(`Accepting pair code: ${pairCode}`);
    const peer = await client.acceptPair(pairCode);
    peerId = peer.id;
    console.log(`Paired with ${peerId}`);
  } else {
    // Initiating side: request a fresh pair code.
    const code = await client.requestPairCode();
    console.log('');
    console.log('  Share this code with your other device:');
    console.log(`  ${code.shortCode}`);
    console.log('');
    console.log(`  Or run: npx tsx index.ts --code ${code.shortCode}`);
    console.log('');
    console.log('Waiting for peer to connect…');

    const peer = await client.waitForPairing();
    peerId = peer.id;
    console.log(`Paired with ${peerId}`);
  }

  console.log('Echo mode active. Ctrl+C to quit.\n');

  // Echo every incoming message back with "Echo: " prefix.
  client.on('message', async (msg) => {
    console.log(`[${msg.from}] ${msg.payload}`);
    const reply = `Echo: ${msg.payload}`;
    try {
      await client.send(msg.from, reply);
      console.log(`[echo] ${reply}`);
    } catch (err) {
      console.error('send error:', err);
    }
  });

  client.on('disconnected', (reason) => {
    console.log(`Disconnected: ${reason}`);
  });

  client.on('error', (err) => {
    console.error(`Server error [${err.code}]: ${err.message}`);
  });

  // Keep the process alive until interrupted.
  await new Promise<void>((resolve) => {
    process.on('SIGINT', () => { resolve(); });
    process.on('SIGTERM', () => { resolve(); });
  });

  console.log('\nShutting down.');
  client.disconnect();
}

main().catch((err: unknown) => {
  console.error('Fatal:', err instanceof Error ? err.message : err);
  process.exit(1);
});
