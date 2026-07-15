/**
 * Self-pair integration test: builds and runs the real cmd/relayly server binary
 * (not an in-process test double) and drives two sdk/ts RelaylyClients through it
 * end to end — register, connect, pair, a real Noise XX handshake, and bidirectional
 * encrypted delivery. This is the "each SDK against itself" leg of the interop matrix
 * (docs/tasks/02-sdks-and-interop.md), landed early since it directly de-risks this
 * PR the same way sdk/go's client_test.go does.
 *
 * Node has no global WebSocket on two of the three Node versions this repo's CI
 * matrix tests (18, 20; only 22 has it natively), so this file polyfills
 * globalThis.WebSocket with the `ws` package before importing RelaylyClient — a
 * test-only devDependency, not a runtime dependency of the SDK.
 */
import { WebSocket as NodeWebSocket } from 'ws';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
(globalThis as any).WebSocket = NodeWebSocket;

import { describe, expect, it } from 'vitest';
import { execFile, spawn, type ChildProcess } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'node:net';

const execFileAsync = promisify(execFile);
const currentDir = dirname(fileURLToPath(import.meta.url));

async function repoRoot(): Promise<string> {
  // sdk/ts/src is three levels under the repo root.
  return join(currentDir, '..', '..', '..');
}

async function buildRelayServer(): Promise<string> {
  const root = await repoRoot();
  const dir = await mkdtemp(join(tmpdir(), 'relayly-server-'));
  const binPath = join(dir, 'relayly-server');
  await execFileAsync('go', ['build', '-o', binPath, './cmd/relayly'], { cwd: root });
  return binPath;
}

async function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.listen(0, '127.0.0.1', () => {
      const address = srv.address();
      const port = typeof address === 'object' && address ? address.port : 0;
      srv.close(() => resolve(port));
    });
    srv.on('error', reject);
  });
}

interface RunningServer {
  baseUrl: string;
  wsUrl: string;
  stop: () => Promise<void>;
}

async function startRelayServer(binPath: string): Promise<RunningServer> {
  const port = await freePort();
  const dbDir = await mkdtemp(join(tmpdir(), 'relayly-db-'));
  const dbPath = join(dbDir, 'relayly.db');

  const child: ChildProcess = spawn(binPath, ['start', '--host', '127.0.0.1', '--port', String(port), '--db.path', dbPath], {
    stdio: ['ignore', 'ignore', 'inherit'],
  });

  const baseUrl = `http://127.0.0.1:${port}`;
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    try {
      const resp = await fetch(`${baseUrl}/health`);
      if (resp.ok) break;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 50));
  }

  return {
    baseUrl,
    wsUrl: `ws://127.0.0.1:${port}/ws`,
    stop: async () => {
      child.kill();
      await rm(dbDir, { recursive: true, force: true });
    },
  };
}

interface DeviceCreds {
  device_id: string;
  device_token: string;
}

async function registerDevice(baseUrl: string, name: string): Promise<DeviceCreds> {
  const resp = await fetch(`${baseUrl}/api/v1/devices`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!resp.ok) throw new Error(`registering device ${name}: status ${resp.status}`);
  return (await resp.json()) as DeviceCreds;
}

describe('RelaylyClient self-pair integration', () => {
  it('registers, connects, pairs, and exchanges encrypted messages both ways', async () => {
    const { RelaylyClient } = await import('./client.js');
    const { generateKey } = await import('./crypto.js');
    const { InMemoryPeerKeyStore } = await import('./peerStore.js');

    const binPath = await buildRelayServer();
    const server = await startRelayServer(binPath);
    try {
      const devA = await registerDevice(server.baseUrl, 'device-a');
      const devB = await registerDevice(server.baseUrl, 'device-b');

      const clientA = new RelaylyClient(server.wsUrl, {
        deviceId: devA.device_id,
        deviceToken: devA.device_token,
        keyPair: generateKey(),
        peerStore: new InMemoryPeerKeyStore(),
      });
      const clientB = new RelaylyClient(server.wsUrl, {
        deviceId: devB.device_id,
        deviceToken: devB.device_token,
        keyPair: generateKey(),
        peerStore: new InMemoryPeerKeyStore(),
      });

      await clientA.connect();
      await clientB.connect();

      const code = await clientA.requestPairCode();
      expect(code.shortCode).toHaveLength(6);

      const waitForA = clientA.waitForPairing();
      const peerB = await clientB.acceptPair(code.shortCode);
      const peerA = await waitForA;

      expect(peerA.id).toBe(devB.device_id);
      expect(peerB.id).toBe(devA.device_id);

      const messageFromB = new Promise<string>((resolve) => {
        clientB.on('message', (msg) => resolve(msg.payload));
      });
      await clientA.send(peerA.id, 'hello from A');
      expect(await messageFromB).toBe('hello from A');

      const messageFromA = new Promise<string>((resolve) => {
        clientA.on('message', (msg) => resolve(msg.payload));
      });
      await clientB.send(peerB.id, 'hello from B');
      expect(await messageFromA).toBe('hello from B');

      clientA.disconnect();
      clientB.disconnect();
    } finally {
      await server.stop();
    }
  }, 30_000);
});
