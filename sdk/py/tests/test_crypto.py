"""Tests for the crypto layer — X25519 key generation, encoding, file persistence."""
import base64
import tempfile
from pathlib import Path

import pytest

import relayly
from relayly._crypto import (
    PrivateKey,
    PublicKey,
    generate_key,
    load_key_from_file,
    load_or_generate_key,
)


def test_generate_key_returns_different_keys():
    k1 = generate_key()
    k2 = generate_key()
    assert k1.to_bytes() != k2.to_bytes()


def test_generated_key_is_32_bytes():
    key = generate_key()
    assert len(key.to_bytes()) == 32


def test_public_key_derived_from_private():
    key = generate_key()
    pub = key.public_key
    assert len(pub._raw) == 32


def test_public_key_base64_roundtrip():
    key = generate_key()
    pub = key.public_key
    restored = PublicKey.from_base64(pub.to_base64())
    assert restored._raw == pub._raw


def test_private_key_base64_roundtrip():
    key = generate_key()
    b64 = key.to_base64()
    raw = base64.b64decode(b64)
    assert len(raw) == 32
    assert raw == key.to_bytes()


def test_private_key_from_bytes_roundtrip():
    from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey

    key = generate_key()
    raw = key.to_bytes()
    restored = PrivateKey(X25519PrivateKey.from_private_bytes(raw))
    assert restored.to_bytes() == raw
    assert restored.public_key.to_base64() == key.public_key.to_base64()


def test_save_and_load_key_file():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "device.key"
        original = generate_key()
        original.save_to_file(path)

        loaded = load_key_from_file(path)
        assert loaded.to_bytes() == original.to_bytes()


def test_save_to_file_sets_restrictive_permissions():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "device.key"
        generate_key().save_to_file(path)
        assert (path.stat().st_mode & 0o777) == 0o600


def test_load_or_generate_creates_on_missing():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "device.key"
        assert not path.exists()

        key1 = load_or_generate_key(path)
        assert path.exists()

        key2 = load_or_generate_key(path)
        assert key1.to_bytes() == key2.to_bytes()


def test_public_key_invalid_length_raises():
    with pytest.raises(ValueError, match="32"):
        PublicKey(b"short")


def test_relayly_module_exports():
    assert hasattr(relayly, "connect")
    assert hasattr(relayly, "Options")
    assert hasattr(relayly, "generate_key")
    assert hasattr(relayly, "Client")
    assert hasattr(relayly, "PeerStore")
    assert hasattr(relayly, "NotReadyError")
    assert hasattr(relayly, "PeerKeyMismatchError")
