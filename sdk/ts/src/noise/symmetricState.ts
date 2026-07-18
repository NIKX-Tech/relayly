import { CipherState } from './cipherState.js';
import { HASHLEN, hash, noiseHKDF } from './primitives.js';

const PROTOCOL_NAME = new TextEncoder().encode('Noise_XX_25519_ChaChaPoly_BLAKE2s');

function concat(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}

/** Noise's SymmetricState: (ck, h, cipherState), threaded through one handshake. */
export class SymmetricState {
  private ck: Uint8Array;
  private h: Uint8Array;
  private readonly cipherState = new CipherState();

  constructor() {
    // InitializeSymmetric(protocol_name): "Noise_XX_25519_ChaChaPoly_BLAKE2s" is 34
    // bytes, longer than HASHLEN (32), so h = HASH(protocol_name); ck = h.
    this.h = PROTOCOL_NAME.length <= HASHLEN
      ? concat(PROTOCOL_NAME, new Uint8Array(HASHLEN - PROTOCOL_NAME.length))
      : hash(PROTOCOL_NAME);
    this.ck = this.h.slice();
  }

  mixKey(inputKeyMaterial: Uint8Array): void {
    const [ck, tempK] = noiseHKDF(this.ck, inputKeyMaterial, 2);
    this.ck = ck;
    this.cipherState.initializeKey(tempK);
  }

  mixHash(data: Uint8Array): void {
    this.h = hash(concat(this.h, data));
  }

  encryptAndHash(plaintext: Uint8Array): Uint8Array {
    const ciphertext = this.cipherState.encryptWithAd(this.h, plaintext);
    this.mixHash(ciphertext);
    return ciphertext;
  }

  decryptAndHash(ciphertext: Uint8Array): Uint8Array {
    const plaintext = this.cipherState.decryptWithAd(this.h, ciphertext);
    this.mixHash(ciphertext);
    return plaintext;
  }

  /** Split(): derives the two transport cipher states once the handshake completes. */
  split(): [CipherState, CipherState] {
    const [k1, k2] = noiseHKDF(this.ck, new Uint8Array(0), 2);
    const c1 = new CipherState();
    c1.initializeKey(k1);
    const c2 = new CipherState();
    c2.initializeKey(k2);
    return [c1, c2];
  }

  getHandshakeHash(): Uint8Array {
    return this.h;
  }
}
