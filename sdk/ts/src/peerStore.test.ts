import { describe, expect, it } from 'vitest';
import { InMemoryPeerKeyStore } from './peerStore.js';
import { PeerKeyMismatchError } from './errors.js';

describe('InMemoryPeerKeyStore', () => {
  it('pins the first-seen key and persists it in memory', async () => {
    const store = new InMemoryPeerKeyStore();
    await store.pinOrVerify('device-b', 'key-b');
    const got = await store.get('device-b');
    expect(got?.staticKey).toBe('key-b');
  });

  it('treats re-announcing the same key as a no-op', async () => {
    const store = new InMemoryPeerKeyStore();
    await store.pinOrVerify('device-b', 'key-b');
    await expect(store.pinOrVerify('device-b', 'key-b')).resolves.not.toThrow();
  });

  it('rejects a mismatched key and keeps the original pin', async () => {
    const store = new InMemoryPeerKeyStore();
    await store.pinOrVerify('device-b', 'key-b');
    await expect(store.pinOrVerify('device-b', 'key-b-different')).rejects.toThrow(PeerKeyMismatchError);
    const got = await store.get('device-b');
    expect(got?.staticKey).toBe('key-b');
  });

  it('has no pins for a fresh store', async () => {
    const store = new InMemoryPeerKeyStore();
    expect(await store.get('anyone')).toBeUndefined();
  });
});
