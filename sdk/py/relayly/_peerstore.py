"""Persists pinned peer static keys (docs/PROTOCOL.md §7.1): the client-side pin is the
real security boundary. A peer's key is pinned the first time its Noise handshake
completes; any later handshake presenting a different key for the same peer ID
hard-fails with PeerKeyMismatchError. Unpinning is never automatic.

Schema is shared byte-for-byte across every official SDK (docs/tasks/
02-sdks-and-interop.md), so the same file can in principle be read/written by clients
written in different languages on the same machine.
"""
from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path

DEFAULT_PEER_STORE_PATH = "~/.relayly/peers.json"


class PeerKeyMismatchError(Exception):
    """The client-side pin check (§7.1, the real security boundary) rejecting a peer
    whose authenticated static key differs from the one already pinned for that peer
    ID. Unpinning is an explicit user action only; this error is never auto-retried."""


class PeerStore:
    """Loads/persists pinned peer static keys. Use PeerStore.load(path) to create one."""

    def __init__(self, path: str, peers: dict[str, dict[str, str]]) -> None:
        self._path = path
        self._peers = peers

    @classmethod
    def load(cls, path: str = DEFAULT_PEER_STORE_PATH) -> PeerStore:
        """Loads the peer store at path, creating an empty one in memory if the file
        doesn't exist yet (it is created on first successful pin)."""
        expanded = str(Path(path).expanduser())
        try:
            with open(expanded, "r", encoding="utf-8") as f:
                data = f.read()
        except FileNotFoundError:
            return cls(expanded, {})

        if not data.strip():
            return cls(expanded, {})
        peers = json.loads(data)
        return cls(expanded, peers)

    def pin_or_verify(self, peer_id: str, static_key_b64: str) -> None:
        """Implements §7.1: if peer_id has no recorded pin yet, static_key_b64 is
        pinned and persisted. If a pin already exists and matches, this is a no-op. If
        a pin already exists and differs, raises PeerKeyMismatchError and leaves the
        original pin in place."""
        existing = self._peers.get(peer_id)
        if existing is not None:
            if existing["static_key"] != static_key_b64:
                raise PeerKeyMismatchError(
                    f"relayly: peer's authenticated key does not match the pinned key (peer {peer_id})"
                )
            return

        self._peers[peer_id] = {
            "static_key": static_key_b64,
            "pinned_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        self._save()

    def get(self, peer_id: str) -> str | None:
        """Returns the pinned static key (base64) for peer_id, if any."""
        entry = self._peers.get(peer_id)
        return entry["static_key"] if entry is not None else None

    def _save(self) -> None:
        directory = os.path.dirname(self._path)
        if directory:
            os.makedirs(directory, mode=0o700, exist_ok=True)

        data = json.dumps(self._peers, indent=2)
        tmp = self._path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            f.write(data)
        os.chmod(tmp, 0o600)
        os.replace(tmp, self._path)
