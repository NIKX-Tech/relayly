import { NoiseSession } from './noise/session.js';
import type { KeyPair } from './noise/primitives.js';
import { NotReadyError } from './errors.js';

/** Throttles how often this client starts a brand new responder session for an
 * unsolicited msg1 arriving on an already-healthy peer connection. The relay is
 * untrusted (docs/PROTOCOL.md §6, §7); without this a malicious/compromised relay
 * could force perpetual handshake churn by injecting 0x01 frames. */
const UNSOLICITED_MSG1_MIN_INTERVAL_MS = 2000;

export interface HandshakeOutcome {
  reply?: Uint8Array | undefined;
  /** Set once the session this envelope was driving finishes (success or failure). */
  completed?: NoiseSession | undefined;
  /** True if `completed` was a rekey attempt (vs. the peer's first-ever handshake). */
  wasPending: boolean;
}

/**
 * Tracks everything this client knows about one paired device: the currently active
 * Noise session, and (while a rekey is in flight) a pending replacement session that
 * must not replace `active` until it actually completes — docs/PROTOCOL.md §6's
 * make-before-break rule.
 */
export interface PairWaiter {
  resolve: (publicKey: Uint8Array) => void;
  reject: (err: Error) => void;
}

export class PeerConnection {
  readonly id: string;
  /** From pair_complete, for the §7.2 cross-check. */
  announcedStaticKey: string;

  private active?: NoiseSession | undefined;
  private pending?: NoiseSession | undefined;
  private lastUnsolicitedMsg1 = 0;

  /** Notified exactly once — when this peer's very first handshake resolves
   * (successfully or not). Cleared on first use. */
  private firstPairWaiter?: PairWaiter | undefined;

  constructor(id: string, announcedStaticKey: string) {
    this.id = id;
    this.announcedStaticKey = announcedStaticKey;
  }

  setFirstPairWaiter(waiter: PairWaiter): void {
    this.firstPairWaiter = waiter;
  }

  /** Returns and clears the registered waiter, if any, so it is only ever notified once. */
  takeFirstPairWaiter(): PairWaiter | undefined {
    const waiter = this.firstPairWaiter;
    this.firstPairWaiter = undefined;
    return waiter;
  }

  /** Begins the very first handshake for this peer (§5.3: the accepting device initiates). */
  startAsInitiator(staticKeyPair: KeyPair): Uint8Array {
    const { session, msg1 } = NoiseSession.startAsInitiator(staticKeyPair);
    session.setPeerIdHint(this.id);
    this.active = session;
    return msg1;
  }

  /** Begins a replacement handshake (§6: the lexicographically smaller device_id
   * re-initiates on reconnect). The existing active session keeps working until the
   * replacement completes. */
  startRekeyAsInitiator(staticKeyPair: KeyPair): Uint8Array {
    const { session, msg1 } = NoiseSession.startAsInitiator(staticKeyPair);
    session.setPeerIdHint(this.id);
    this.pending = session;
    return msg1;
  }

  /**
   * Feeds one received envelopeHandshake payload to the right session, starting a
   * new responder session if needed and applying make-before-break plus the
   * unsolicited-msg1 rate limit.
   */
  handleHandshakeEnvelope(staticKeyPair: KeyPair, data: Uint8Array): HandshakeOutcome {
    let session: NoiseSession;
    let wasPending = false;

    if (this.pending) {
      session = this.pending;
      wasPending = true;
    } else if (this.active && !this.active.isReady()) {
      // Continuing the (first-ever) in-progress handshake.
      session = this.active;
    } else if (!this.active) {
      // No session at all yet: this incoming msg1 starts the very first handshake
      // for this peer, with us as responder.
      session = NoiseSession.startAsResponder(staticKeyPair);
      session.setPeerIdHint(this.id);
      this.active = session;
    } else {
      // active exists and is ready: an unsolicited msg1 on a healthy connection.
      const now = Date.now();
      if (now - this.lastUnsolicitedMsg1 < UNSOLICITED_MSG1_MIN_INTERVAL_MS) {
        return { wasPending: false }; // rate-limited, drop silently
      }
      this.lastUnsolicitedMsg1 = now;
      session = NoiseSession.startAsResponder(staticKeyPair);
      session.setPeerIdHint(this.id);
      this.pending = session;
      wasPending = true;
    }

    const reply = session.handleHandshakeMessage(data);
    const completed = session.isSettled() ? session : undefined;
    return { reply, completed, wasPending };
  }

  /** Swaps a just-completed pending session into active. No-op if `session` was
   * already active (the peer's first-ever handshake, not a rekey). */
  promote(session: NoiseSession): void {
    if (this.pending === session) {
      this.active = session;
      this.pending = undefined;
    }
  }

  /** Drops a failed pending replacement, leaving the existing active session (still
   * healthy) untouched. No-op if `session` was the first-ever (active) session. */
  abandon(session: NoiseSession): void {
    if (this.pending === session) {
      this.pending = undefined;
    }
  }

  /** The active session's authenticated peer static key, once ready. */
  get publicKey(): Uint8Array | undefined {
    return this.active?.isReady() ? this.active.peerStaticKey : undefined;
  }

  send(payload: Uint8Array): Uint8Array {
    if (!this.active) throw new NotReadyError(this.id);
    return this.active.encrypt(payload);
  }

  recv(ciphertext: Uint8Array): Uint8Array {
    if (!this.active) throw new NotReadyError(this.id);
    return this.active.decrypt(ciphertext);
  }
}
