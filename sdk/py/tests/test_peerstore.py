"""Tests for PeerStore — pinned peer static keys (docs/PROTOCOL.md §7.1), matching
sdk/go's peerstore_test.go / sdk/ts's peerStore.test.ts scenarios."""
import json
import tempfile
from pathlib import Path

import pytest

from relayly._peerstore import PeerKeyMismatchError, PeerStore


def test_load_missing_file_starts_empty():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "peers.json"
        store = PeerStore.load(str(path))
        assert store.get("peer-a") is None


def test_pin_first_seen_key_and_persist():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "peers.json"
        store = PeerStore.load(str(path))

        store.pin_or_verify("peer-a", "aGVsbG8=")
        assert store.get("peer-a") == "aGVsbG8="
        assert path.exists()

        on_disk = json.loads(path.read_text())
        assert on_disk["peer-a"]["static_key"] == "aGVsbG8="
        assert "pinned_at" in on_disk["peer-a"]


def test_matching_reannounce_is_a_noop():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "peers.json"
        store = PeerStore.load(str(path))
        store.pin_or_verify("peer-a", "aGVsbG8=")
        store.pin_or_verify("peer-a", "aGVsbG8=")  # same key again, no error
        assert store.get("peer-a") == "aGVsbG8="


def test_mismatch_rejected_original_key_kept():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "peers.json"
        store = PeerStore.load(str(path))
        store.pin_or_verify("peer-a", "aGVsbG8=")

        with pytest.raises(PeerKeyMismatchError):
            store.pin_or_verify("peer-a", "d29ybGQ=")

        assert store.get("peer-a") == "aGVsbG8="  # original pin untouched


def test_persists_across_reload():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "peers.json"
        store1 = PeerStore.load(str(path))
        store1.pin_or_verify("peer-a", "aGVsbG8=")

        store2 = PeerStore.load(str(path))
        assert store2.get("peer-a") == "aGVsbG8="


def test_save_sets_restrictive_permissions():
    with tempfile.TemporaryDirectory() as d:
        path = Path(d) / "peers.json"
        store = PeerStore.load(str(path))
        store.pin_or_verify("peer-a", "aGVsbG8=")
        assert (path.stat().st_mode & 0o777) == 0o600
