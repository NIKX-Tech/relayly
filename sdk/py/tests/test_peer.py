"""Tests for PeerConn — make-before-break (docs/PROTOCOL.md §6) and the
unsolicited-msg1 rate limit, matching sdk/go's noise_test.go / sdk/ts's
peer.test.ts scenarios."""
from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey

from relayly._crypto import PrivateKey
from relayly._noise import NoiseSession
from relayly._peer import PeerConn


def _keypair() -> PrivateKey:
    return PrivateKey(X25519PrivateKey.generate())


def _complete_first_handshake(a_key: PrivateKey, b_key: PrivateKey):
    """Drives a full handshake between a fresh initiator NoiseSession and a PeerConn
    playing the responder role, returning both once ready."""
    a_session, msg1 = NoiseSession.as_initiator(a_key)
    b_peer = PeerConn("device-a", "")

    reply1, completed1, _ = b_peer.handle_handshake_envelope(b_key, msg1)
    assert completed1 is None
    msg3, done = a_session.handle_handshake_message(reply1)
    assert done

    reply2, completed2, was_pending = b_peer.handle_handshake_envelope(b_key, msg3)
    assert reply2 is None
    assert completed2 is not None
    assert was_pending is False  # first-ever handshake, not a rekey
    b_peer.promote(completed2)

    return a_session, b_peer


def test_completes_first_handshake_and_roundtrips_both_ways():
    a_key, b_key = _keypair(), _keypair()
    a_session, b_peer = _complete_first_handshake(a_key, b_key)
    assert a_session.ready

    ct_a_to_b = a_session.encrypt(b"hello from A")
    assert b_peer.recv(ct_a_to_b) == b"hello from A"

    ct_b_to_a = b_peer.send(b"hello from B")
    assert a_session.decrypt(ct_b_to_a) == b"hello from B"


def test_make_before_break_inflight_rekey_never_disturbs_existing_session():
    a_key, b_key = _keypair(), _keypair()
    a_session, b_peer = _complete_first_handshake(a_key, b_key)

    # Inject an unsolicited rekey attempt that never completes (stop after msg1/msg2).
    rekey_session, rekey_msg1 = NoiseSession.as_initiator(a_key)
    reply, completed, was_pending = b_peer.handle_handshake_envelope(b_key, rekey_msg1)
    assert was_pending is True
    assert completed is None  # still mid-handshake

    # The original session must still work, both directions, throughout.
    ct1 = a_session.encrypt(b"still using the old session")
    assert b_peer.recv(ct1) == b"still using the old session"

    ct2 = b_peer.send(b"original session still alive mid-rekey")
    assert a_session.decrypt(ct2) == b"original session still alive mid-rekey"


def test_rate_limits_second_unsolicited_msg1_after_failed_first_attempt():
    a_key, b_key = _keypair(), _keypair()
    a_session, b_peer = _complete_first_handshake(a_key, b_key)

    # First unsolicited attempt: accepted, then fails (garbage instead of a real msg2
    # reply) — settles as failed, and the client calls abandon() on it.
    _, rekey_msg1 = NoiseSession.as_initiator(a_key)
    _, _, was_pending = b_peer.handle_handshake_envelope(b_key, rekey_msg1)
    assert was_pending is True
    garbage = bytes([0xFF, 0xFF, 0xFF, 0xFF])
    _, failed_completed, _ = b_peer.handle_handshake_envelope(b_key, garbage)
    assert failed_completed is not None
    assert failed_completed.failed
    b_peer.abandon(failed_completed)

    # A second unsolicited attempt arriving immediately after must be dropped.
    _, rekey2_msg1 = NoiseSession.as_initiator(a_key)
    reply2, completed2, was_pending2 = b_peer.handle_handshake_envelope(b_key, rekey2_msg1)
    assert reply2 is None
    assert completed2 is None
    assert was_pending2 is False

    # The original session must still be unaffected throughout.
    ct = a_session.encrypt(b"still alive")
    assert b_peer.recv(ct) == b"still alive"
