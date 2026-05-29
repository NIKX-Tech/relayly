from __future__ import annotations

import base64
from pathlib import Path

import nacl.public
import nacl.utils


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
    """X25519 private key for NaCl box (XSalsa20-Poly1305) encryption."""

    def __init__(self, key: nacl.public.PrivateKey) -> None:
        self._key = key

    @property
    def public_key(self) -> PublicKey:
        return PublicKey(bytes(self._key.public_key))

    def to_bytes(self) -> bytes:
        return bytes(self._key)

    def to_base64(self) -> str:
        return base64.b64encode(bytes(self._key)).decode()

    def save_to_file(self, path: str | Path) -> None:
        p = Path(path).expanduser()
        p.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        p.write_text(self.to_base64() + "\n")
        p.chmod(0o600)

    def encrypt(self, plaintext: bytes, recipient: PublicKey) -> tuple[bytes, bytes]:
        """Encrypt plaintext for recipient. Returns (ciphertext, nonce)."""
        box = nacl.public.Box(self._key, nacl.public.PublicKey(recipient._raw))
        nonce = nacl.utils.random(nacl.public.Box.NONCE_SIZE)
        encrypted = box.encrypt(plaintext, nonce)
        return bytes(encrypted.ciphertext), bytes(encrypted.nonce)

    def decrypt(self, ciphertext: bytes, nonce: bytes, sender: PublicKey) -> bytes:
        """Decrypt a ciphertext received from sender."""
        box = nacl.public.Box(self._key, nacl.public.PublicKey(sender._raw))
        return bytes(box.decrypt(ciphertext, nonce))


def generate_key() -> PrivateKey:
    """Generate a new random X25519 private key."""
    return PrivateKey(nacl.public.PrivateKey.generate())


def load_key_from_file(path: str | Path) -> PrivateKey:
    """Load a private key from a file saved by PrivateKey.save_to_file()."""
    text = Path(path).expanduser().read_text().strip()
    raw = base64.b64decode(text)
    return PrivateKey(nacl.public.PrivateKey(raw))


def load_or_generate_key(path: str | Path) -> PrivateKey:
    """Load the key at path, or generate and save a new one if missing."""
    p = Path(path).expanduser()
    if p.exists():
        return load_key_from_file(p)
    key = generate_key()
    key.save_to_file(p)
    return key
