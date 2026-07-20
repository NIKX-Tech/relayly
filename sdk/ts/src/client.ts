/**
 * RelaylyClient — the main client class for connecting to a Relayly relay server.
 *
 * @example
 * ```ts
 * import { RelaylyClient, generateKey } from 'relayly';
 *
 * const keyPair = generateKey();
 * const client = new RelaylyClient('wss://relay.example.com/ws', {
 *   deviceId: 'my-browser',
 *   deviceToken, // from POST /api/v1/devices
 *   keyPair,
 * });
 *
 * await client.connect();
 *
 * const { shortCode } = await client.requestPairCode();
 * console.log('Share this code:', shortCode);
 *
 * const peer = await client.waitForPairing(); // blocks until the Noise handshake is up too
 * await client.send(peer.id, 'Hello!');
 *
 * client.on('message', (msg) => {
 *   console.log(`[${msg.from}]`, msg.payload);
 * });
 * ```
 */
import { WebSocketTransport } from './transport.js';
import { encodeBase64, stringToBytes, bytesToString } from './crypto.js';
import { PeerConnection } from './peer.js';
import { InMemoryPeerKeyStore } from './peerStore.js';
import { ENVELOPE_HANDSHAKE, ENVELOPE_TRANSPORT, encodeEnvelope, decodeEnvelope } from './noise/session.js';
import type { NoiseSession } from './noise/session.js';
import { errorForCode, KeyMismatchError, RelaylyClientError } from './errors.js';
import type {
  Peer,
  PairCode,
  RelayMessage,
  RelaylyClientOptions,
  RelaylyClientEvents,
  RelaylyError,
  WireMessage,
} from './types.js';
import type { PeerKeyStore } from './peerStore.js';

// ─── Wire message type constants (docs/PROTOCOL.md §5) ────────────────────────

const MSG = {
  WELCOME: 'welcome',
  ANNOUNCE_KEY: 'announce_key',
  PAIR_REQUEST: 'pair_request',
  PAIR_CODE: 'pair_code',
  PAIR_ACCEPT: 'pair_accept',
  PAIR_COMPLETE: 'pair_complete',
  PEER_STATUS: 'peer_status',
  PING: 'ping',
  PONG: 'pong',
  ERROR: 'error',
} as const;

const PROTOCOL_VERSION = 1;
const DEFAULT_PING_INTERVAL_MS = 30_000;

// ─── Event emitter (tiny, dependency-free) ────────────────────────────────────

type EventMap = RelaylyClientEvents;
type EventKey = keyof EventMap;
type Listener<K extends EventKey> = EventMap[K];

class TinyEmitter {
  private listeners: { [K in EventKey]?: Array<Listener<K>> } = {};

  on<K extends EventKey>(event: K, listener: Listener<K>): this {
    if (!this.listeners[event]) {
      (this.listeners as Record<K, Array<Listener<K>>>)[event] = [];
    }
    (this.listeners[event] as Array<Listener<K>>).push(listener);
    return this;
  }

  off<K extends EventKey>(event: K, listener: Listener<K>): this {
    const arr = this.listeners[event] as Array<Listener<K>> | undefined;
    if (arr) {
      const idx = arr.indexOf(listener);
      if (idx !== -1) arr.splice(idx, 1);
    }
    return this;
  }

  once<K extends EventKey>(event: K, listener: Listener<K>): this {
    const wrapped = ((...args: Parameters<Listener<K>>) => {
      (listener as (...args: unknown[]) => void)(...(args as unknown[]));
      this.off(event, wrapped as Listener<K>);
    }) as Listener<K>;
    return this.on(event, wrapped);
  }

  protected emit<K extends EventKey>(event: K, ...args: Parameters<Listener<K>>): void {
    const arr = this.listeners[event] as Array<Listener<K>> | undefined;
    arr?.forEach((fn) => (fn as (...a: Parameters<Listener<K>>) => void)(...args));
  }
}

// ─── Pending pairing state ─────────────────────────────────────────────────────

interface PendingPairRequest {
  resolveCode: (code: PairCode) => void;
  reject: (err: Error) => void;
}

interface PendingAccept {
  resolve: (peer: Peer) => void;
  reject: (err: Error) => void;
}

// ─── RelaylyClient ────────────────────────────────────────────────────────────

/**
 * RelaylyClient connects to a Relayly relay server and provides a simple API for
 * device pairing and end-to-end encrypted messaging. Encryption is device-to-device
 * Noise XX (docs/PROTOCOL.md §6); the relay itself holds no key material.
 */
export class RelaylyClient extends TinyEmitter {
  private readonly serverUrl: string;
  private readonly opts: Required<RelaylyClientOptions>;
  private transport: WebSocketTransport;

  /** This device's one linked peer connection (v1: at most one, docs/PROTOCOL.md §5.3). */
  private peerConns = new Map<string, PeerConnection>();

  private pendingPairRequest: PendingPairRequest | null = null;
  private pendingPairAccepts = new Map<string, PendingAccept>();

  /** Resolves once welcome arrives and announce_key is sent after (re)connect. */
  private authResolve: (() => void) | null = null;
  private authReject: ((err: Error) => void) | null = null;

  private pingTimer: ReturnType<typeof setInterval> | null = null;

  constructor(serverUrl: string, opts: RelaylyClientOptions) {
    super();
    this.serverUrl = normalizeUrl(serverUrl);
    this.opts = {
      deviceId: opts.deviceId,
      deviceToken: opts.deviceToken,
      keyPair: opts.keyPair,
      peerStore: opts.peerStore ?? new InMemoryPeerKeyStore(),
      pingIntervalMs: opts.pingIntervalMs ?? DEFAULT_PING_INTERVAL_MS,
      reconnectDelayMs: opts.reconnectDelayMs ?? 1_000,
      maxReconnectDelayMs: opts.maxReconnectDelayMs ?? 60_000,
    };

    // Auth is HTTP-layer query params, no in-band auth frame (docs/PROTOCOL.md §3).
    const authedUrl = `${this.serverUrl}?device_id=${encodeURIComponent(this.opts.deviceId)}&token=${encodeURIComponent(this.opts.deviceToken)}`;

    this.transport = new WebSocketTransport(authedUrl, {
      reconnectDelayMs: this.opts.reconnectDelayMs,
      maxReconnectDelayMs: this.opts.maxReconnectDelayMs,
      onOpen: () => this.handleOpen(),
      onClose: (reason) => this.handleClose(reason),
      onMessage: (data) => this.handleMessage(data),
      onReconnecting: (attempt) => this.emit('reconnecting', attempt),
    });
  }

  // ─── Public API ────────────────────────────────────────────────────────────

  /**
   * Connect to the Relayly server and authenticate (HTTP-layer query params,
   * docs/PROTOCOL.md §3). Resolves once welcome is consumed and announce_key sent.
   *
   * @example
   * await client.connect();
   */
  async connect(): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      this.authResolve = resolve;
      this.authReject = reject;
      this.transport.connect();
    });
  }

  /**
   * Request a pairing code from the server.
   * Display the returned shortCode or render the qrCodeUrl as a QR image.
   *
   * @example
   * const { shortCode, qrCodeUrl } = await client.requestPairCode();
   * console.log('Share code:', shortCode);
   */
  async requestPairCode(): Promise<PairCode> {
    return new Promise<PairCode>((resolve, reject) => {
      this.pendingPairRequest = { resolveCode: resolve, reject };
      this.sendControl({ type: MSG.PAIR_REQUEST });
    });
  }

  /**
   * Accept a pairing code received from another device. Blocks until the resulting
   * Noise handshake completes (docs/PROTOCOL.md §5.3: the accepting device is the
   * initiator), so the returned Peer can be sent to immediately.
   *
   * @example
   * const peer = await client.acceptPair('483921');
   */
  async acceptPair(code: string): Promise<Peer> {
    return new Promise<Peer>((resolve, reject) => {
      this.pendingPairAccepts.set(code, { resolve, reject });
      this.sendControl({ type: MSG.PAIR_ACCEPT, code });
    });
  }

  /**
   * Wait for pairing to complete on the *requesting* side (after requestPairCode).
   * Resolves once the Noise handshake is up too — an alias for listening to 'paired' once.
   *
   * @example
   * const code = await client.requestPairCode();
   * showQR(code.qrCodeUrl);
   * const peer = await client.waitForPairing();
   */
  waitForPairing(): Promise<Peer> {
    return new Promise<Peer>((resolve, reject) => {
      this.once('paired', resolve);
      this.once('error', (err) => reject(new Error(err.message)));
    });
  }

  /**
   * Send an encrypted message to a paired peer.
   *
   * @throws NotReadyError if the peer's session isn't up yet (only expected in the
   * brief window after a reconnect forces a re-handshake).
   */
  async send(peerId: string, message: string | Uint8Array): Promise<void> {
    const peer = this.peerConns.get(peerId);
    if (!peer) {
      throw new Error(
        `relayly: no paired peer with ID "${peerId}" — call requestPairCode or acceptPair first`,
      );
    }
    const plaintextBytes: Uint8Array = typeof message === 'string' ? stringToBytes(message) : message;
    const ciphertext = peer.send(plaintextBytes); // throws NotReadyError if not ready
    this.sendBinary(encodeEnvelope(ENVELOPE_TRANSPORT, ciphertext));
  }

  /** Returns all currently paired peers ready to send() to. */
  getPeers(): ReadonlyMap<string, Peer> {
    const out = new Map<string, Peer>();
    for (const [id, conn] of this.peerConns) {
      const publicKey = conn.publicKey;
      if (publicKey) out.set(id, { id, publicKey });
    }
    return out;
  }

  /** Permanently close the connection. No further reconnects will occur. */
  disconnect(): void {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    this.transport.close();
  }

  // ─── Event listener overrides (typed) ─────────────────────────────────────

  override on<K extends EventKey>(event: K, listener: EventMap[K]): this {
    return super.on(event, listener);
  }

  override off<K extends EventKey>(event: K, listener: EventMap[K]): this {
    return super.off(event, listener);
  }

  override once<K extends EventKey>(event: K, listener: EventMap[K]): this {
    return super.once(event, listener);
  }

  // ─── Internal handlers ─────────────────────────────────────────────────────

  private handleOpen(): void {
    // Nothing to send yet: auth is HTTP-layer query params (docs/PROTOCOL.md §3),
    // there is no in-band auth frame. We wait for `welcome` before announce_key.
  }

  private handleClose(reason: string): void {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    if (this.authReject) {
      this.authReject(new Error(`relayly: connection closed before welcome (${reason})`));
      this.authResolve = null;
      this.authReject = null;
    }
    this.emit('disconnected', reason);
  }

  private handleMessage(data: string | Uint8Array): void {
    if (typeof data === 'string') {
      this.handleControlMessage(data);
      return;
    }

    const envelope = decodeEnvelope(data);
    if (!envelope) return;
    if (envelope.kind === ENVELOPE_HANDSHAKE) {
      this.handleHandshakeEnvelope(envelope.payload);
    } else if (envelope.kind === ENVELOPE_TRANSPORT) {
      this.handleTransportEnvelope(envelope.payload);
    }
  }

  private handleControlMessage(raw: string): void {
    let frame: WireMessage;
    try {
      frame = JSON.parse(raw) as WireMessage;
    } catch {
      return; // skip malformed frames
    }

    switch (frame.type) {
      case MSG.WELCOME:
        this.handleWelcome(frame);
        break;
      case MSG.PAIR_CODE:
        this.handlePairCode(frame);
        break;
      case MSG.PAIR_COMPLETE:
        this.handlePairComplete(frame);
        break;
      case MSG.PEER_STATUS:
        this.handlePeerStatus(frame);
        break;
      case MSG.ERROR:
        this.handleError(frame);
        break;
      case MSG.PONG:
        break; // keepalive, nothing to do
      default:
        break; // unknown type: ignored per §5's forward-compatibility rule
    }
  }

  private handleWelcome(frame: WireMessage): void {
    if (frame.protocol_version !== PROTOCOL_VERSION) {
      const err = new Error(
        `relayly: unsupported protocol_version ${frame.protocol_version} (this SDK implements ${PROTOCOL_VERSION})`,
      );
      this.authReject?.(err);
      this.authResolve = null;
      this.authReject = null;
      return;
    }

    // A reconnect must not discard an already-healthy peerConn just because the
    // control channel briefly reset — only register peers we don't already track.
    for (const p of frame.peers ?? []) {
      if (!this.peerConns.has(p.id)) {
        this.peerConns.set(p.id, new PeerConnection(p.id, p.static_key));
      }
    }

    this.sendControl({
      type: MSG.ANNOUNCE_KEY,
      static_key: encodeBase64(this.opts.keyPair.publicKey),
    });

    this.startPingLoop();
    this.emit('connected');
    this.authResolve?.();
    this.authResolve = null;
    this.authReject = null;
  }

  private handlePairCode(frame: WireMessage): void {
    const pending = this.pendingPairRequest;
    if (!pending || !frame.code) return;
    this.pendingPairRequest = null;

    const pairCode: PairCode = {
      shortCode: frame.code,
      expiresIn: frame.expires_in ?? 300,
      qrCodeUrl: buildQrUrl(this.serverUrl, frame.code),
    };
    pending.resolveCode(pairCode);
  }

  /** Implements §5.3: link the peer, and if we're the accepting device, start the
   * Noise handshake as initiator immediately. */
  private handlePairComplete(frame: WireMessage): void {
    if (!frame.peer_id) return;

    const pendingAccept = frame.code ? this.pendingPairAccepts.get(frame.code) : undefined;
    if (frame.code && pendingAccept) this.pendingPairAccepts.delete(frame.code);

    let peer = this.peerConns.get(frame.peer_id);
    if (!peer) {
      peer = new PeerConnection(frame.peer_id, frame.peer_static_key ?? '');
      this.peerConns.set(frame.peer_id, peer);
    } else {
      peer.announcedStaticKey = frame.peer_static_key ?? '';
    }

    if (pendingAccept) {
      const peerId = frame.peer_id;
      peer.setFirstPairWaiter({
        resolve: (publicKey) => pendingAccept.resolve({ id: peerId, publicKey }),
        reject: pendingAccept.reject,
      });
      const msg1 = peer.startAsInitiator(this.opts.keyPair);
      this.sendBinary(encodeEnvelope(ENVELOPE_HANDSHAKE, msg1));
    }
    // else: we requested; wait for the accepting device's incoming msg1. The
    // requester observes completion via the 'paired'/'ready' events (waitForPairing()).
  }

  /** Implements §6's reconnect rule: whichever side's device_id is lexicographically
   * smaller re-initiates a fresh handshake whenever the peer comes online. */
  private handlePeerStatus(frame: WireMessage): void {
    const online = frame.online === true;
    if (frame.peer_id) this.emit('peerStatus', frame.peer_id, online);
    if (!online || !frame.peer_id) return;

    const peer = this.peerConns.get(frame.peer_id);
    if (!peer) return;
    if (this.opts.deviceId >= frame.peer_id) return; // larger ID: wait for the incoming msg1

    try {
      const msg1 = peer.startRekeyAsInitiator(this.opts.keyPair);
      this.sendBinary(encodeEnvelope(ENVELOPE_HANDSHAKE, msg1));
    } catch {
      // best-effort; a future peer_status or AEAD failure can retry
    }
  }

  private handleError(frame: WireMessage): void {
    const typedErr = errorForCode(frame.code ?? '', frame.message ?? frame.code ?? 'unknown error');
    const publicErr = Object.assign(new Error(typedErr.message), { code: typedErr.code }) as RelaylyError;
    this.emit('error', publicErr);

    if (this.authReject) {
      this.authReject(typedErr);
      this.authResolve = null;
      this.authReject = null;
    }
    this.pendingPairRequest?.reject(typedErr);
    this.pendingPairRequest = null;
    this.pendingPairAccepts.forEach((p) => p.reject(typedErr));
    this.pendingPairAccepts.clear();
  }

  private handleHandshakeEnvelope(payload: Uint8Array): void {
    const peer = this.getSolePeer();
    if (!peer) return;

    const outcome = peer.handleHandshakeEnvelope(this.opts.keyPair, payload);
    if (outcome.reply) {
      this.sendBinary(encodeEnvelope(ENVELOPE_HANDSHAKE, outcome.reply));
    }
    if (outcome.completed) {
      this.resolveHandshake(peer, outcome.completed, outcome.wasPending).catch(() => {
        // resolveHandshake never throws (errors are routed to waiters/events), this
        // catch only guards against something unexpected inside peerStore.
      });
    }
  }

  /**
   * Runs once, exactly when a Noise handshake attempt finishes: applies §7's
   * client-side pin (the real boundary) and server-announced-key cross-check,
   * promotes or abandons a rekey attempt per §6's make-before-break rule, notifies
   * a first-pairing waiter if one is registered, and fires 'ready'/'paired'.
   */
  private async resolveHandshake(peer: PeerConnection, session: NoiseSession, wasPending: boolean): Promise<void> {
    let error: Error | undefined;
    let publicKey: Uint8Array | undefined;

    if (!session.isReady()) {
      error = new Error(`relayly: handshake with ${peer.id} failed`);
    } else {
      const authenticated = session.peerStaticKey!;
      const authenticatedB64 = encodeBase64(authenticated);
      try {
        await this.opts.peerStore.pinOrVerify(peer.id, authenticatedB64);
        if (peer.announcedStaticKey && peer.announcedStaticKey !== authenticatedB64) {
          error = new KeyMismatchError(
            `server-announced key for peer ${peer.id} does not match the authenticated handshake`,
          );
        } else {
          publicKey = authenticated;
        }
      } catch (err) {
        error = err instanceof Error ? err : new Error(String(err));
      }
    }

    if (wasPending) {
      if (error) {
        peer.abandon(session); // existing active session, if any, is untouched
        return;
      }
      peer.promote(session);
    }

    const waiter = peer.takeFirstPairWaiter();
    if (error) {
      waiter?.reject(error);
      if (!wasPending) {
        const code = error instanceof RelaylyClientError ? error.code : 'handshake_failed';
        this.emit('error', Object.assign(new Error(error.message), { code }) as RelaylyError);
      }
      return;
    }

    waiter?.resolve(publicKey!);
    this.emit('ready', peer.id);
    if (!wasPending) {
      this.emit('paired', { id: peer.id, publicKey: publicKey! });
    }
  }

  private handleTransportEnvelope(ciphertext: Uint8Array): void {
    const peer = this.getSolePeer();
    if (!peer) return;

    let plaintext: Uint8Array;
    try {
      plaintext = peer.recv(ciphertext);
    } catch {
      return; // not ready, or decryption failed — drop silently
    }

    const msg: RelayMessage = {
      from: peer.id,
      payload: bytesToString(plaintext),
      rawPayload: plaintext,
      timestamp: new Date(),
    };
    this.emit('message', msg);
  }

  /** v1 supports exactly one linked peer per device (docs/PROTOCOL.md §5.3).
   * Incoming binary envelopes carry no sender field — the relay only ever forwards
   * them from the paired device. */
  private getSolePeer(): PeerConnection | undefined {
    return this.peerConns.values().next().value;
  }

  private sendControl(frame: Partial<WireMessage>): void {
    this.transport.send(JSON.stringify(frame));
  }

  private sendBinary(bytes: Uint8Array): void {
    this.transport.send(bytes);
  }

  private startPingLoop(): void {
    if (this.pingTimer !== null) clearInterval(this.pingTimer);
    this.pingTimer = setInterval(() => {
      try {
        this.sendControl({ type: MSG.PING });
      } catch {
        // Connection may have dropped — transport will reconnect
      }
    }, this.opts.pingIntervalMs);
  }
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function normalizeUrl(url: string): string {
  if (url.startsWith('http://')) return url.replace('http://', 'ws://');
  if (url.startsWith('https://')) return url.replace('https://', 'wss://');
  return url;
}

function buildQrUrl(serverUrl: string, code: string): string {
  try {
    const u = new URL(serverUrl.replace(/^ws/, 'https'));
    u.pathname = '/pair';
    u.searchParams.set('code', code);
    return u.toString().replace(/^https/, serverUrl.startsWith('wss') ? 'wss' : 'ws');
  } catch {
    return `${serverUrl}/pair?code=${code}`;
  }
}
