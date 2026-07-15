import { describe, expect, it } from 'vitest';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { FilePeerKeyStore } from './node.js';
import { PeerKeyMismatchError } from './errors.js';

async function tempPath(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), 'relayly-peerstore-'));
  return join(dir, 'peers.json');
}

describe('FilePeerKeyStore', () => {
  it('persists the first pin to disk in the canonical schema', async () => {
    const path = await tempPath();
    const store = new FilePeerKeyStore(path);
    await store.pinOrVerify('device-b', 'key-b-base64');

    const raw = JSON.parse(await readFile(path, 'utf8'));
    expect(raw['device-b'].static_key).toBe('key-b-base64');
    expect(typeof raw['device-b'].pinned_at).toBe('string');
  });

  it('survives being reloaded from a fresh instance pointed at the same file', async () => {
    const path = await tempPath();
    await new FilePeerKeyStore(path).pinOrVerify('device-b', 'key-b');

    const reloaded = new FilePeerKeyStore(path);
    const got = await reloaded.get('device-b');
    expect(got?.staticKey).toBe('key-b');
  });

  it('treats re-announcing the same key as a no-op', async () => {
    const path = await tempPath();
    const store = new FilePeerKeyStore(path);
    await store.pinOrVerify('device-b', 'key-b');
    await expect(store.pinOrVerify('device-b', 'key-b')).resolves.not.toThrow();
  });

  it('rejects a mismatched key and keeps the original pin on disk', async () => {
    const path = await tempPath();
    const store = new FilePeerKeyStore(path);
    await store.pinOrVerify('device-b', 'key-b');
    await expect(store.pinOrVerify('device-b', 'key-b-different')).rejects.toThrow(PeerKeyMismatchError);
    expect((await store.get('device-b'))?.staticKey).toBe('key-b');
  });

  it('returns no pin for a file that does not exist yet', async () => {
    const path = await tempPath();
    const store = new FilePeerKeyStore(path);
    expect(await store.get('anyone')).toBeUndefined();
  });
});
