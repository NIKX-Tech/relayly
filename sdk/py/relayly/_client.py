from __future__ import annotations

import asyncio
import base64
import dataclasses
import json
from datetime import datetime
from typing import AsyncGenerator, Callable
from urllib.parse import urlparse, urlunparse, urlencode, parse_qsl

import websockets
import websockets.exceptions

from ._crypto import PrivateKey, PublicKey


# ---------------------------------------------------------------------------
# Public types
# ---------------------------------------------------------------------------


@dataclasses.dataclass
class Peer:
    """A paired remote device."""

    id: str
    public_key: bytes  # raw 32-byte X25519 public key


@dataclasses.dataclass
class Message:
    """A decrypted incoming message from a paired peer."""

    from_device: str
    payload: bytes
    timestamp: datetime | None = None


@dataclasses.dataclass
class Options:
    """Configuration for a Relayly client connection.

    Example::

        opts = Options(
            device_id="my-laptop",
            private_key=generate_key(),
        )
    """

    device_id: str
    private_key: PrivateKey
    ping_interval: float = 30.0
    reconnect_delay: float = 1.0   # seconds; set to -1 to disable
    max_reconnect_delay: float = 60.0
    on_reconnect: Callable[[], None] | None = None
    on_disconnect: Callable[[Exception], None] | None = None


# ---------------------------------------------------------------------------
# Internal types
# ---------------------------------------------------------------------------


@dataclasses.dataclass
class _PairResult:
    code: str = ""
    expires_in: int = 0
    peer_id: str = ""
    peer_public_key: bytes = dataclasses.field(default_factory=bytes)
    error: Exception | None = None


class PairCode:
    """A pairing code to share with another device."""

    def __init__(self, short: str, expires_in: int, client: Client, future: asyncio.Future) -> None:
        self.short = short
        self.expires_in = expires_in
        self._client = client
        self._future: asyncio.Future[_PairResult] = future

    def qr_code_url(self, server_url: str) -> str:
        """Return a URL encoding the server address and code, suitable for a QR code."""
        parsed = urlparse(server_url)
        query = urlencode(dict(parse_qsl(parsed.query)) | {"code": self.short})
        return urlunparse(parsed._replace(path="/pair", query=query))

    async def wait(self) -> Peer:
        """Block until the other device accepts the pairing."""
        result = await self._future
        if result.error:
            raise result.error
        return Peer(id=result.peer_id, public_key=result.peer_public_key)


# ---------------------------------------------------------------------------
# Client
# ---------------------------------------------------------------------------


class Client:
    """A connected Relayly client. Use :func:`connect` to create one."""

    def __init__(self, server_url: str, opts: Options) -> None:
        self._server_url = _normalise_ws_url(server_url)
        self._opts = opts
        self._ws: websockets.WebSocketClientProtocol | None = None
        self._closed = False
        self._stop: asyncio.Event = asyncio.Event()

        self._peers: dict[str, Peer] = {}
        self._message_queue: asyncio.Queue[Message | None] = asyncio.Queue(maxsize=64)
        self._send_queue: asyncio.Queue[dict | None] = asyncio.Queue(maxsize=64)
        self._pair_waiters: dict[str, asyncio.Future[_PairResult]] = {}

        self._tasks: list[asyncio.Task] = []

    def _start_loops(self) -> None:
        self._tasks = [
            asyncio.create_task(self._read_loop(), name="relayly-read"),
            asyncio.create_task(self._write_loop(), name="relayly-write"),
            asyncio.create_task(self._ping_loop(), name="relayly-ping"),
        ]

    async def send(self, peer_id: str, payload: bytes) -> None:
        """Encrypt and send a message to a paired peer device."""
        peer = self._peers.get(peer_id)
        if peer is None:
            raise ValueError(f"relayly: no paired peer '{peer_id}' — pair first")

        ciphertext, nonce = self._opts.private_key.encrypt(payload, PublicKey(peer.public_key))
        await self._enqueue({
            "type": "send",
            "to": peer_id,
            "payload": base64.b64encode(ciphertext).decode(),
            "nonce": base64.b64encode(nonce).decode(),
        })

    async def request_pair_code(self) -> PairCode:
        """Ask the server for a 6-digit pairing code to share with another device."""
        loop = asyncio.get_running_loop()

        # Register waiter before enqueuing — avoids missing a fast server response.
        req_future: asyncio.Future[_PairResult] = loop.create_future()
        self._pair_waiters["__request__"] = req_future
        await self._enqueue({"type": "pair_request"})

        try:
            result = await asyncio.wait_for(req_future, timeout=10.0)
        except asyncio.TimeoutError:
            self._pair_waiters.pop("__request__", None)
            raise TimeoutError("relayly: timed out waiting for pair code from server")
        if result.error:
            raise result.error

        complete_future: asyncio.Future[_PairResult] = loop.create_future()
        self._pair_waiters[result.code] = complete_future
        return PairCode(short=result.code, expires_in=result.expires_in, client=self, future=complete_future)

    async def accept_pair(self, code: str) -> Peer:
        """Accept a pairing code from another device."""
        loop = asyncio.get_running_loop()
        future: asyncio.Future[_PairResult] = loop.create_future()
        self._pair_waiters[code] = future
        await self._enqueue({"type": "pair_accept", "code": code})

        try:
            result = await asyncio.wait_for(future, timeout=10.0)
        except asyncio.TimeoutError:
            self._pair_waiters.pop(code, None)
            raise TimeoutError("relayly: timed out waiting for pair completion")
        if result.error:
            raise result.error
        return Peer(id=result.peer_id, public_key=result.peer_public_key)

    async def messages(self) -> AsyncGenerator[Message, None]:
        """Async generator that yields decrypted messages from paired peers.

        The generator ends when the client is closed::

            async for msg in client.messages():
                print(f"[{msg.from_device}] {msg.payload.decode()}")
        """
        while True:
            msg = await self._message_queue.get()
            if msg is None:
                return
            yield msg

    async def close(self) -> None:
        """Gracefully shut down the client and stop all background tasks."""
        if self._closed:
            return
        self._closed = True
        self._stop.set()

        if self._ws is not None:
            try:
                await self._ws.close()
            except Exception:
                pass

        # Unblock write loop sentinel
        await self._send_queue.put(None)

        for task in self._tasks:
            task.cancel()
        await asyncio.gather(*self._tasks, return_exceptions=True)
        self._tasks.clear()

        # Drain any pending pair waiters
        for fut in self._pair_waiters.values():
            if not fut.done():
                fut.set_exception(Exception("relayly: client closed"))
        self._pair_waiters.clear()

    # ------------------------------------------------------------------
    # Background loops
    # ------------------------------------------------------------------

    async def _read_loop(self) -> None:
        try:
            while True:
                ws = self._ws
                assert ws is not None
                try:
                    async for raw in ws:
                        try:
                            frame = json.loads(raw)
                        except Exception:
                            continue
                        self._dispatch(frame)
                except Exception as exc:
                    if self._closed:
                        return
                    if not await self._reconnect_with_backoff(exc):
                        return
        finally:
            await self._message_queue.put(None)  # signal messages() to stop

    async def _write_loop(self) -> None:
        while True:
            msg = await self._send_queue.get()
            if msg is None:
                return
            ws = self._ws
            if ws is None:
                continue
            try:
                await ws.send(json.dumps(msg))
            except Exception:
                pass  # drop; _read_loop handles reconnect

    async def _ping_loop(self) -> None:
        interval = self._opts.ping_interval
        while True:
            try:
                await asyncio.wait_for(self._stop.wait(), timeout=interval)
                return  # stop event fired
            except asyncio.TimeoutError:
                pass
            if self._closed:
                return
            await self._enqueue({"type": "ping"})

    # ------------------------------------------------------------------
    # Reconnect
    # ------------------------------------------------------------------

    async def _reconnect_with_backoff(self, cause: Exception) -> bool:
        if self._opts.on_disconnect is not None:
            try:
                self._opts.on_disconnect(cause)
            except Exception:
                pass

        if self._opts.reconnect_delay < 0:
            return False

        delay = max(self._opts.reconnect_delay, 0.1)
        max_delay = self._opts.max_reconnect_delay

        while True:
            try:
                await asyncio.wait_for(self._stop.wait(), timeout=delay)
                return False  # close() was called
            except asyncio.TimeoutError:
                pass

            if self._closed:
                return False

            try:
                await self._dial()
                if self._opts.on_reconnect is not None:
                    try:
                        self._opts.on_reconnect()
                    except Exception:
                        pass
                return True
            except Exception:
                delay = min(delay * 2, max_delay)

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    async def _dial(self) -> None:
        ws = await websockets.connect(self._server_url, open_timeout=10)
        self._ws = ws

        pub_b64 = self._opts.private_key.public_key.to_base64()
        await ws.send(json.dumps({
            "type": "auth",
            "device_id": self._opts.device_id,
            "public_key": pub_b64,
        }))

        raw = await asyncio.wait_for(ws.recv(), timeout=10.0)
        resp = json.loads(raw)

        if resp.get("type") == "error":
            await ws.close()
            raise ConnectionError(
                f"relayly: authentication failed: {resp.get('message')} ({resp.get('code')})"
            )
        if resp.get("type") != "auth_ok":
            await ws.close()
            raise ConnectionError(
                f"relayly: unexpected auth response: {resp.get('type')}"
            )

    def _dispatch(self, frame: dict) -> None:
        t = frame.get("type", "")

        if t == "message":
            self._handle_message(frame)

        elif t == "pair_code":
            fut = self._pair_waiters.pop("__request__", None)
            if fut is not None and not fut.done():
                fut.set_result(_PairResult(
                    code=frame.get("code", ""),
                    expires_in=frame.get("expires_in", 0),
                ))

        elif t == "pair_complete":
            raw_pub = frame.get("peer_public_key", "")
            try:
                pub_bytes = base64.b64decode(raw_pub)
            except Exception:
                return
            peer = Peer(id=frame.get("peer_id", ""), public_key=pub_bytes)
            self._peers[peer.id] = peer
            code = frame.get("code", "")
            fut = self._pair_waiters.pop(code, None)
            if fut is not None and not fut.done():
                fut.set_result(_PairResult(peer_id=peer.id, peer_public_key=pub_bytes))

        elif t == "error":
            exc = Exception(f"{frame.get('code', 'error')}: {frame.get('message', '')}")
            for fut in list(self._pair_waiters.values()):
                if not fut.done():
                    fut.set_exception(exc)
            self._pair_waiters.clear()

    def _handle_message(self, frame: dict) -> None:
        sender_id = frame.get("from", "")
        peer = self._peers.get(sender_id)
        if peer is None:
            return
        try:
            ciphertext = base64.b64decode(frame["payload"])
            nonce = base64.b64decode(frame["nonce"])
            plaintext = self._opts.private_key.decrypt(ciphertext, nonce, PublicKey(peer.public_key))
        except Exception:
            return

        ts_str = frame.get("timestamp")
        ts: datetime | None = None
        if ts_str:
            try:
                ts = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
            except Exception:
                pass

        try:
            self._message_queue.put_nowait(Message(from_device=sender_id, payload=plaintext, timestamp=ts))
        except asyncio.QueueFull:
            pass  # drop; caller should consume promptly

    async def _enqueue(self, msg: dict) -> None:
        if self._closed:
            raise RuntimeError("relayly: client is closed")
        await self._send_queue.put(msg)


# ---------------------------------------------------------------------------
# Factory
# ---------------------------------------------------------------------------


async def connect(server_url: str, opts: Options) -> Client:
    """Dial a Relayly server and return an authenticated, ready-to-use Client.

    Example::

        key = load_or_generate_key("~/.relayly/device.key")
        client = await connect("wss://relay.example.com", Options(
            device_id="my-laptop",
            private_key=key,
        ))
        async for msg in client.messages():
            print(msg.payload.decode())
    """
    if not opts.device_id:
        raise ValueError("relayly: Options.device_id is required")

    client = Client(server_url, opts)
    await client._dial()
    client._start_loops()
    return client


def _normalise_ws_url(url: str) -> str:
    parsed = urlparse(url)
    if parsed.scheme == "http":
        parsed = parsed._replace(scheme="ws")
    elif parsed.scheme == "https":
        parsed = parsed._replace(scheme="wss")
    return urlunparse(parsed)
