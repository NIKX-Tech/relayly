"""
Relayly Python SDK — self-hosted end-to-end encrypted WebSocket relay.

Quick start::

    import asyncio
    import relayly

    async def main():
        key = relayly.load_or_generate_key("~/.relayly/device.key")

        client = await relayly.connect("wss://relay.example.com", relayly.Options(
            device_id="my-laptop",
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

from ._client import Client, Message, Options, Peer, PairCode, connect
from ._crypto import PrivateKey, PublicKey, generate_key, load_key_from_file, load_or_generate_key

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
]
