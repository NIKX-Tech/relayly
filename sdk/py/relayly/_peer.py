"""PeerConn tracks everything this client knows about one paired device: its
authenticated static key once known, the currently active Noise session, and (while a
rekey is in flight) a pending replacement session that must not replace active until it
actually completes — docs/PROTOCOL.md §6's make-before-break rule. No locking is
needed: this SDK runs its I/O on a single asyncio event loop, so there is never
concurrent access to a PeerConn from two threads (mirrors sdk/go's peer.go, minus the
mutex Go needs for its goroutine-based concurrency).
"""
from __future__ import annotations

import time
from typing import TYPE_CHECKING

from ._noise import NotReadyError, NoiseSession

if TYPE_CHECKING:
    from ._crypto import PrivateKey

# Throttles how often this client starts a brand new responder session for an
# unsolicited msg1 arriving on an already-healthy peer connection. The relay is
# untrusted (docs/PROTOCOL.md §6, §7), so without this a malicious or compromised
# relay could otherwise force perpetual handshake churn.
UNSOLICITED_MSG1_MIN_INTERVAL = 2.0  # seconds


class PeerConn:
    def __init__(self, peer_id: str, announced_static_key: str) -> None:
        self.id = peer_id
        self.announced_static_key = announced_static_key

        self.active: NoiseSession | None = None
        self.pending: NoiseSession | None = None

        # Notified exactly once, when this peer's very first handshake resolves
        # (successfully or not). Cleared on first use.
        self.first_pair_waiter = None

        self._last_unsolicited_msg1 = 0.0

    def set_first_pair_waiter(self, waiter) -> None:
        self.first_pair_waiter = waiter

    def take_first_pair_waiter(self):
        waiter = self.first_pair_waiter
        self.first_pair_waiter = None
        return waiter

    def start_as_initiator(self, priv: PrivateKey) -> bytes:
        """Begins the very first handshake for this peer (§5.3: the accepting device
        initiates). Returns the msg1 payload to send."""
        session, msg1 = NoiseSession.as_initiator(priv)
        self.active = session
        return msg1

    def start_rekey_as_initiator(self, priv: PrivateKey) -> bytes:
        """Begins a replacement handshake (§6: triggered when this device's ID is
        lexicographically smaller than the peer's, on any reconnect). The existing
        active session, if any, keeps working until the replacement completes."""
        session, msg1 = NoiseSession.as_initiator(priv)
        self.pending = session
        return msg1

    def handle_handshake_envelope(
        self, priv: PrivateKey, data: bytes
    ) -> tuple[bytes | None, NoiseSession | None, bool]:
        """Feeds one received ENVELOPE_HANDSHAKE payload to the right session, starting
        a new responder session if needed and applying make-before-break plus
        unsolicited-msg1 rate limiting. Returns (reply, completed, was_pending): reply
        is not None when a response must be sent; completed is set once the session it
        was driving finishes (successfully or not, check completed.failed);
        was_pending tells the caller whether that was a rekey attempt (abandon on
        failure, promote on success) or the peer's first-ever handshake (nothing to
        fall back to).
        """
        was_pending = False

        if self.pending is not None:
            session = self.pending
            was_pending = True
        elif self.active is not None and not self.active.ready:
            # Continuing the (first-ever) in-progress handshake.
            session = self.active
        elif self.active is None:
            # No session at all yet: this incoming msg1 starts the very first
            # handshake for this peer, with us as responder.
            session = NoiseSession.as_responder(priv)
            self.active = session
        else:
            # active exists and is ready: an unsolicited msg1 on a healthy connection.
            now = time.monotonic()
            if now - self._last_unsolicited_msg1 < UNSOLICITED_MSG1_MIN_INTERVAL:
                return None, None, False  # rate-limited, drop silently
            self._last_unsolicited_msg1 = now
            session = NoiseSession.as_responder(priv)
            self.pending = session
            was_pending = True

        reply, done = session.handle_handshake_message(data)
        if not done:
            return reply, None, was_pending
        return reply, session, was_pending

    def promote(self, session: NoiseSession) -> None:
        """Swaps a just-completed pending session into active. No-op if session was
        already active (the peer's first-ever handshake, not a rekey)."""
        if self.pending is session:
            self.active = session
            self.pending = None

    def abandon(self, session: NoiseSession) -> None:
        """Drops a failed pending replacement, leaving the existing active session
        (still healthy) untouched. No-op if session was the peer's first-ever (active)
        session — a failed first handshake fails the peer entirely; there is nothing to
        fall back to."""
        if self.pending is session:
            self.pending = None

    def send(self, payload: bytes) -> bytes:
        """Encrypts payload for this peer using the active session, or raises
        NotReadyError if there isn't a ready one yet."""
        if self.active is None:
            raise NotReadyError("relayly: peer session is not ready")
        return self.active.encrypt(payload)

    def recv(self, ciphertext: bytes) -> bytes:
        """Decrypts an incoming transport envelope using the active session."""
        if self.active is None:
            raise NotReadyError("relayly: peer session is not ready")
        return self.active.decrypt(ciphertext)
