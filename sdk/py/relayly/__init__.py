"""
Relayly Python SDK — self-hosted end-to-end encrypted WebSocket relay.

Encryption is device-to-device Noise XX (Noise_XX_25519_ChaChaPoly_BLAKE2s); the relay
itself holds no key material. See docs/PROTOCOL.md for the full wire spec.

Quick start::

    import asyncio
    import relayly

    async def main():
        key = relayly.load_or_generate_key("~/.relayly/device.key")

        client = await relayly.connect("wss://relay.example.com", relayly.Options(
            device_id="my-laptop",
            device_token=device_token,  # from POST /api/v1/devices
            private_key=key,
        ))

        code = await client.request_pair_code()
        print("Share:", code.short)

        peer = await code.wait()
        await client.send(peer.id, b"hello!")

        async for msg in client.messages():
            print(f"[{msg.from_device}]", msg.payload.decode())

    asyncio.run(main())
"""

from ._client import Client, Message, Options, PairCode, Peer, connect
from ._crypto import PrivateKey, PublicKey, generate_key, load_key_from_file, load_or_generate_key
from ._errors import (
    AlreadyPairedError,
    CodeExpiredError,
    InternalError,
    InvalidCodeError,
    KeyMismatchError,
    MalformedError,
    NotReadyError,
    PeerKeyMismatchError,
    PeerOfflineError,
    RateLimitedError,
    RelaylyError,
)
from ._peerstore import DEFAULT_PEER_STORE_PATH, PeerStore

__all__ = [
    # factory
    "connect",
    # types
    "Client",
    "Message",
    "Options",
    "Peer",
    "PairCode",
    # crypto
    "PrivateKey",
    "PublicKey",
    "generate_key",
    "load_key_from_file",
    "load_or_generate_key",
    # peer key pinning
    "PeerStore",
    "DEFAULT_PEER_STORE_PATH",
    # errors
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
]
