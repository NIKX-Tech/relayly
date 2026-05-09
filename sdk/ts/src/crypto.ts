/**
 * End-to-end encryption using NaCl box (X25519 + XSalsa20-Poly1305).
 *
 * Uses the audited tweetnacl library — zero native dependencies,
 * works in browsers, Node.js, and React Native.
 */
import nacl from 'tweetnacl';
import naclUtil from 'tweetnacl-util';
import type { KeyPair, RawKey } from './types.js';

// tweetnacl-util API:
//   encodeBase64(arr: Uint8Array) → string
//   decodeBase64(s: string) → Uint8Array
//   encodeUTF8(arr: Uint8Array) → string   (Uint8Array → UTF-8 string)
//   decodeUTF8(s: string) → Uint8Array     (UTF-8 string → Uint8Array)

export const encodeBase64 = (arr: Uint8Array): string => naclUtil.encodeBase64(arr);
export const decodeBase64 = (s: string): Uint8Array => naclUtil.decodeBase64(s);
/** Convert a UTF-8 string to Uint8Array bytes */
export const stringToBytes = (s: string): Uint8Array => naclUtil.decodeUTF8(s);
/** Convert Uint8Array bytes to a UTF-8 string */
export const bytesToString = (arr: Uint8Array): string => naclUtil.encodeUTF8(arr);

/**
 * Generate a new random X25519 keypair.
 *
 * @example
 * const key = generateKey();
 * localStorage.setItem('relayly_private_key', encodeBase64(key.privateKey));
 */
export function generateKey(): KeyPair {
  const kp = nacl.box.keyPair();
  return {
    privateKey: kp.secretKey,
    publicKey: kp.publicKey,
  };
}

/**
 * Restore a keypair from a previously saved base64-encoded private key.
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
  const kp = nacl.box.keyPair.fromSecretKey(privateKey);
  return {
    privateKey: kp.secretKey,
    publicKey: kp.publicKey,
  };
}

/**
 * Encrypt a plaintext message for a recipient using NaCl box.
 *
 * @returns { ciphertext, nonce } — both as Uint8Array
 */
export function encrypt(
  plaintext: Uint8Array,
  recipientPublicKey: RawKey,
  senderPrivateKey: RawKey,
): { ciphertext: Uint8Array; nonce: Uint8Array } {
  const nonce = nacl.randomBytes(nacl.box.nonceLength);
  const ciphertext = nacl.box(plaintext, nonce, recipientPublicKey, senderPrivateKey);
  if (!ciphertext) {
    throw new Error('relayly: encryption failed');
  }
  return { ciphertext, nonce };
}

/**
 * Decrypt a ciphertext from a sender using NaCl box.
 *
 * @throws if decryption fails (wrong key, corrupted data, or tampered message)
 */
export function decrypt(
  ciphertext: Uint8Array,
  nonce: Uint8Array,
  senderPublicKey: RawKey,
  recipientPrivateKey: RawKey,
): Uint8Array {
  const plaintext = nacl.box.open(ciphertext, nonce, senderPublicKey, recipientPrivateKey);
  if (!plaintext) {
    throw new Error('relayly: decryption failed — message may be corrupted or key mismatch');
  }
  return plaintext;
}
