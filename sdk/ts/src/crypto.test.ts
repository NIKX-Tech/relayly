import { describe, it, expect } from 'vitest';
import { generateKey, keyPairFromPrivateKey, encodeBase64, decodeBase64, stringToBytes, bytesToString } from './crypto';

describe('Relayly Crypto', () => {
  it('should generate a valid keypair', () => {
    const keyPair = generateKey();
    expect(keyPair.publicKey).toHaveLength(32);
    expect(keyPair.privateKey).toHaveLength(32);
  });

  it('should generate different keys each time', () => {
    const a = generateKey();
    const b = generateKey();
    expect(encodeBase64(a.privateKey)).not.toBe(encodeBase64(b.privateKey));
  });

  it('should restore the same keypair from a saved base64 private key', () => {
    const original = generateKey();
    const restored = keyPairFromPrivateKey(encodeBase64(original.privateKey));
    expect(encodeBase64(restored.privateKey)).toBe(encodeBase64(original.privateKey));
    expect(encodeBase64(restored.publicKey)).toBe(encodeBase64(original.publicKey));
  });

  it('should reject a private key of the wrong length', () => {
    expect(() => keyPairFromPrivateKey(encodeBase64(new Uint8Array(16)))).toThrow();
  });

  it('should handle base64 encoding/decoding', () => {
    const bytes = new Uint8Array([1, 2, 3, 4, 5]);
    const b64 = encodeBase64(bytes);
    const decoded = decodeBase64(b64);

    expect(decoded).toEqual(bytes);
  });

  it('should round-trip UTF-8 strings', () => {
    const message = 'Hello, 世界! 👋';
    expect(bytesToString(stringToBytes(message))).toBe(message);
  });
});
