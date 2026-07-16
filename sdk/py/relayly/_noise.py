"""Device-to-device Noise XX handshake and transport encryption
(Noise_XX_25519_ChaChaPoly_BLAKE2s, docs/PROTOCOL.md §6), driven through the
`noiseprotocol` library rather than hand-rolled: its DH/cipher/hash backends delegate
to `cryptography`, and it supports our exact cipher suite by name. Verified
byte-for-byte against the same flynn/noise reference vectors used in sdk/ts/src/noise/
noise.test.ts (see tests/test_noise.py) — real cross-implementation proof, not just
"the library claims to support it."
"""
from __future__ import annotations

import enum

from noise.connection import Keypair, NoiseConnection

from ._crypto import PrivateKey

NOISE_PROTOCOL_NAME = b"Noise_XX_25519_ChaChaPoly_BLAKE2s"

# E2E envelope types (docs/PROTOCOL.md §6): binary WebSocket frames are one byte of
# envelope type followed by the Noise message or transport ciphertext.
ENVELOPE_HANDSHAKE = 0x01
ENVELOPE_TRANSPORT = 0x02


def encode_envelope(kind: int, payload: bytes) -> bytes:
    return bytes([kind]) + payload


def decode_envelope(frame: bytes) -> tuple[int, bytes] | None:
    if len(frame) < 1:
        return None
    return frame[0], frame[1:]


class NotReadyError(Exception):
    """Raised by send()/recv() when a peer's Noise session isn't ready yet."""


class _Status(enum.Enum):
    HANDSHAKING = enum.auto()
    READY = enum.auto()
    FAILED = enum.auto()


class NoiseSession:
    """Drives exactly one Noise XX handshake, as either role, and once it completes,
    encrypts/decrypts transport messages for that session. Does not itself implement
    the make-before-break replacement policy — see _peer.py for the wrapper that
    decides when a new NoiseSession may replace an existing one.
    """

    def __init__(self, initiator: bool, private_key: PrivateKey) -> None:
        self._initiator = initiator
        self._status = _Status.HANDSHAKING
        self._error: Exception | None = None
        self._peer_static: bytes | None = None

        self._conn = NoiseConnection.from_name(NOISE_PROTOCOL_NAME)
        if initiator:
            self._conn.set_as_initiator()
        else:
            self._conn.set_as_responder()
        self._conn.set_keypair_from_private_bytes(Keypair.STATIC, private_key.to_bytes())
        self._conn.start_handshake()

        # noiseprotocol deletes NoiseConnection.noise_protocol.handshake_state the
        # instant the handshake finishes (handshake_done() in noise_protocol.py), and
        # exposes no public accessor for the authenticated peer static key (.rs)
        # afterward. The HandshakeState *object* itself isn't affected by that later
        # `del` of the attribute path — only the noise_protocol.handshake_state binding
        # is removed — so capturing our own reference here, before any message is
        # processed, keeps .rs readable after handshake_finished flips to True.
        self._handshake_state = self._conn.noise_protocol.handshake_state

    @classmethod
    def as_initiator(cls, private_key: PrivateKey) -> tuple[NoiseSession, bytes]:
        """Starts a handshake as the Noise initiator and returns (session, msg1) —
        msg1 is the first message to send as an ENVELOPE_HANDSHAKE frame."""
        session = cls(True, private_key)
        msg1 = bytes(session._conn.write_message())
        return session, msg1

    @classmethod
    def as_responder(cls, private_key: PrivateKey) -> NoiseSession:
        """Starts a handshake as the Noise responder, ready to receive msg1."""
        return cls(False, private_key)

    @property
    def ready(self) -> bool:
        return self._status == _Status.READY

    @property
    def failed(self) -> bool:
        return self._status == _Status.FAILED

    @property
    def peer_static_key(self) -> bytes:
        """The authenticated peer static key. Raises if the handshake isn't ready."""
        if self._peer_static is None:
            raise NotReadyError("relayly: handshake not ready")
        return self._peer_static

    def handle_handshake_message(self, data: bytes) -> tuple[bytes | None, bool]:
        """Feeds one received ENVELOPE_HANDSHAKE payload into the state machine.
        Returns (reply, done): reply is non-None when a response message must be sent
        back; done is True once the handshake has completed (successfully or not) —
        check .failed in that case.
        """
        if self._status != _Status.HANDSHAKING:
            self._fail(Exception("relayly: handshake message received after handshake finished"))
            return None, True

        try:
            if self._initiator:
                # The only message an initiator ever receives is msg2; writing msg3
                # completes the handshake for the initiator.
                self._conn.read_message(data)
                reply = bytes(self._conn.write_message())
                if self._conn.handshake_finished:
                    self._finish()
                return reply, self._conn.handshake_finished

            # Responder.
            self._conn.read_message(data)
            if not self._conn.handshake_finished:
                reply = bytes(self._conn.write_message())
                return reply, False
            self._finish()
            return None, True
        except Exception as exc:  # noqa: BLE001 — any handshake failure (bad AEAD
            # tag, malformed message, wrong state) ends the session, never propagates.
            self._fail(exc)
            return None, True

    def _finish(self) -> None:
        self._peer_static = self._handshake_state.rs.public_bytes
        self._status = _Status.READY

    def _fail(self, exc: Exception) -> None:
        if self._status == _Status.FAILED:
            return
        self._status = _Status.FAILED
        self._error = exc

    def encrypt(self, plaintext: bytes) -> bytes:
        if self._status != _Status.READY:
            raise NotReadyError("relayly: peer session is not ready")
        return self._conn.encrypt(plaintext)

    def decrypt(self, ciphertext: bytes) -> bytes:
        if self._status != _Status.READY:
            raise NotReadyError("relayly: peer session is not ready")
        return self._conn.decrypt(ciphertext)
