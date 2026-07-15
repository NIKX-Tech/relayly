import { CipherState } from './cipherState.js';
import { HandshakeState } from './handshakeState.js';
import type { KeyPair } from './primitives.js';
import { NotReadyError } from '../errors.js';

/** E2E envelope types (docs/PROTOCOL.md §6): binary WebSocket frames are one byte of
 * envelope type followed by the Noise message or transport ciphertext. */
export const ENVELOPE_HANDSHAKE = 0x01;
export const ENVELOPE_TRANSPORT = 0x02;

export function encodeEnvelope(kind: number, payload: Uint8Array): Uint8Array {
  const out = new Uint8Array(1 + payload.length);
  out[0] = kind;
  out.set(payload, 1);
  return out;
}

export function decodeEnvelope(frame: Uint8Array): { kind: number; payload: Uint8Array } | null {
  if (frame.length < 1) return null;
  return { kind: frame[0]!, payload: frame.slice(1) };
}

type SessionStatus = 'handshaking' | 'ready' | 'failed';

/**
 * Drives one Noise XX handshake (docs/PROTOCOL.md §6) as either role and, once it
 * completes, encrypts/decrypts transport messages for that session. Does not itself
 * implement the make-before-break replacement policy — see peer.ts for the wrapper
 * that decides when a new NoiseSession may replace an existing one.
 */
export class NoiseSession {
  private status: SessionStatus = 'handshaking';
  private readonly handshake: HandshakeState;
  private gotFirstMessage = false;
  private sendCipher?: CipherState;
  private recvCipher?: CipherState;
  private peerId?: string | undefined;
  private _peerStaticKey?: Uint8Array | undefined;

  private readonly readyPromise: Promise<void>;
  private resolveReady!: () => void;
  private rejectReady!: (err: Error) => void;

  private constructor(private readonly initiator: boolean, staticKeyPair: KeyPair) {
    this.handshake = new HandshakeState(initiator, staticKeyPair);
    this.readyPromise = new Promise((resolve, reject) => {
      this.resolveReady = resolve;
      this.rejectReady = reject;
    });
    this.readyPromise.catch(() => {}); // never an unhandled rejection if nobody awaits
  }

  /** Starts a handshake as the Noise initiator. Returns the msg1 payload to send. */
  static startAsInitiator(staticKeyPair: KeyPair): { session: NoiseSession; msg1: Uint8Array } {
    const session = new NoiseSession(true, staticKeyPair);
    const { message } = session.handshake.writeMessage();
    return { session, msg1: message };
  }

  /** Starts a handshake as the Noise responder, ready to receive msg1. */
  static startAsResponder(staticKeyPair: KeyPair): NoiseSession {
    return new NoiseSession(false, staticKeyPair);
  }

  get peerStaticKey(): Uint8Array | undefined {
    return this._peerStaticKey;
  }

  isReady(): boolean {
    return this.status === 'ready';
  }

  /** True once the handshake has finished, successfully or not. */
  isSettled(): boolean {
    return this.status !== 'handshaking';
  }

  /** Resolves once the handshake finishes; rejects if it fails. */
  waitReady(): Promise<void> {
    return this.readyPromise;
  }

  /**
   * Feeds one received handshake message. Returns a reply to send, if any. A bad
   * peer message fails this session (observable via waitReady()); it does not throw.
   */
  handleHandshakeMessage(data: Uint8Array): Uint8Array | undefined {
    if (this.status !== 'handshaking') {
      throw new Error('relayly: handshake message received after handshake finished');
    }

    try {
      if (this.initiator) {
        // The only message an initiator ever receives is msg2; writing msg3
        // completes the handshake for the initiator (matches HandshakeState).
        this.handshake.readMessage(data);
        const { message, result } = this.handshake.writeMessage();
        if (result) {
          this.finish(result.cipherState1, result.cipherState2);
        }
        return message;
      }

      if (!this.gotFirstMessage) {
        this.gotFirstMessage = true;
        this.handshake.readMessage(data); // msg1
        const { message } = this.handshake.writeMessage(); // msg2
        return message;
      }

      const { result } = this.handshake.readMessage(data); // msg3
      if (result) {
        this.finish(result.cipherState2, result.cipherState1);
      }
      return undefined;
    } catch (err) {
      this.fail(err instanceof Error ? err : new Error(String(err)));
      return undefined;
    }
  }

  private finish(sendCipher: CipherState, recvCipher: CipherState): void {
    this.sendCipher = sendCipher;
    this.recvCipher = recvCipher;
    this._peerStaticKey = this.handshake.peerStaticKey;
    this.status = 'ready';
    this.resolveReady();
  }

  private fail(err: Error): void {
    if (this.status === 'failed') return;
    this.status = 'failed';
    this.rejectReady(err);
  }

  encrypt(plaintext: Uint8Array): Uint8Array {
    if (this.status !== 'ready' || !this.sendCipher) {
      throw new NotReadyError(this.peerId ?? '?');
    }
    return this.sendCipher.encryptWithAd(new Uint8Array(0), plaintext);
  }

  decrypt(ciphertext: Uint8Array): Uint8Array {
    if (this.status !== 'ready' || !this.recvCipher) {
      throw new NotReadyError(this.peerId ?? '?');
    }
    return this.recvCipher.decryptWithAd(new Uint8Array(0), ciphertext);
  }

  /** Used only to make NotReadyError's message more useful; purely cosmetic. */
  setPeerIdHint(peerId: string): void {
    this.peerId = peerId;
  }
}
