use thiserror::Error;

#[derive(Debug, Error)]
pub enum Error {
    #[error("connection error: {0}")]
    Connection(String),
    #[error("auth failed: {0}")]
    Auth(String),
    #[error("no paired peer: {0}")]
    PeerNotFound(String),
    #[error("crypto error: {0}")]
    Crypto(String),
    #[error("client closed")]
    Closed,
    #[error("operation timed out")]
    Timeout,
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    // Relay control-channel error codes (docs/PROTOCOL.md §5.1).
    #[error("invalid pairing code{}", suffix(.0))]
    InvalidCode(String),
    #[error("pairing code expired{}", suffix(.0))]
    CodeExpired(String),
    #[error("device already paired{}", suffix(.0))]
    AlreadyPaired(String),
    #[error("peer is offline{}", suffix(.0))]
    PeerOffline(String),
    #[error("rate limited{}", suffix(.0))]
    RateLimited(String),
    #[error("malformed request{}", suffix(.0))]
    Malformed(String),
    #[error("internal server error{}", suffix(.0))]
    Internal(String),
    /// The server-announced-key lock (§7.2) rejecting a different key than the one
    /// already recorded for this device.
    #[error("announced static key does not match the server's record{}", suffix(.0))]
    KeyMismatch(String),

    // SDK-local errors, not server error codes.
    /// The peer's Noise session isn't up yet — notably during the brief window after
    /// a reconnect forces a re-handshake (§6). request_pair_code/accept_pair/
    /// PairCode::wait never return until the very first handshake completes, so this
    /// should not occur on a freshly paired peer.
    #[error("peer session is not ready")]
    NotReady,
    /// The client-side pin check (§7.1, the real security boundary) rejecting a peer
    /// whose authenticated static key differs from the one already pinned for that
    /// peer ID. Unpinning is an explicit user action only; this error is never
    /// auto-retried.
    #[error("peer's authenticated key does not match the pinned key ({0})")]
    PeerKeyMismatch(String),
}

fn suffix(message: &str) -> String {
    if message.is_empty() {
        String::new()
    } else {
        format!(": {message}")
    }
}

/// Maps a control-channel error's machine code (docs/PROTOCOL.md §5.1) to a typed
/// Error variant, wrapped with the human-readable message from the server.
pub fn error_for_code(code: &str, message: &str) -> Error {
    let message = message.to_string();
    match code {
        "invalid_code" => Error::InvalidCode(message),
        "code_expired" => Error::CodeExpired(message),
        "already_paired" => Error::AlreadyPaired(message),
        "peer_offline" => Error::PeerOffline(message),
        "rate_limited" => Error::RateLimited(message),
        "malformed" => Error::Malformed(message),
        "key_mismatch" => Error::KeyMismatch(message),
        _ => Error::Internal(message),
    }
}
