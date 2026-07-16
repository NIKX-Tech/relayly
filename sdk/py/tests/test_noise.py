"""flynn/noise cross-validation vectors — the same fixed keys/deterministic ephemeral
bytes already embedded in sdk/ts/src/noise/noise.test.ts, generated with a standalone
Go program using flynn/noise (already proven server-side and in sdk/go). This is the
actual correctness gate for using `noiseprotocol` here rather than "the library claims
to support our cipher suite": Noise's AEAD auth means a subtle mismatch fails loudly
(decrypt/MAC failure), not silently.

noiseprotocol's own ephemeral-key-generation path can't be reseeded per call, so the
deterministic ephemeral bytes are injected directly via
set_keypair_from_private_bytes(Keypair.EPHEMERAL, ...) before the handshake starts —
the library then uses that fixed key instead of generating a random one (it emits a
UserWarning about this being test-only, which is exactly the point).
"""
from __future__ import annotations

import warnings

import pytest
from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey
from noise.connection import Keypair, NoiseConnection

from relayly._crypto import PrivateKey
from relayly._noise import NOISE_PROTOCOL_NAME, NoiseSession

A_STATIC_PRIVATE = bytes.fromhex("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
B_STATIC_PRIVATE = bytes.fromhex("101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f")
A_STATIC_PUBLIC = bytes.fromhex("07a37cbc142093c8b755dc1b10e86cb426374ad16aa853ed0bdfc0b2b86d1c7c")
B_STATIC_PUBLIC = bytes.fromhex("d89e3bad79437dbed9f843418304f460ff05c7fe81fe4a9577a804cb9367ff66")

MSG1 = "358072d6365880d1aeea329adf9121383851ed21a28e3b75e965d0d2cd166254"
MSG2 = (
    "34e42d4af5ef94a07a3a84201b889d4cd1a743cb27b11b6a10438a8feb8e5847ee0b2fa3bbca43904"
    "cbf6186d5e09fe67128c94cc3e3da6d35bf21f0358c487d5300c27a709ae1da5b4951c9eb1f0afd64"
    "e57891c7894b617293b07c9a455311"
)
MSG3 = (
    "b8312f344cb91f060c34ae9a514f48981b3316af898179729fd217b843cf0f75b07d427b956b287b"
    "149ee47a4b0b71e3b822b0f15bc616ce52af8a3dbeab8bc8"
)
CT_A_TO_B_1 = "a21eb0be51f6230018b2a51f1b501eb2885cf12b23e6351f1a577c43"
CT_B_TO_A_1 = "362c3040c6440177f0d09b74b5457be4fc12cc30733563aa87dc83b9"


def _det_bytes(seed: int, n: int) -> bytes:
    return bytes((seed + i) & 0xFF for i in range(n))


def _make_connection(static_private: bytes, ephemeral_seed: int, initiator: bool) -> NoiseConnection:
    conn = NoiseConnection.from_name(NOISE_PROTOCOL_NAME)
    if initiator:
        conn.set_as_initiator()
    else:
        conn.set_as_responder()
    conn.set_keypair_from_private_bytes(Keypair.STATIC, static_private)
    with warnings.catch_warnings():
        warnings.simplefilter("ignore")  # expected: manually seeding the ephemeral key
        conn.set_keypair_from_private_bytes(Keypair.EPHEMERAL, _det_bytes(ephemeral_seed, 32))
    conn.start_handshake()
    return conn


@pytest.mark.filterwarnings("ignore:One of ephemeral keypairs is already set")
def test_handshake_messages_match_flynn_noise_vectors():
    a = _make_connection(A_STATIC_PRIVATE, 0x20, initiator=True)
    b = _make_connection(B_STATIC_PRIVATE, 0x30, initiator=False)

    msg1 = bytes(a.write_message())
    assert msg1.hex() == MSG1
    b.read_message(msg1)

    msg2 = bytes(b.write_message())
    assert msg2.hex() == MSG2
    a.read_message(msg2)

    msg3 = bytes(a.write_message())
    assert msg3.hex() == MSG3
    b.read_message(msg3)

    assert a.handshake_finished
    assert b.handshake_finished

    ct1 = a.encrypt(b"hello from A")
    assert ct1.hex() == CT_A_TO_B_1
    assert b.decrypt(ct1) == b"hello from A"

    ct2 = b.encrypt(b"hello from B")
    assert ct2.hex() == CT_B_TO_A_1
    assert a.decrypt(ct2) == b"hello from B"


def test_noise_session_handshake_and_transport_roundtrip():
    # Build PrivateKey wrappers from the fixed raw bytes (not generate_key(), so both
    # sides' handshake is fully reproducible for this test).
    a_priv = PrivateKey(X25519PrivateKey.from_private_bytes(A_STATIC_PRIVATE))
    b_priv = PrivateKey(X25519PrivateKey.from_private_bytes(B_STATIC_PRIVATE))

    initiator, msg1 = NoiseSession.as_initiator(a_priv)
    responder = NoiseSession.as_responder(b_priv)

    reply, done = responder.handle_handshake_message(msg1)
    assert reply is not None
    assert not done

    msg3, done = initiator.handle_handshake_message(reply)
    assert msg3 is not None
    assert done
    assert initiator.ready

    _, done = responder.handle_handshake_message(msg3)
    assert done
    assert responder.ready

    assert initiator.peer_static_key == B_STATIC_PUBLIC
    assert responder.peer_static_key == A_STATIC_PUBLIC

    ct = initiator.encrypt(b"hello from A")
    assert responder.decrypt(ct) == b"hello from A"

    ct2 = responder.encrypt(b"hello from B")
    assert initiator.decrypt(ct2) == b"hello from B"


def test_noise_session_rejects_corrupted_ciphertext():
    a_priv = PrivateKey(X25519PrivateKey.from_private_bytes(A_STATIC_PRIVATE))
    b_priv = PrivateKey(X25519PrivateKey.from_private_bytes(B_STATIC_PRIVATE))

    initiator, msg1 = NoiseSession.as_initiator(a_priv)
    responder = NoiseSession.as_responder(b_priv)
    reply, _ = responder.handle_handshake_message(msg1)
    msg3, _ = initiator.handle_handshake_message(reply)
    responder.handle_handshake_message(msg3)

    ct = bytearray(initiator.encrypt(b"hi"))
    ct[0] ^= 0xFF
    with pytest.raises(Exception):
        responder.decrypt(bytes(ct))
