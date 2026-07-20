/**
 * Shared TypeScript types for the relayly library.
 */
import type { PeerKeyStore } from './peerStore.js';

// ─── Keys ────────────────────────────────────────────────────────────────────

/**
 * A raw 32-byte Uint8Array representing an X25519 key (public or private).
 */
export type RawKey = Uint8Array;

/**
 * A Relayly keypair — the device's static identity, used as the Noise XX static
 * keypair (docs/PROTOCOL.md §6) for device-to-device end-to-end encryption.
 */
export interface KeyPair {
  /** 32-byte X25519 private key (Curve25519) */
  privateKey: RawKey;
  /** 32-byte X25519 public key */
  publicKey: RawKey;
}

// ─── Pairing ─────────────────────────────────────────────────────────────────

/**
 * A pairing code returned by requestPairCode().
 * Display the short code or render the qrCodeUrl as a QR image.
 */
export interface PairCode {
  /** 6-digit short code to share out-of-band. e.g. "483921" */
  shortCode: string;
  /** Seconds until this code expires. */
  expiresIn: number;
  /** A URL encoding both the server and code, suitable for a QR image. */
  qrCodeUrl: string;
}

/**
 * A paired remote device, with its Noise handshake already complete and pinned
 * (docs/PROTOCOL.md §7.1) — safe to send() to immediately.
 */
export interface Peer {
  /** The device identifier registered with the server. */
  id: string;
  /** 32-byte X25519 public key of the remote device, as authenticated by the Noise
   * handshake (not merely the server's announced value). */
  publicKey: RawKey;
}

// ─── Messages ────────────────────────────────────────────────────────────────

/**
 * An incoming decrypted message from a paired peer.
 */
export interface RelayMessage {
  /** The device ID of the sender. */
  from: string;
  /** The decrypted plaintext as a UTF-8 string. */
  payload: string;
  /** Raw decrypted bytes (same data as payload, in Uint8Array form). */
  rawPayload: Uint8Array;
  /** When this client received and decrypted the message. The E2E transport
   * envelope (docs/PROTOCOL.md §6) carries no timestamp of its own, so this is a
   * local receipt time, not a server-assigned one. */
  timestamp: Date;
}

// ─── Events ──────────────────────────────────────────────────────────────────

/**
 * Events emitted by RelaylyClient.
 */
export interface RelaylyClientEvents {
  /** Fired when a message is received from a paired peer. */
  message: (msg: RelayMessage) => void;
  /** Fired when pairing is complete (either side) and the Noise handshake is up. */
  paired: (peer: Peer) => void;
  /** Fired whenever a peer's session becomes usable for send() — both after the
   * first pairing and after any later re-handshake following a reconnect
   * (docs/PROTOCOL.md §6). */
  ready: (peerId: string) => void;
  /** Fired when the server reports the paired peer's online/offline transition. */
  peerStatus: (peerId: string, online: boolean) => void;
  /** Fired when the WebSocket connection is established (or re-established). */
  connected: () => void;
  /** Fired when the connection drops. Will attempt reconnect unless closed. */
  disconnected: (reason: string) => void;
  /** Fired before each reconnect attempt. */
  reconnecting: (attempt: number) => void;
  /** Fired on any server error. */
  error: (err: RelaylyError) => void;
}

// ─── Errors ──────────────────────────────────────────────────────────────────

/**
 * A typed error returned by the Relayly server.
 */
export interface RelaylyError extends Error {
  /** Machine-readable error code from the server, e.g. "peer_not_found" */
  code: string;
}

// ─── Options ─────────────────────────────────────────────────────────────────

/**
 * Options for constructing a RelaylyClient.
 */
export interface RelaylyClientOptions {
  /** Unique identifier for this device. Required. */
  deviceId: string;
  /** Authenticates this device to the relay (docs/PROTOCOL.md §2, §3). Obtain one
   * from POST /api/v1/devices (or the relayly CLI's `pair` command). Required. */
  deviceToken: string;
  /**
   * The device's keypair. Use generateKey() to create one.
   * Persist the private key across sessions for a stable identity.
   */
  keyPair: KeyPair;
  /**
   * Where pinned peer static keys (docs/PROTOCOL.md §7.1) are persisted. Defaults to
   * an in-memory store (does not survive a reload/restart, logs a warning). Pass a
   * `FilePeerKeyStore` from `relayly/node` under Node.js for a persistent,
   * cross-SDK-compatible pin.
   */
  peerStore?: PeerKeyStore;
  /**
   * How often to send keepalive pings (ms). Default: 30_000.
   */
  pingIntervalMs?: number;
  /**
   * Initial reconnect delay in ms. Default: 1_000.
   * Set to 0 to disable automatic reconnection.
   */
  reconnectDelayMs?: number;
  /**
   * Maximum reconnect delay in ms. Default: 60_000.
   */
  maxReconnectDelayMs?: number;
}

// ─── Wire Protocol ───────────────────────────────────────────────────────────

/** One entry of welcome's peers array. */
export interface WirePeer {
  id: string;
  static_key: string;
}

/**
 * Internal control-channel frame (docs/PROTOCOL.md §5) — not exported as part of
 * the public API. Carried on WebSocket text frames only; encrypted application data
 * travels as a binary E2E envelope (see noise/session.ts), never as JSON.
 */
export interface WireMessage {
  type: string;
  protocol_version?: number;
  device_id?: string;
  peers?: WirePeer[];
  static_key?: string;
  code?: string;
  expires_in?: number;
  peer_id?: string;
  peer_static_key?: string;
  online?: boolean;
  message?: string;
}
