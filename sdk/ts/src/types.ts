/**
 * Shared TypeScript types for the relayly-client library.
 */

// ─── Keys ────────────────────────────────────────────────────────────────────

/**
 * A raw 32-byte Uint8Array representing an X25519 key (public or private).
 */
export type RawKey = Uint8Array;

/**
 * A Relayly keypair — private key is used for signing/decryption,
 * public key is shared with peers during pairing.
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
 * A paired remote device.
 */
export interface Peer {
  /** The device identifier registered with the server. */
  id: string;
  /** 32-byte X25519 public key of the remote device. */
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
  /** Server-assigned receive timestamp. */
  timestamp: Date;
}

// ─── Events ──────────────────────────────────────────────────────────────────

/**
 * Events emitted by RelaylyClient.
 */
export interface RelaylyClientEvents {
  /** Fired when a message is received from a paired peer. */
  message: (msg: RelayMessage) => void;
  /** Fired when pairing is complete (either side). */
  paired: (peer: Peer) => void;
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
  /**
   * The device's keypair. Use generateKey() to create one.
   * Persist the private key across sessions for a stable identity.
   */
  keyPair: KeyPair;
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

/** Internal wire frame — not exported as part of the public API. */
export interface WireMessage {
  type: string;
  device_id?: string;
  public_key?: string;
  session_id?: string;
  code?: string;
  expires_in?: number;
  peer_id?: string;
  peer_public_key?: string;
  to?: string;
  from?: string;
  payload?: string;
  nonce?: string;
  timestamp?: string;
  error_code?: string;
  message?: string;
}
