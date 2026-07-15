import { CipherState } from './cipherState.js';
import { SymmetricState } from './symmetricState.js';
import { DHLEN, KeyPair, dh, generateKeypair, randomBytes as defaultRandomBytes } from './primitives.js';

const TAGLEN = 16;

function concatAll(parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}

export interface HandshakeResult {
  /** initiator -> responder */
  cipherState1: CipherState;
  /** responder -> initiator */
  cipherState2: CipherState;
}

/**
 * Drives one Noise XX handshake (docs/PROTOCOL.md §6): `-> e` / `<- e, ee, s, es` /
 * `-> s, se`. Hardcoded to this one pattern rather than a generic multi-pattern
 * engine — it's all Protocol v1 needs, and it's less surface for a mistake. Verified
 * byte-for-byte against flynn/noise-generated vectors in noise.test.ts, not just
 * implemented from memory of the spec.
 */
export class HandshakeState {
  private readonly symmetric = new SymmetricState();
  private readonly s: KeyPair;
  private readonly randomBytes: (n: number) => Uint8Array;
  private e?: KeyPair;
  private rs?: Uint8Array;
  private re?: Uint8Array;
  private writeCount = 0;
  private readCount = 0;

  constructor(
    private readonly initiator: boolean,
    staticKeyPair: KeyPair,
    randomBytes: (n: number) => Uint8Array = defaultRandomBytes,
  ) {
    this.s = staticKeyPair;
    this.randomBytes = randomBytes;
    // Initialize(...) always calls MixHash(prologue) right after InitializeSymmetric,
    // even for an empty prologue (docs/PROTOCOL.md §6) — mixing in an empty byte
    // array still changes h (h = HASH(h)), so this is not a no-op to skip. XX has no
    // pre-message keys to mix in beyond this.
    this.symmetric.mixHash(new Uint8Array(0));
  }

  get peerStaticKey(): Uint8Array | undefined {
    return this.rs;
  }

  /** Writes this party's next handshake message. */
  writeMessage(): { message: Uint8Array; result?: HandshakeResult } {
    if (this.initiator && this.writeCount === 0) {
      // Message 1: -> e
      this.e = generateKeypair(this.randomBytes);
      this.symmetric.mixHash(this.e.publicKey);
      const payload = this.symmetric.encryptAndHash(new Uint8Array(0));
      this.writeCount++;
      return { message: concatAll([this.e.publicKey, payload]) };
    }

    if (!this.initiator && this.writeCount === 0) {
      // Message 2: <- e, ee, s, es
      this.e = generateKeypair(this.randomBytes);
      this.symmetric.mixHash(this.e.publicKey);
      this.symmetric.mixKey(dh(this.e.privateKey, this.re!));
      const encryptedS = this.symmetric.encryptAndHash(this.s.publicKey);
      this.symmetric.mixKey(dh(this.s.privateKey, this.re!));
      const payload = this.symmetric.encryptAndHash(new Uint8Array(0));
      this.writeCount++;
      return { message: concatAll([this.e.publicKey, encryptedS, payload]) };
    }

    if (this.initiator && this.writeCount === 1) {
      // Message 3: -> s, se
      const encryptedS = this.symmetric.encryptAndHash(this.s.publicKey);
      this.symmetric.mixKey(dh(this.s.privateKey, this.re!));
      const payload = this.symmetric.encryptAndHash(new Uint8Array(0));
      this.writeCount++;
      const [cipherState1, cipherState2] = this.symmetric.split();
      return { message: concatAll([encryptedS, payload]), result: { cipherState1, cipherState2 } };
    }

    throw new Error('relayly: no handshake message to write in this state');
  }

  /** Reads a handshake message received from the peer. */
  readMessage(message: Uint8Array): { result?: HandshakeResult } {
    if (!this.initiator && this.readCount === 0) {
      // Message 1: -> e
      this.re = message.slice(0, DHLEN);
      this.symmetric.mixHash(this.re);
      this.symmetric.decryptAndHash(message.slice(DHLEN)); // empty payload
      this.readCount++;
      return {};
    }

    if (this.initiator && this.readCount === 0) {
      // Message 2: <- e, ee, s, es
      this.re = message.slice(0, DHLEN);
      this.symmetric.mixHash(this.re);
      this.symmetric.mixKey(dh(this.e!.privateKey, this.re));
      const encryptedS = message.slice(DHLEN, DHLEN + DHLEN + TAGLEN);
      this.rs = this.symmetric.decryptAndHash(encryptedS);
      this.symmetric.mixKey(dh(this.e!.privateKey, this.rs));
      this.symmetric.decryptAndHash(message.slice(DHLEN + DHLEN + TAGLEN)); // empty payload
      this.readCount++;
      return {};
    }

    if (!this.initiator && this.readCount === 1) {
      // Message 3: -> s, se
      const encryptedS = message.slice(0, DHLEN + TAGLEN);
      this.rs = this.symmetric.decryptAndHash(encryptedS);
      this.symmetric.mixKey(dh(this.e!.privateKey, this.rs));
      this.symmetric.decryptAndHash(message.slice(DHLEN + TAGLEN)); // empty payload
      this.readCount++;
      const [cipherState1, cipherState2] = this.symmetric.split();
      return { result: { cipherState1, cipherState2 } };
    }

    throw new Error('relayly: no handshake message expected in this state');
  }
}
