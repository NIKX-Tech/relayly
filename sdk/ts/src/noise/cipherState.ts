import { aeadDecrypt, aeadEncrypt } from './primitives.js';

/** Noise's CipherState: (k, n). Encrypts/decrypts one direction of one session. */
export class CipherState {
  private key: Uint8Array | null = null;
  private n = 0n;

  initializeKey(key: Uint8Array): void {
    this.key = key;
    this.n = 0n;
  }

  hasKey(): boolean {
    return this.key !== null;
  }

  encryptWithAd(ad: Uint8Array, plaintext: Uint8Array): Uint8Array {
    if (this.key === null) return plaintext;
    const ciphertext = aeadEncrypt(this.key, this.n, ad, plaintext);
    this.n += 1n;
    return ciphertext;
  }

  decryptWithAd(ad: Uint8Array, ciphertext: Uint8Array): Uint8Array {
    if (this.key === null) return ciphertext;
    const plaintext = aeadDecrypt(this.key, this.n, ad, ciphertext);
    this.n += 1n;
    return plaintext;
  }
}
