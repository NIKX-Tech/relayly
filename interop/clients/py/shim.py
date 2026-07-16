"""A thin CLI wrapper around sdk/py's public API, driven by newline-delimited JSON
over stdin/stdout. Exists only for the interop harness (interop/harness/) to drive a
real relayly.Client as a subprocess — no internal/test-only hooks, proving the public
API alone is enough for interop testing. Protocol matches interop/clients/go/main.go
exactly (see its doc comment for the full command/event list).

Requires `pip install -e sdk/py` first.
"""
from __future__ import annotations

import argparse
import asyncio
import base64
import json
import sys

import relayly


def emit(obj: dict) -> None:
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


async def stdin_lines():
    loop = asyncio.get_running_loop()
    # Default limit is 64 KiB; a max-size test payload (~64 KiB raw) becomes larger
    # than that once base64-encoded plus JSON framing, so raise it well past the
    # largest command line the interop harness will ever send.
    reader = asyncio.StreamReader(limit=1_000_000)
    protocol = asyncio.StreamReaderProtocol(reader)
    await loop.connect_read_pipe(lambda: protocol, sys.stdin)
    while True:
        line = await reader.readline()
        if not line:
            return
        yield line.decode().strip()


async def handle_command(client: relayly.Client, cmd: dict) -> None:
    action = cmd.get("cmd")

    if action == "request_pair_code":
        try:
            code = await client.request_pair_code()
            emit({"event": "pair_code", "code": code.short, "expires_in": code.expires_in})
            peer = await code.wait()
            emit(
                {
                    "event": "paired",
                    "peer_id": peer.id,
                    "peer_public_key_b64": base64.b64encode(peer.public_key).decode(),
                }
            )
        except Exception as exc:
            emit({"event": "pair_error", "message": str(exc)})

    elif action == "accept_pair":
        try:
            peer = await client.accept_pair(cmd["code"])
            emit(
                {
                    "event": "paired",
                    "peer_id": peer.id,
                    "peer_public_key_b64": base64.b64encode(peer.public_key).decode(),
                }
            )
        except Exception as exc:
            emit({"event": "pair_error", "message": str(exc)})

    elif action == "send":
        try:
            payload = base64.b64decode(cmd["payload_b64"])
            await client.send(cmd["peer_id"], payload)
            emit({"event": "sent"})
        except Exception as exc:
            emit({"event": "send_error", "message": str(exc)})

    elif action == "close":
        await client.close()


async def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server", required=True)
    parser.add_argument("--device-id", required=True)
    parser.add_argument("--device-token", required=True)
    parser.add_argument("--peer-store-path", default=None)
    args = parser.parse_args()

    key = relayly.generate_key()

    opts_kwargs = dict(
        device_id=args.device_id,
        device_token=args.device_token,
        private_key=key,
        on_ready=lambda peer_id: emit({"event": "ready_signal", "peer_id": peer_id}),
        on_peer_status=lambda peer_id, online: emit(
            {"event": "peer_status", "peer_id": peer_id, "online": online}
        ),
    )
    if args.peer_store_path:
        opts_kwargs["peer_store_path"] = args.peer_store_path
    opts = relayly.Options(**opts_kwargs)

    try:
        client = await relayly.connect(args.server, opts)
    except Exception as exc:
        emit({"event": "connect_error", "message": str(exc)})
        sys.exit(1)

    async def message_pump():
        async for msg in client.messages():
            emit(
                {
                    "event": "message",
                    "from": msg.from_device,
                    "payload_b64": base64.b64encode(msg.payload).decode(),
                }
            )

    asyncio.create_task(message_pump())
    emit({"event": "ready"})

    async for line in stdin_lines():
        try:
            cmd = json.loads(line)
        except Exception:
            continue
        if cmd.get("cmd") == "close":
            await handle_command(client, cmd)
            break
        asyncio.create_task(handle_command(client, cmd))

    await asyncio.sleep(0.2)  # let close() finish flushing


if __name__ == "__main__":
    asyncio.run(main())
