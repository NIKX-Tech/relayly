/**
 * RelaylyClient — the main client class for connecting to a Relayly relay server.
 *
 * @example
 * ```ts
 * import { RelaylyClient, generateKey } from 'relayly-client';
 *
 * const keyPair = generateKey();
 * const client = new RelaylyClient('wss://relay.example.com', {
 *   deviceId: 'my-browser',
 *   keyPair,
 * });
 *
 * await client.connect();
 *
 * const { shortCode } = await client.requestPairCode();
 * console.log('Share this code:', shortCode);
 *
 * const peer = await client.waitForPairing();
 * await client.send(peer.id, 'Hello!');
 *
 * client.on('message', (msg) => {
 *   console.log(`[${msg.from}]`, msg.payload);
 * });
 * ```
 */
import { WebSocketTransport } from './transport.js';
import { encrypt, decrypt, encodeBase64, decodeBase64, stringToBytes, bytesToString } from './crypto.js';
import type {
  KeyPair,
  Peer,
  PairCode,
  RelayMessage,
  RelaylyClientOptions,
  RelaylyClientEvents,
  RelaylyError,
  WireMessage,
} from './types.js';

// ─── Wire message type constants ──────────────────────────────────────────────

const MSG = {
  AUTH: 'auth',
  AUTH_OK: 'auth_ok',
  PAIR_REQUEST: 'pair_request',
  PAIR_CODE: 'pair_code',
  PAIR_ACCEPT: 'pair_accept',
  PAIR_COMPLETE: 'pair_complete',
  SEND: 'send',
  MESSAGE: 'message',
  PING: 'ping',
  PONG: 'pong',
  ERROR: 'error',
} as const;

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

// ─── Pending pair code state ──────────────────────────────────────────────────

interface PendingPair {
  resolve: (peer: Peer) => void;
  reject: (err: Error) => void;
  code?: string; // set after pair_code response
}

// ─── RelaylyClient ────────────────────────────────────────────────────────────

/**
 * RelaylyClient connects to a Relayly relay server and provides a simple API
 * for device pairing and end-to-end encrypted messaging.
 */
export class RelaylyClient extends TinyEmitter {
  private readonly serverUrl: string;
  private readonly opts: Required<RelaylyClientOptions>;
  private transport: WebSocketTransport;

  /** Map of peer ID → Peer (populated after successful pairing) */
  private peers = new Map<string, Peer>();

  /** Pending pair request (initiating side) */
  private pendingPairRequest: PendingPair | null = null;

  /** Pending pair accepts: code → PendingPair (accepting side) */
  private pendingPairAccepts = new Map<string, PendingPair>();

  /** Resolves when authenticated after (re)connect */
  private authResolve: (() => void) | null = null;
  private authReject: ((err: Error) => void) | null = null;

  private pingTimer: ReturnType<typeof setInterval> | null = null;

  constructor(serverUrl: string, opts: RelaylyClientOptions) {
    super();
    this.serverUrl = normalizeUrl(serverUrl);
    this.opts = {
      deviceId: opts.deviceId,
      keyPair: opts.keyPair,
      pingIntervalMs: opts.pingIntervalMs ?? DEFAULT_PING_INTERVAL_MS,
      reconnectDelayMs: opts.reconnectDelayMs ?? 1_000,
      maxReconnectDelayMs: opts.maxReconnectDelayMs ?? 60_000,
    };

    this.transport = new WebSocketTransport(this.serverUrl, {
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
   * Connect to the Relayly server and authenticate.
   * Resolves when the connection is established and authentication succeeds.
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
   * // <img src={`https://api.qrserver.com/v1/create-qr-code/?data=${qrCodeUrl}`} />
   */
  async requestPairCode(): Promise<PairCode> {
    return new Promise<PairCode>((resolve, reject) => {
      this.pendingPairRequest = {
        resolve: () => {}, // will be set to real resolve after we get the code
        reject,
      };

      // We intercept the pair_code frame in handleMessage to get the code first
      const originalReject = reject;
      this.pendingPairRequest.reject = originalReject;

      // Store a modified resolve that returns a PairCode
      (this.pendingPairRequest as PendingPair & { _resolveCode?: (code: PairCode) => void })
        ._resolveCode = resolve;

      this.sendFrame({ type: MSG.PAIR_REQUEST });
    });
  }

  /**
   * Accept a pairing code received from another device.
   * Returns the newly paired Peer once the server confirms.
   *
   * @example
   * const peer = await client.acceptPair('483921');
   * console.log('Paired with', peer.id);
   */
  async acceptPair(code: string): Promise<Peer> {
    return new Promise<Peer>((resolve, reject) => {
      this.pendingPairAccepts.set(code, { resolve, reject, code });
      this.sendFrame({ type: MSG.PAIR_ACCEPT, code });
    });
  }

  /**
   * Wait for an incoming pair_complete from any device (after requestPairCode).
   * This is an alias for listening to the 'paired' event once.
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
   * @param peerId   The device ID of the recipient (must be paired).
   * @param message  Either a string or raw bytes.
   *
   * @example
   * await client.send(peer.id, 'Hello from the browser!');
   */
  async send(peerId: string, message: string | Uint8Array): Promise<void> {
    const peer = this.peers.get(peerId);
    if (!peer) {
      throw new Error(
        `relayly: no paired peer with ID "${peerId}" — call requestPairCode or acceptPair first`,
      );
    }

    const plaintextBytes: Uint8Array =
      typeof message === 'string' ? stringToBytes(message) : message;
    const { ciphertext, nonce } = encrypt(plaintextBytes, peer.publicKey, this.opts.keyPair.privateKey);

    this.sendFrame({
      type: MSG.SEND,
      to: peerId,
      payload: encodeBase64(ciphertext),
      nonce: encodeBase64(nonce),
    });
  }

  /**
   * Returns all currently paired peers.
   */
  getPeers(): ReadonlyMap<string, Peer> {
    return this.peers;
  }

  /**
   * Permanently close the connection. No further reconnects will occur.
   */
  disconnect(): void {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    this.transport.close();
  }

  // ─── Event listener overrides (typed) ─────────────────────────────────────

  on<K extends EventKey>(event: K, listener: EventMap[K]): this {
    return super.on(event, listener);
  }

  off<K extends EventKey>(event: K, listener: EventMap[K]): this {
    return super.off(event, listener);
  }

  once<K extends EventKey>(event: K, listener: EventMap[K]): this {
    return super.once(event, listener);
  }

  // ─── Internal handlers ─────────────────────────────────────────────────────

  private handleOpen(): void {
    // Authenticate immediately
    this.sendFrame({
      type: MSG.AUTH,
      device_id: this.opts.deviceId,
      public_key: encodeBase64(this.opts.keyPair.publicKey),
    });
  }

  private handleClose(reason: string): void {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    this.emit('disconnected', reason);
  }

  private handleMessage(raw: string): void {
    let frame: WireMessage;
    try {
      frame = JSON.parse(raw) as WireMessage;
    } catch {
      return; // skip malformed frames
    }

    switch (frame.type) {
      case MSG.AUTH_OK:
        this.startPingLoop();
        this.emit('connected');
        this.authResolve?.();
        this.authResolve = null;
        this.authReject = null;
        break;

      case MSG.PAIR_CODE:
        this.handlePairCode(frame);
        break;

      case MSG.PAIR_COMPLETE:
        this.handlePairComplete(frame);
        break;

      case MSG.MESSAGE:
        this.handleIncomingMessage(frame);
        break;

      case MSG.ERROR:
        this.handleError(frame);
        break;

      case MSG.PONG:
        break; // keepalive, nothing to do

      default:
        break;
    }
  }

  private handlePairCode(frame: WireMessage): void {
    const pending = this.pendingPairRequest as
      | (PendingPair & { _resolveCode?: (code: PairCode) => void })
      | null;
    if (!pending || !frame.code) return;

    // Now we have the code — register it so pair_complete can match it
    pending.code = frame.code;

    const pairCode: PairCode = {
      shortCode: frame.code,
      expiresIn: frame.expires_in ?? 300,
      qrCodeUrl: buildQrUrl(this.serverUrl, frame.code),
    };

    pending._resolveCode?.(pairCode);
  }

  private handlePairComplete(frame: WireMessage): void {
    if (!frame.peer_id || !frame.peer_public_key) return;

    const peerPublicKey = decodeBase64(frame.peer_public_key);
    const peer: Peer = {
      id: frame.peer_id,
      publicKey: peerPublicKey,
    };
    this.peers.set(peer.id, peer);
    this.emit('paired', peer);

    // Resolve pending pair request (initiating side)
    const pendingReq = this.pendingPairRequest;
    if (pendingReq !== null && pendingReq.code === frame.code) {
      this.pendingPairRequest = null;
      pendingReq.resolve(peer);
    }

    // Resolve pending pair accept (accepting side)
    if (frame.code) {
      const accept = this.pendingPairAccepts.get(frame.code);
      if (accept) {
        accept.resolve(peer);
        this.pendingPairAccepts.delete(frame.code);
      }
    }
  }

  private handleIncomingMessage(frame: WireMessage): void {
    if (!frame.from || !frame.payload || !frame.nonce) return;

    const peer = this.peers.get(frame.from);
    if (!peer) return; // unknown sender, drop

    try {
      const ciphertext = decodeBase64(frame.payload);
      const nonce = decodeBase64(frame.nonce);
      const rawPayload = decrypt(ciphertext, nonce, peer.publicKey, this.opts.keyPair.privateKey);
      const payload: string = bytesToString(rawPayload);

      const msg: RelayMessage = {
        from: frame.from,
        payload,
        rawPayload,
        timestamp: frame.timestamp ? new Date(frame.timestamp) : new Date(),
      };

      this.emit('message', msg);
    } catch {
      // Decryption failed — drop silently
    }
  }

  private handleError(frame: WireMessage): void {
    const err = Object.assign(
      new Error(frame.message ?? 'Unknown relay error'),
      { code: frame.error_code ?? 'unknown' },
    ) as RelaylyError;

    this.emit('error', err);

    // Reject any pending pair operations
    if (this.authReject) {
      this.authReject(err);
      this.authResolve = null;
      this.authReject = null;
    }
    this.pendingPairRequest?.reject(err);
    this.pendingPairRequest = null;
    this.pendingPairAccepts.forEach((p) => p.reject(err));
    this.pendingPairAccepts.clear();
  }

  private sendFrame(frame: Partial<WireMessage>): void {
    this.transport.send(JSON.stringify(frame));
  }

  private startPingLoop(): void {
    if (this.pingTimer !== null) clearInterval(this.pingTimer);
    this.pingTimer = setInterval(() => {
      try {
        this.sendFrame({ type: MSG.PING });
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
