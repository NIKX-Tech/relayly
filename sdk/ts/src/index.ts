/**
 * relayly-client — Public API
 *
 * Main exports for browser, Node.js, and React Native.
 *
 * @example
 * ```ts
 * import { RelaylyClient, generateKey, keyPairFromPrivateKey } from 'relayly-client';
 * ```
 */

// Main client
export { RelaylyClient } from './client.js';

// Crypto utilities
export {
  generateKey,
  keyPairFromPrivateKey,
  encrypt,
  decrypt,
  encodeBase64,
  decodeBase64,
  stringToBytes,
  bytesToString,
} from './crypto.js';

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
//   import { usePairing, useRelayly } from 'relayly-client/react';
