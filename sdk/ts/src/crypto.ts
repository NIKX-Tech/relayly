/**
 * Key generation and encoding helpers. Device identity keys are X25519 (used as the
 * Noise XX static keypair, docs/PROTOCOL.md §6); actual encryption is a stateful
 * Noise session (see noise/session.ts), not a function you call per message, so
 * there's no encrypt()/decrypt() here anymore.
 *
 * Uses @noble/curves (X25519) and @noble/hashes (randomness) — audited, pure
 * TS/JS, zero native dependencies, works in browsers, Node.js, and React Native.
 */
import { generateKeypair, publicKeyFromPrivate } from './noise/primitives.js';
import type { KeyPair, RawKey } from './types.js';

export function encodeBase64(bytes: Uint8Array): string {
  if (typeof Buffer !== 'undefined') {
    return Buffer.from(bytes).toString('base64');
  }
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}

export function decodeBase64(s: string): Uint8Array {
  if (typeof Buffer !== 'undefined') {
    return new Uint8Array(Buffer.from(s, 'base64'));
  }
  const binary = atob(s);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

/** Convert a UTF-8 string to Uint8Array bytes */
export const stringToBytes = (s: string): Uint8Array => new TextEncoder().encode(s);
/** Convert Uint8Array bytes to a UTF-8 string */
export const bytesToString = (arr: Uint8Array): string => new TextDecoder().decode(arr);

/**
 * Generate a new random X25519 keypair.
 *
 * @example
 * const key = generateKey();
 * localStorage.setItem('relayly_private_key', encodeBase64(key.privateKey));
 */
export function generateKey(): KeyPair {
  const kp = generateKeypair();
  return { privateKey: kp.privateKey, publicKey: kp.publicKey };
}

/**
 * Restore a keypair from a previously saved base64-encoded private key. Reads the
 * same on-disk format the pre-Protocol-v1 SDK used (32-byte X25519 key, base64), so
 * existing device key files remain valid.
 *
 * @example
 * const saved = localStorage.getItem('relayly_private_key');
 * const key = keyPairFromPrivateKey(saved!);
 */
export function keyPairFromPrivateKey(base64PrivateKey: string): KeyPair {
  const privateKey = decodeBase64(base64PrivateKey);
  if (privateKey.length !== 32) {
    throw new Error(`relayly: invalid private key length — expected 32 bytes, got ${privateKey.length}`);
  }
  return { privateKey, publicKey: publicKeyFromPrivate(privateKey) };
}

export type { KeyPair, RawKey };
