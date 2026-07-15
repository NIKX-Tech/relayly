/**
 * Peer key pinning (docs/PROTOCOL.md §7.1): the client-side pin is the real security
 * boundary, not the relay's own announced-key lock. Browsers have no filesystem, so
 * the store is an injectable interface; the default is in-memory (works everywhere,
 * doesn't survive a reload/restart). A filesystem-backed store matching the exact
 * schema shared with every other official SDK lives in a separate entry point,
 * `relayly-client/node` (see node.ts), so browser bundlers never try to resolve
 * `node:fs`.
 */
import { PeerKeyMismatchError } from './errors.js';

/** One pinned peer entry — the schema shared byte-for-byte across every SDK. */
export interface PinnedPeer {
  staticKey: string; // base64
  pinnedAt: string; // RFC 3339
}

/** Injectable storage for pinned peer keys. All methods are async so a real
 * filesystem- or IndexedDB-backed implementation can be dropped in. */
export interface PeerKeyStore {
  get(peerId: string): Promise<PinnedPeer | undefined>;
  /** Pins staticKey for peerId if unset; verifies it matches if already pinned.
   * Throws PeerKeyMismatchError on a mismatch, leaving the original pin intact. */
  pinOrVerify(peerId: string, staticKeyBase64: string): Promise<void>;
}

let warnedOnce = false;

/**
 * Default peer key store: in-memory only. Logs a one-time warning, since pins that
 * don't survive a reload/restart weaken §7.1's guarantee to "TOFU every session"
 * rather than "TOFU once, ever" — fine for quick starts and browser demos, not for
 * anything that should actually detect a re-paired/compromised peer over time.
 */
export class InMemoryPeerKeyStore implements PeerKeyStore {
  private readonly pins = new Map<string, PinnedPeer>();

  async get(peerId: string): Promise<PinnedPeer | undefined> {
    return this.pins.get(peerId);
  }

  async pinOrVerify(peerId: string, staticKeyBase64: string): Promise<void> {
    if (!warnedOnce) {
      warnedOnce = true;
      // eslint-disable-next-line no-console
      console.warn(
        'relayly: using the default in-memory peer key store — pinned peer keys will ' +
          'not survive a reload/restart. Pass Options.peerStore (see relayly-client/node ' +
          'for a filesystem-backed store) for a persistent pin.',
      );
    }
    const existing = this.pins.get(peerId);
    if (existing) {
      if (existing.staticKey !== staticKeyBase64) {
        throw new PeerKeyMismatchError(peerId);
      }
      return;
    }
    this.pins.set(peerId, { staticKey: staticKeyBase64, pinnedAt: new Date().toISOString() });
  }
}
