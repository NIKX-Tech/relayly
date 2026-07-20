/**
 * Node.js example: connect, accept a pair code, send a message, listen for replies.
 *
 * Usage:
 *   npx tsx send.ts ws://localhost:8080/ws 483921 "Hello from Node!"
 *
 * Environment variables:
 *   RELAYLY_SERVER=ws://...
 *   RELAYLY_PAIR_CODE=483921
 */

import { RelaylyClient, generateKey, encodeBase64, keyPairFromPrivateKey } from 'relayly';
import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { loadConfig } from './config.js';

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

// ─── Device registration ─────────────────────────────────────────────────────

interface DeviceCreds {
  device_id: string;
  device_token: string;
}

/** Registers via POST /api/v1/devices the first time this runs, reusing the saved
 * credentials afterward, the same load-or-create pattern used for the identity key. */
async function registerOrLoadDevice(serverUrl: string, credsFile: string, name: string): Promise<DeviceCreds> {
  if (existsSync(credsFile)) {
    const creds = JSON.parse(readFileSync(credsFile, 'utf-8')) as DeviceCreds;
    if (creds.device_id && creds.device_token) return creds;
  }

  const apiUrl = new URL(serverUrl);
  apiUrl.protocol = apiUrl.protocol === 'wss:' ? 'https:' : 'http:';
  apiUrl.pathname = '/api/v1/devices';
  apiUrl.search = '';

  const resp = await fetch(apiUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!resp.ok) throw new Error(`registering device: server returned ${resp.status}`);
  const creds = (await resp.json()) as DeviceCreds;

  const dir = dirname(credsFile);
  if (!existsSync(dir)) mkdirSync(dir, { recursive: true, mode: 0o700 });
  writeFileSync(credsFile, JSON.stringify(creds, null, 2), { mode: 0o600 });
  return creds;
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
  const { device_id: deviceId, device_token: deviceToken } = await registerOrLoadDevice(
    cfg.serverUrl,
    cfg.credsPath,
    'node-send',
  );

  const client = new RelaylyClient(cfg.serverUrl, {
    deviceId,
    deviceToken,
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
