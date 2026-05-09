/**
 * WebSocket transport with automatic exponential-backoff reconnection.
 * This is an internal module — not exported as part of the public API.
 */

const DEFAULT_RECONNECT_DELAY_MS = 1_000;
const DEFAULT_MAX_RECONNECT_DELAY_MS = 60_000;

export type TransportMessage = string;

export interface TransportOptions {
  reconnectDelayMs?: number;
  maxReconnectDelayMs?: number;
  onOpen?: () => void;
  onClose?: (reason: string) => void;
  onMessage?: (data: string) => void;
  onReconnecting?: (attempt: number) => void;
}

/**
 * WebSocketTransport manages the raw WebSocket lifecycle, including reconnection.
 */
export class WebSocketTransport {
  private url: string;
  private opts: Required<TransportOptions>;
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempt = 0;
  private currentDelay: number;
  private _closed = false;

  constructor(url: string, opts: TransportOptions = {}) {
    this.url = url;
    this.opts = {
      reconnectDelayMs: opts.reconnectDelayMs ?? DEFAULT_RECONNECT_DELAY_MS,
      maxReconnectDelayMs: opts.maxReconnectDelayMs ?? DEFAULT_MAX_RECONNECT_DELAY_MS,
      onOpen: opts.onOpen ?? (() => {}),
      onClose: opts.onClose ?? (() => {}),
      onMessage: opts.onMessage ?? (() => {}),
      onReconnecting: opts.onReconnecting ?? (() => {}),
    };
    this.currentDelay = this.opts.reconnectDelayMs;
  }

  /** Connect (or reconnect) the WebSocket. */
  connect(): void {
    if (this._closed) return;

    const ws = new WebSocket(this.url);
    this.ws = ws;

    ws.addEventListener('open', () => {
      this.reconnectAttempt = 0;
      this.currentDelay = this.opts.reconnectDelayMs;
      this.opts.onOpen();
    });

    ws.addEventListener('close', (event) => {
      if (this._closed) return;
      const reason = event.reason || `code ${event.code}`;
      this.opts.onClose(reason);
      this.scheduleReconnect();
    });

    ws.addEventListener('error', () => {
      // The 'close' event will follow, so we just let that handle reconnect.
    });

    ws.addEventListener('message', (event) => {
      if (typeof event.data === 'string') {
        this.opts.onMessage(event.data);
      }
    });
  }

  /** Send a raw string message. */
  send(data: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('relayly: WebSocket is not connected');
    }
    this.ws.send(data);
  }

  /** Returns true if the WebSocket is currently open. */
  get isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }

  /** Close the transport permanently — no further reconnects will happen. */
  close(): void {
    this._closed = true;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close(1000, 'Client closed');
      this.ws = null;
    }
  }

  private scheduleReconnect(): void {
    if (this._closed || this.opts.reconnectDelayMs === 0) return;

    this.reconnectAttempt++;
    this.opts.onReconnecting(this.reconnectAttempt);

    this.reconnectTimer = setTimeout(() => {
      this.currentDelay = Math.min(
        this.currentDelay * 2,
        this.opts.maxReconnectDelayMs,
      );
      this.connect();
    }, this.currentDelay);
  }
}
