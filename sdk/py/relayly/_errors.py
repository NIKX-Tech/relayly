"""Typed exceptions for the relay's control-channel error codes (docs/PROTOCOL.md
§5.1), plus SDK-local errors not tied to a server error code."""
from __future__ import annotations

from ._noise import NotReadyError
from ._peerstore import PeerKeyMismatchError

__all__ = [
    "RelaylyError",
    "InvalidCodeError",
    "CodeExpiredError",
    "AlreadyPairedError",
    "PeerOfflineError",
    "RateLimitedError",
    "MalformedError",
    "InternalError",
    "KeyMismatchError",
    "NotReadyError",
    "PeerKeyMismatchError",
    "error_for_code",
]


class RelaylyError(Exception):
    """Base class for all typed errors surfaced by the relay's control channel."""

    code = "internal"

    def __init__(self, message: str | None = None) -> None:
        super().__init__(message or self.__doc__ or self.code)


class InvalidCodeError(RelaylyError):
    """relayly: invalid pairing code"""

    code = "invalid_code"


class CodeExpiredError(RelaylyError):
    """relayly: pairing code expired"""

    code = "code_expired"


class AlreadyPairedError(RelaylyError):
    """relayly: device already paired"""

    code = "already_paired"


class PeerOfflineError(RelaylyError):
    """relayly: peer is offline"""

    code = "peer_offline"


class RateLimitedError(RelaylyError):
    """relayly: rate limited"""

    code = "rate_limited"


class MalformedError(RelaylyError):
    """relayly: malformed request"""

    code = "malformed"


class InternalError(RelaylyError):
    """relayly: internal server error"""

    code = "internal"


class KeyMismatchError(RelaylyError):
    """relayly: announced static key does not match the server's record"""

    code = "key_mismatch"


_CODE_TO_ERROR: dict[str, type[RelaylyError]] = {
    "invalid_code": InvalidCodeError,
    "code_expired": CodeExpiredError,
    "already_paired": AlreadyPairedError,
    "peer_offline": PeerOfflineError,
    "rate_limited": RateLimitedError,
    "malformed": MalformedError,
    "key_mismatch": KeyMismatchError,
}


def error_for_code(code: str, message: str) -> RelaylyError:
    """Maps a control-channel error's machine code (docs/PROTOCOL.md §5.1) to a typed
    exception carrying the server's human-readable message."""
    error_cls = _CODE_TO_ERROR.get(code, InternalError)
    return error_cls(message or None)
