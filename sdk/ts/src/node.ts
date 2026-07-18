/**
 * Node.js-only extras, import from 'relayly/node', never from the main
 * entry point, so browser bundlers never try to resolve `node:fs`.
 *
 * @example
 * ```ts
 * import { RelaylyClient } from 'relayly';
 * import { FilePeerKeyStore } from 'relayly/node';
 *
 * const client = new RelaylyClient(url, {
 *   deviceId, deviceToken, keyPair,
 *   peerStore: new FilePeerKeyStore(), // defaults to ~/.relayly/peers.json
 * });
 * ```
 */
import { readFile, mkdir, rename, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { dirname, join } from 'node:path';
import type { PeerKeyStore, PinnedPeer } from './peerStore.js';
import { PeerKeyMismatchError } from './errors.js';

export type { PeerKeyStore, PinnedPeer } from './peerStore.js';
export { InMemoryPeerKeyStore } from './peerStore.js';

export const DEFAULT_PEER_STORE_PATH = join(homedir(), '.relayly', 'peers.json');

/**
 * Filesystem-backed peer key store, reading/writing the exact canonical JSON schema
 * shared across every official SDK (docs/tasks/02-sdks-and-interop.md):
 * `{"<peer_device_id>": {"static_key": "<base64>", "pinned_at": "<rfc3339>"}}` — so
 * the same file can in principle be shared by sdk/go, sdk/py, sdk/rust clients on the
 * same machine.
 */
export class FilePeerKeyStore implements PeerKeyStore {
  constructor(private readonly path: string = DEFAULT_PEER_STORE_PATH) {}

  async get(peerId: string): Promise<PinnedPeer | undefined> {
    const pins = await this.load();
    const pin = pins[peerId];
    return pin ? { staticKey: pin.static_key, pinnedAt: pin.pinned_at } : undefined;
  }

  async pinOrVerify(peerId: string, staticKeyBase64: string): Promise<void> {
    const pins = await this.load();
    const existing = pins[peerId];
    if (existing) {
      if (existing.static_key !== staticKeyBase64) {
        throw new PeerKeyMismatchError(peerId);
      }
      return;
    }
    pins[peerId] = { static_key: staticKeyBase64, pinned_at: new Date().toISOString() };
    await this.save(pins);
  }

  private async load(): Promise<Record<string, WireSchemaPin>> {
    try {
      const data = await readFile(this.path, 'utf8');
      return data.trim() === '' ? {} : (JSON.parse(data) as Record<string, WireSchemaPin>);
    } catch (err) {
      if (isNotFound(err)) return {};
      throw err;
    }
  }

  private async save(pins: Record<string, WireSchemaPin>): Promise<void> {
    await mkdir(dirname(this.path), { recursive: true, mode: 0o700 });
    const tmp = `${this.path}.tmp`;
    await writeFile(tmp, JSON.stringify(pins, null, 2), { mode: 0o600 });
    await rename(tmp, this.path);
  }
}

/** The on-disk field names (snake_case, matching every other SDK's JSON exactly). */
interface WireSchemaPin {
  static_key: string;
  pinned_at: string;
}

function isNotFound(err: unknown): boolean {
  return typeof err === 'object' && err !== null && 'code' in err && (err as { code?: string }).code === 'ENOENT';
}
