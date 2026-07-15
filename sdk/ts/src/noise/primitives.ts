/**
 * Thin wrappers around the vetted primitives Noise_XX_25519_ChaChaPoly_BLAKE2s needs
 * (docs/PROTOCOL.md §6): X25519 (@noble/curves), ChaCha20-Poly1305 (@noble/ciphers),
 * BLAKE2s + HKDF (@noble/hashes). This file never implements curve or cipher math
 * itself, only the small amount of Noise-specific bookkeeping (nonce encoding,
 * splitting HKDF output into the blocks Noise expects) around them.
 */
import { x25519 } from '@noble/curves/ed25519.js';
import { chacha20poly1305 } from '@noble/ciphers/chacha.js';
import { blake2s } from '@noble/hashes/blake2.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { randomBytes as nobleRandomBytes } from '@noble/hashes/utils.js';

export const DHLEN = 32;
export const HASHLEN = 32;

export interface KeyPair {
  privateKey: Uint8Array;
  publicKey: Uint8Array;
}

/** Generates a fresh X25519 keypair using a CSPRNG (or an injected one, for tests). */
export function generateKeypair(randomBytes: (n: number) => Uint8Array = nobleRandomBytes): KeyPair {
  const privateKey = randomBytes(DHLEN);
  return { privateKey, publicKey: x25519.getPublicKey(privateKey) };
}

/** Derives the X25519 public key for a given private key (e.g. one loaded from disk). */
export function publicKeyFromPrivate(privateKey: Uint8Array): Uint8Array {
  return x25519.getPublicKey(privateKey);
}

/** DH(privateKey, publicKey) — the raw X25519 shared secret, Noise's `DH()` function. */
export function dh(privateKey: Uint8Array, publicKey: Uint8Array): Uint8Array {
  return x25519.getSharedSecret(privateKey, publicKey);
}

/** HASH(data) — BLAKE2s, 32-byte output. */
export function hash(data: Uint8Array): Uint8Array {
  return blake2s(data);
}

/**
 * Noise's HKDF(chaining_key, input_key_material, numOutputs): RFC 5869 HKDF with
 * salt=chaining_key, ikm=input_key_material, empty info, chopped into 32-byte blocks.
 * Used for both MixKey (2 outputs) and Split (2 outputs) — XX never needs 3.
 */
export function noiseHKDF(chainingKey: Uint8Array, inputKeyMaterial: Uint8Array, numOutputs: 2): [Uint8Array, Uint8Array] {
  const okm = hkdf(blake2s, inputKeyMaterial, chainingKey, undefined, HASHLEN * numOutputs);
  const outputs: Uint8Array[] = [];
  for (let i = 0; i < numOutputs; i++) {
    outputs.push(okm.slice(i * HASHLEN, (i + 1) * HASHLEN));
  }
  return outputs as [Uint8Array, Uint8Array];
}

/** Encodes a Noise nonce counter as ChaChaPoly expects: 4 zero bytes + 8-byte little-endian n. */
function encodeNonce(n: bigint): Uint8Array {
  const nonce = new Uint8Array(12);
  const view = new DataView(nonce.buffer);
  view.setBigUint64(4, n, true); // little-endian, per docs/PROTOCOL.md §6 / the Noise spec
  return nonce;
}

/** ENCRYPT(k, n, ad, plaintext) — ChaCha20-Poly1305, ciphertext includes the 16-byte tag. */
export function aeadEncrypt(key: Uint8Array, n: bigint, ad: Uint8Array, plaintext: Uint8Array): Uint8Array {
  return chacha20poly1305(key, encodeNonce(n), ad).encrypt(plaintext);
}

/** DECRYPT(k, n, ad, ciphertext) — throws if authentication fails. */
export function aeadDecrypt(key: Uint8Array, n: bigint, ad: Uint8Array, ciphertext: Uint8Array): Uint8Array {
  return chacha20poly1305(key, encodeNonce(n), ad).decrypt(ciphertext);
}

export { nobleRandomBytes as randomBytes };
