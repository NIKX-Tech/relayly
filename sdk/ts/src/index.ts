/**
 * relayly, Public API
 *
 * Main exports for browser, Node.js, and React Native.
 *
 * @example
 * ```ts
 * import { RelaylyClient, generateKey, keyPairFromPrivateKey } from 'relayly';
 * ```
 *
 * Node.js-only extras (e.g. a filesystem-backed peer key store) live at
 * `relayly/node` so browser bundlers never try to resolve `node:fs`:
 *   import { FilePeerKeyStore } from 'relayly/node';
 */

// Main client
export { RelaylyClient } from './client.js';

// Crypto / key utilities
export {
  generateKey,
  keyPairFromPrivateKey,
  encodeBase64,
  decodeBase64,
  stringToBytes,
  bytesToString,
} from './crypto.js';

// Peer key pinning (docs/PROTOCOL.md §7.1) — the in-memory default; see
// relayly/node for a persistent, cross-SDK-compatible store.
export { InMemoryPeerKeyStore } from './peerStore.js';
export type { PeerKeyStore, PinnedPeer } from './peerStore.js';

// Typed errors
export {
  RelaylyClientError,
  NotReadyError,
  PeerKeyMismatchError,
  InvalidCodeError,
  CodeExpiredError,
  AlreadyPairedError,
  PeerOfflineError,
  RateLimitedError,
  MalformedError,
  InternalError,
  KeyMismatchError,
} from './errors.js';

// Types — all public facing interfaces
export type {
  KeyPair,
  RawKey,
  Peer,
  PairCode,
  RelayMessage,
  RelaylyClientOptions,
  RelaylyClientEvents,
  RelaylyError,
} from './types.js';

// React hooks are NOT re-exported here to avoid forcing React as a dependency.
// Import them separately:
//   import { usePairing, useRelayly } from 'relayly/react';
