//! PeerConn tracks everything this client knows about one paired device: its
//! authenticated static key once known, the currently active Noise session, and
//! (while a rekey is in flight) a pending replacement session that must not replace
//! `active` until it actually completes — docs/PROTOCOL.md §6's make-before-break
//! rule. Mutated only while the caller holds the write lock on the peers map (see
//! client.rs's `Shared::peers: RwLock<HashMap<String, PeerConn>>`), so it needs no
//! internal locking of its own.

use std::time::{Duration, Instant};

use tokio::sync::oneshot;

use crate::{
    crypto::{PrivateKey, PublicKey},
    noise::NoiseSession,
    Error,
};

/// Throttles how often this client starts a brand new responder session for an
/// unsolicited msg1 arriving on an already-healthy peer connection. The relay is
/// untrusted (docs/PROTOCOL.md §6, §7); without this a malicious/compromised relay
/// could force perpetual handshake churn by injecting 0x01 frames.
const UNSOLICITED_MSG1_MIN_INTERVAL: Duration = Duration::from_secs(2);

/// What a pending request_pair_code/accept_pair call resolves with. Not every field
/// is populated in every context: the pair_code response only sets `code`/
/// `expires_in`; the pair_complete response (surfaced once the resulting Noise
/// handshake actually finishes) only sets `peer_id`/`peer_public_key`.
#[derive(Default)]
pub struct PairResult {
    pub code: String,
    pub expires_in: u64,
    pub peer_id: String,
    pub peer_public_key: Option<PublicKey>,
    pub error: Option<Error>,
}

/// Result of feeding one handshake envelope to a `PeerConn` (see
/// `handle_handshake_envelope`). Carries everything the caller needs to finish
/// resolving a completed handshake without needing ownership of the `NoiseSession`
/// itself — `PeerConn` keeps that, and exposes `promote_pending`/`abandon_pending` for
/// the caller to decide its fate.
pub struct HandshakeOutcome {
    pub reply: Option<Vec<u8>>,
    pub done: bool,
    /// True if this was a rekey attempt (vs. the peer's first-ever handshake).
    pub was_pending: bool,
    /// The authenticated peer static key. Set only when `done` and the handshake
    /// succeeded.
    pub peer_static_key: Option<Vec<u8>>,
    /// Set when `done` and the handshake failed.
    pub failed: bool,
}

impl HandshakeOutcome {
    fn dropped() -> Self {
        Self { reply: None, done: false, was_pending: false, peer_static_key: None, failed: false }
    }
}

pub struct PeerConn {
    pub announced_static_key: String,

    active: Option<NoiseSession>,
    pending: Option<NoiseSession>,

    /// Notified exactly once, when this peer's very first handshake resolves
    /// (successfully or not). Cleared on first use.
    first_pair_waiter: Option<oneshot::Sender<PairResult>>,

    last_unsolicited_msg1: Option<Instant>,
}

impl PeerConn {
    pub fn new(announced_static_key: String) -> Self {
        Self {
            announced_static_key,
            active: None,
            pending: None,
            first_pair_waiter: None,
            last_unsolicited_msg1: None,
        }
    }

    pub fn set_first_pair_waiter(&mut self, waiter: oneshot::Sender<PairResult>) {
        self.first_pair_waiter = Some(waiter);
    }

    pub fn take_first_pair_waiter(&mut self) -> Option<oneshot::Sender<PairResult>> {
        self.first_pair_waiter.take()
    }

    /// Begins the very first handshake for this peer (§5.3: the accepting device
    /// initiates). Returns the msg1 payload to send.
    pub fn start_as_initiator(&mut self, priv_key: &PrivateKey) -> Result<Vec<u8>, Error> {
        let (session, msg1) = NoiseSession::as_initiator(priv_key)?;
        self.active = Some(session);
        Ok(msg1)
    }

    /// Begins a replacement handshake (§6: triggered when this device's ID is
    /// lexicographically smaller than the peer's, on any reconnect). The existing
    /// active session, if any, keeps working until the replacement completes.
    pub fn start_rekey_as_initiator(&mut self, priv_key: &PrivateKey) -> Result<Vec<u8>, Error> {
        let (session, msg1) = NoiseSession::as_initiator(priv_key)?;
        self.pending = Some(session);
        Ok(msg1)
    }

    /// Feeds one received ENVELOPE_HANDSHAKE payload to the right session, starting a
    /// new responder session if needed and applying make-before-break plus
    /// unsolicited-msg1 rate limiting.
    #[allow(clippy::unnecessary_unwrap)] // each `.unwrap()` follows a branch-specific
    // condition in an if/else-if chain that decides which session to drive; the
    // conditions aren't independent of each other, so `if let` per-branch would need
    // duplicating the "which branch matched" logic rather than simplifying it.
    pub fn handle_handshake_envelope(&mut self, priv_key: &PrivateKey, data: &[u8]) -> HandshakeOutcome {
        let mut was_pending = false;

        let session: &mut NoiseSession = if self.pending.is_some() {
            was_pending = true;
            self.pending.as_mut().unwrap()
        } else if self.active.as_ref().is_some_and(|a| !a.ready()) {
            // Continuing the (first-ever) in-progress handshake.
            self.active.as_mut().unwrap()
        } else if self.active.is_none() {
            // No session at all yet: this incoming msg1 starts the very first
            // handshake for this peer, with us as responder.
            let responder = match NoiseSession::as_responder(priv_key) {
                Ok(s) => s,
                Err(_) => return HandshakeOutcome::dropped(),
            };
            self.active = Some(responder);
            self.active.as_mut().unwrap()
        } else {
            // active exists and is ready: an unsolicited msg1 on a healthy connection.
            let now = Instant::now();
            let limited = self
                .last_unsolicited_msg1
                .is_some_and(|last| now.duration_since(last) < UNSOLICITED_MSG1_MIN_INTERVAL);
            if limited {
                return HandshakeOutcome::dropped(); // rate-limited, drop silently
            }
            self.last_unsolicited_msg1 = Some(now);
            was_pending = true;
            let responder = match NoiseSession::as_responder(priv_key) {
                Ok(s) => s,
                Err(_) => return HandshakeOutcome::dropped(),
            };
            self.pending = Some(responder);
            self.pending.as_mut().unwrap()
        };

        let (reply, done) = session.handle_handshake_message(data);
        if !done {
            return HandshakeOutcome { reply, done: false, was_pending, peer_static_key: None, failed: false };
        }

        let failed = session.failed();
        let peer_static_key = session.peer_static_key().map(<[u8]>::to_vec);
        HandshakeOutcome { reply, done: true, was_pending, peer_static_key, failed }
    }

    /// Swaps a just-completed pending session into active. No-op if there was no
    /// pending session (the peer's first-ever handshake, not a rekey, has nothing to
    /// promote — it's already sitting in `active`).
    pub fn promote_pending(&mut self) {
        if let Some(session) = self.pending.take() {
            self.active = Some(session);
        }
    }

    /// Drops a failed pending replacement, leaving the existing active session (still
    /// healthy) untouched.
    pub fn abandon_pending(&mut self) {
        self.pending = None;
    }

    /// Encrypts payload for this peer using the active session, or NotReady if there
    /// isn't a ready one yet.
    pub fn send(&mut self, payload: &[u8]) -> Result<Vec<u8>, Error> {
        match &mut self.active {
            Some(session) => session.encrypt(payload),
            None => Err(Error::NotReady),
        }
    }

    /// Decrypts an incoming transport envelope using the active session.
    pub fn recv(&mut self, ciphertext: &[u8]) -> Result<Vec<u8>, Error> {
        match &mut self.active {
            Some(session) => session.decrypt(ciphertext),
            None => Err(Error::NotReady),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::crypto::generate_key;

    /// Drives a full handshake between a fresh initiator NoiseSession and a PeerConn
    /// playing the responder role, returning both once ready.
    fn complete_first_handshake(a_key: &PrivateKey, b_key: &PrivateKey) -> (NoiseSession, PeerConn) {
        let (mut a_session, msg1) = NoiseSession::as_initiator(a_key).unwrap();
        let mut b_peer = PeerConn::new(String::new());

        let outcome1 = b_peer.handle_handshake_envelope(b_key, &msg1);
        assert!(!outcome1.done);
        let (msg3, done) = a_session.handle_handshake_message(&outcome1.reply.unwrap());
        assert!(done);

        let outcome2 = b_peer.handle_handshake_envelope(b_key, &msg3.unwrap());
        assert!(outcome2.reply.is_none());
        assert!(outcome2.done);
        assert!(!outcome2.was_pending); // first-ever handshake, not a rekey
        assert!(!outcome2.failed);
        // First-ever handshake: nothing to promote, it's already sitting in `active`.

        (a_session, b_peer)
    }

    #[test]
    fn completes_first_handshake_and_roundtrips_both_ways() {
        let a_key = generate_key();
        let b_key = generate_key();
        let (mut a_session, mut b_peer) = complete_first_handshake(&a_key, &b_key);

        let ct_a_to_b = a_session.encrypt(b"hello from A").unwrap();
        assert_eq!(b_peer.recv(&ct_a_to_b).unwrap(), b"hello from A");

        let ct_b_to_a = b_peer.send(b"hello from B").unwrap();
        assert_eq!(a_session.decrypt(&ct_b_to_a).unwrap(), b"hello from B");
    }

    #[test]
    fn make_before_break_inflight_rekey_never_disturbs_existing_session() {
        let a_key = generate_key();
        let b_key = generate_key();
        let (mut a_session, mut b_peer) = complete_first_handshake(&a_key, &b_key);

        // Inject an unsolicited rekey attempt that never completes (stop after msg1/msg2).
        let (_, rekey_msg1) = NoiseSession::as_initiator(&a_key).unwrap();
        let outcome = b_peer.handle_handshake_envelope(&b_key, &rekey_msg1);
        assert!(outcome.was_pending);
        assert!(!outcome.done); // still mid-handshake

        // The original session must still work, both directions, throughout.
        let ct1 = a_session.encrypt(b"still using the old session").unwrap();
        assert_eq!(b_peer.recv(&ct1).unwrap(), b"still using the old session");

        let ct2 = b_peer.send(b"original session still alive mid-rekey").unwrap();
        assert_eq!(a_session.decrypt(&ct2).unwrap(), b"original session still alive mid-rekey");
    }

    #[test]
    fn rate_limits_second_unsolicited_msg1_after_failed_first_attempt() {
        let a_key = generate_key();
        let b_key = generate_key();
        let (mut a_session, mut b_peer) = complete_first_handshake(&a_key, &b_key);

        // First unsolicited attempt: accepted, then fails (garbage instead of a real
        // msg2 reply) — settles as failed, and the client calls abandon_pending() on it.
        let (_, rekey_msg1) = NoiseSession::as_initiator(&a_key).unwrap();
        let outcome = b_peer.handle_handshake_envelope(&b_key, &rekey_msg1);
        assert!(outcome.was_pending);
        let garbage = [0xffu8; 4];
        let failed_outcome = b_peer.handle_handshake_envelope(&b_key, &garbage);
        assert!(failed_outcome.done);
        assert!(failed_outcome.failed);
        b_peer.abandon_pending();

        // A second unsolicited attempt arriving immediately after must be dropped.
        let (_, rekey2_msg1) = NoiseSession::as_initiator(&a_key).unwrap();
        let outcome2 = b_peer.handle_handshake_envelope(&b_key, &rekey2_msg1);
        assert!(outcome2.reply.is_none());
        assert!(!outcome2.done);
        assert!(!outcome2.was_pending);

        // The original session must still be unaffected throughout.
        let ct = a_session.encrypt(b"still alive").unwrap();
        assert_eq!(b_peer.recv(&ct).unwrap(), b"still alive");
    }
}
