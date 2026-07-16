from __future__ import annotations

import base64
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey
from cryptography.hazmat.primitives.serialization import (
    Encoding,
    NoEncryption,
    PrivateFormat,
    PublicFormat,
)


class PublicKey:
    """X25519 public key."""

    def __init__(self, raw: bytes) -> None:
        if len(raw) != 32:
            raise ValueError(f"relayly: invalid public key: expected 32 bytes, got {len(raw)}")
        self._raw = raw

    def to_base64(self) -> str:
        return base64.b64encode(self._raw).decode()

    @classmethod
    def from_base64(cls, s: str) -> PublicKey:
        return cls(base64.b64decode(s))


class PrivateKey:
    """X25519 private key — the Noise XX static keypair for device-to-device
    end-to-end encryption (docs/PROTOCOL.md §6)."""

    def __init__(self, key: X25519PrivateKey) -> None:
        self._key = key

    @property
    def public_key(self) -> PublicKey:
        raw = self._key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
        return PublicKey(raw)

    def to_bytes(self) -> bytes:
        return self._key.private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption())

    def to_base64(self) -> str:
        return base64.b64encode(self.to_bytes()).decode()

    def save_to_file(self, path: str | Path) -> None:
        p = Path(path).expanduser()
        p.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        p.write_text(self.to_base64() + "\n")
        p.chmod(0o600)


def generate_key() -> PrivateKey:
    """Generate a new random X25519 private key."""
    return PrivateKey(X25519PrivateKey.generate())


def load_key_from_file(path: str | Path) -> PrivateKey:
    """Load a private key from a file saved by PrivateKey.save_to_file()."""
    text = Path(path).expanduser().read_text().strip()
    raw = base64.b64decode(text)
    return PrivateKey(X25519PrivateKey.from_private_bytes(raw))


def load_or_generate_key(path: str | Path) -> PrivateKey:
    """Load the key at path, or generate and save a new one if missing."""
    p = Path(path).expanduser()
    if p.exists():
        return load_key_from_file(p)
    key = generate_key()
    key.save_to_file(p)
    return key
