/**
 * Optional React hooks for relayly-client.
 *
 * Import from 'relayly-client/react':
 *
 * @example
 * ```tsx
 * import { useRelayly, usePairing } from 'relayly-client/react';
 *
 * function Chat({ client, peerId }: { client: RelaylyClient; peerId: string }) {
 *   const { messages, send } = useRelayly(client, peerId);
 *   const { shortCode, qrCodeUrl, status } = usePairing(client);
 *   ...
 * }
 * ```
 */
import { useEffect, useRef, useState, useCallback } from 'react';
import type { RelaylyClient } from './client.js';
import type { RelayMessage, Peer, PairCode } from './types.js';

// ─── usePairing ───────────────────────────────────────────────────────────────

type PairingStatus = 'idle' | 'requesting' | 'waiting' | 'paired' | 'error';

interface UsePairingResult {
  /** Current pairing flow status */
  status: PairingStatus;
  /** The 6-digit short code (available after requestCode() is called) */
  shortCode: string | null;
  /** A URL suitable for a QR code image */
  qrCodeUrl: string | null;
  /** How many seconds until the code expires */
  expiresIn: number;
  /** The peer that completed pairing (available after status === 'paired') */
  peer: Peer | null;
  /** Call to initiate pairing and get a code */
  requestCode: () => Promise<void>;
  /** Reset back to idle state */
  reset: () => void;
}

/**
 * React hook to manage the pairing code flow.
 *
 * @example
 * ```tsx
 * const { status, shortCode, qrCodeUrl, peer, requestCode } = usePairing(client);
 *
 * useEffect(() => { requestCode(); }, []);
 *
 * if (status === 'waiting') return <QRCode value={qrCodeUrl!} />;
 * if (status === 'paired') return <Chat peer={peer!} />;
 * ```
 */
export function usePairing(client: RelaylyClient): UsePairingResult {
  const [status, setStatus] = useState<PairingStatus>('idle');
  const [pairCode, setPairCode] = useState<PairCode | null>(null);
  const [peer, setPeer] = useState<Peer | null>(null);

  useEffect(() => {
    const onPaired = (p: Peer) => {
      setPeer(p);
      setStatus('paired');
    };
    client.on('paired', onPaired);
    return () => { client.off('paired', onPaired); };
  }, [client]);

  const requestCode = useCallback(async () => {
    setStatus('requesting');
    try {
      const code = await client.requestPairCode();
      setPairCode(code);
      setStatus('waiting');
    } catch (err) {
      setStatus('error');
      throw err;
    }
  }, [client]);

  const reset = useCallback(() => {
    setStatus('idle');
    setPairCode(null);
    setPeer(null);
  }, []);

  return {
    status,
    shortCode: pairCode?.shortCode ?? null,
    qrCodeUrl: pairCode?.qrCodeUrl ?? null,
    expiresIn: pairCode?.expiresIn ?? 0,
    peer,
    requestCode,
    reset,
  };
}

// ─── useRelayly ───────────────────────────────────────────────────────────────

interface UseRelaylyResult {
  /** All received messages (from any peer, or filtered by peerId) */
  messages: RelayMessage[];
  /** Send a message to the given peer */
  send: (text: string) => Promise<void>;
  /** Whether the client is currently connected */
  isConnected: boolean;
  /** Clear the message history */
  clearMessages: () => void;
}

/**
 * React hook for sending and receiving messages with a specific peer.
 *
 * @example
 * ```tsx
 * const { messages, send } = useRelayly(client, peer.id);
 *
 * return (
 *   <>
 *     {messages.map((m, i) => <p key={i}>[{m.from}] {m.payload}</p>)}
 *     <button onClick={() => send('Hi!')}>Send</button>
 *   </>
 * );
 * ```
 */
export function useRelayly(client: RelaylyClient, peerId?: string): UseRelaylyResult {
  const [messages, setMessages] = useState<RelayMessage[]>([]);
  const [isConnected, setIsConnected] = useState(false);

  useEffect(() => {
    const onMessage = (msg: RelayMessage) => {
      if (peerId && msg.from !== peerId) return;
      setMessages((prev: RelayMessage[]) => [...prev, msg]);
    };
    const onConnected = () => setIsConnected(true);
    const onDisconnected = () => setIsConnected(false);

    client.on('message', onMessage);
    client.on('connected', onConnected);
    client.on('disconnected', onDisconnected);

    return () => {
      client.off('message', onMessage);
      client.off('connected', onConnected);
      client.off('disconnected', onDisconnected);
    };
  }, [client, peerId]);

  const send = useCallback(
    async (text: string) => {
      if (!peerId) throw new Error('relayly: peerId is required to send');
      await client.send(peerId, text);
    },
    [client, peerId],
  );

  const clearMessages = useCallback(() => setMessages([]), []);

  return { messages, send, isConnected, clearMessages };
}

// ─── useRelaylyConnection ─────────────────────────────────────────────────────

interface UseRelaylyConnectionResult {
  client: RelaylyClient;
  isConnected: boolean;
  reconnectAttempt: number;
}

/**
 * React hook that manages the client connection lifecycle,
 * returning the current connection status and reconnect attempt count.
 *
 * @example
 * ```tsx
 * const { client, isConnected, reconnectAttempt } = useRelaylyConnection(client);
 * if (!isConnected) return <p>Reconnecting (attempt {reconnectAttempt})…</p>;
 * ```
 */
export function useRelaylyConnection(client: RelaylyClient): UseRelaylyConnectionResult {
  const [isConnected, setIsConnected] = useState(false);
  const [reconnectAttempt, setReconnectAttempt] = useState(0);
  const clientRef = useRef(client);
  clientRef.current = client;

  useEffect(() => {
    const onConnected = () => { setIsConnected(true); setReconnectAttempt(0); };
    const onDisconnected = () => setIsConnected(false);
    const onReconnecting = (n: number) => setReconnectAttempt(n);

    client.on('connected', onConnected);
    client.on('disconnected', onDisconnected);
    client.on('reconnecting', onReconnecting);

    return () => {
      client.off('connected', onConnected);
      client.off('disconnected', onDisconnected);
      client.off('reconnecting', onReconnecting);
    };
  }, [client]);

  return { client, isConnected, reconnectAttempt };
}
