/**
 * Base class for every typed error this SDK throws/rejects with. Named
 * RelaylyClientError (not RelaylyError) to avoid colliding with the public
 * `RelaylyError` interface in types.ts — every instance here still has a `.code`
 * string, so it satisfies that interface's shape wherever one is expected.
 */
export class RelaylyClientError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message);
    this.name = this.constructor.name;
  }
}

// ── Server control-channel error codes (docs/PROTOCOL.md §5.1) ────────────────

export class InvalidCodeError extends RelaylyClientError {
  constructor(message: string) {
    super('invalid_code', message);
  }
}
export class CodeExpiredError extends RelaylyClientError {
  constructor(message: string) {
    super('code_expired', message);
  }
}
export class AlreadyPairedError extends RelaylyClientError {
  constructor(message: string) {
    super('already_paired', message);
  }
}
export class PeerOfflineError extends RelaylyClientError {
  constructor(message: string) {
    super('peer_offline', message);
  }
}
export class RateLimitedError extends RelaylyClientError {
  constructor(message: string) {
    super('rate_limited', message);
  }
}
export class MalformedError extends RelaylyClientError {
  constructor(message: string) {
    super('malformed', message);
  }
}
export class InternalError extends RelaylyClientError {
  constructor(message: string) {
    super('internal', message);
  }
}
/** Server-side announced-key lock (§7.2) rejecting a different key than recorded. */
export class KeyMismatchError extends RelaylyClientError {
  constructor(message: string) {
    super('key_mismatch', message);
  }
}

/** Maps a control-channel error's machine code to a typed error. */
export function errorForCode(code: string, message: string): RelaylyClientError {
  switch (code) {
    case 'invalid_code':
      return new InvalidCodeError(message);
    case 'code_expired':
      return new CodeExpiredError(message);
    case 'already_paired':
      return new AlreadyPairedError(message);
    case 'peer_offline':
      return new PeerOfflineError(message);
    case 'rate_limited':
      return new RateLimitedError(message);
    case 'malformed':
      return new MalformedError(message);
    case 'key_mismatch':
      return new KeyMismatchError(message);
    default:
      return new InternalError(message || code);
  }
}

// ── SDK-local errors ────────────────────────────────────────────────────────────

/**
 * Thrown by send() when a peer's Noise session isn't up yet — in normal use this
 * only happens in the brief window after a reconnect forces a re-handshake (§6);
 * requestPairCode/acceptPair/waitForPairing already block until the first handshake
 * completes.
 */
export class NotReadyError extends RelaylyClientError {
  constructor(peerId: string) {
    super('not_ready', `relayly: peer ${peerId}'s session is not ready`);
  }
}

/**
 * Thrown when a peer's authenticated static key differs from the one already
 * pinned for that peer ID (§7.1, the real security boundary). Unpinning is an
 * explicit user action only; this is never auto-retried.
 */
export class PeerKeyMismatchError extends RelaylyClientError {
  constructor(peerId: string) {
    super('peer_key_mismatch', `relayly: peer ${peerId}'s authenticated key does not match the pinned key`);
  }
}
