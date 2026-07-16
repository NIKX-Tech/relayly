from __future__ import annotations

import asyncio
import base64
import dataclasses
import json
from datetime import datetime
from typing import Any, Callable
from urllib.parse import parse_qsl, urlencode, urlparse, urlunparse

import websockets
import websockets.exceptions

from ._crypto import PrivateKey
from ._errors import RelaylyError, error_for_code
from ._noise import ENVELOPE_HANDSHAKE, ENVELOPE_TRANSPORT, NoiseSession, decode_envelope, encode_envelope
from ._peer import PeerConn
from ._peerstore import DEFAULT_PEER_STORE_PATH, PeerKeyMismatchError, PeerStore

PROTOCOL_VERSION = 1

MSG_WELCOME = "welcome"
MSG_ANNOUNCE_KEY = "announce_key"
MSG_PAIR_REQUEST = "pair_request"
MSG_PAIR_CODE = "pair_code"
MSG_PAIR_ACCEPT = "pair_accept"
MSG_PAIR_COMPLETE = "pair_complete"
MSG_PEER_STATUS = "peer_status"
MSG_PING = "ping"
MSG_PONG = "pong"
MSG_ERROR = "error"

# Throttles how often this client re-initiates a rekey after a reconnect notices the
# peer is (still) online — mirrors the request-side rate limiting sdk/go/sdk/ts apply
# on the responder side; here it just prevents redundant sends if peer_status fires
# more than once in quick succession.


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
            device_token=token,  # from POST /api/v1/devices
            private_key=generate_key(),
        )
    """

    device_id: str
    device_token: str
    private_key: PrivateKey
    peer_store_path: str = DEFAULT_PEER_STORE_PATH
    ping_interval: float = 30.0
    reconnect_delay: float = 1.0  # seconds; set to -1 to disable
    max_reconnect_delay: float = 60.0
    on_reconnect: Callable[[], None] | None = None
    on_disconnect: Callable[[Exception], None] | None = None
    # Called whenever a peer's Noise session becomes usable for send() — both after
    # the very first pairing and after any later re-handshake following a reconnect
    # (docs/PROTOCOL.md §6). request_pair_code()/accept_pair() already block until the
    # first handshake completes, so this is mainly useful for noticing recovery.
    on_ready: Callable[[str], None] | None = None
    # Called whenever the server reports the paired peer's online/offline transition.
    on_peer_status: Callable[[str, bool], None] | None = None


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


@dataclasses.dataclass
class _PairWaiter:
    """What a pending request_pair_code/accept_pair call is waiting on. Both sides of
    a pairing register a waiter under the same code — we_are_acceptor is what lets
    _handle_pair_complete tell them apart (docs/PROTOCOL.md §5.3: only the accepting
    device initiates the Noise handshake). Without this distinction both sides would
    call start_as_initiator() and send competing msg1s."""

    future: asyncio.Future
    we_are_acceptor: bool = False


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
        """Block until the other device accepts the pairing (including the resulting
        Noise handshake completing)."""
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

        self._peer_store = PeerStore.load(opts.peer_store_path)
        self._peers: dict[str, PeerConn] = {}
        self._message_queue: asyncio.Queue[Message | None] = asyncio.Queue(maxsize=64)
        self._send_queue: asyncio.Queue[str | bytes | None] = asyncio.Queue(maxsize=64)
        self._pair_waiters: dict[str, _PairWaiter] = {}

        self._tasks: list[asyncio.Task] = []

    def _start_loops(self) -> None:
        self._tasks = [
            asyncio.create_task(self._read_loop(), name="relayly-read"),
            asyncio.create_task(self._write_loop(), name="relayly-write"),
            asyncio.create_task(self._ping_loop(), name="relayly-ping"),
        ]

    async def send(self, peer_id: str, payload: bytes) -> None:
        """Encrypt and send a message to a paired peer device.

        Raises NotReadyError if the peer's Noise session isn't up yet — only expected
        in the brief window after a reconnect forces a re-handshake.
        """
        peer = self._peers.get(peer_id)
        if peer is None:
            raise ValueError(f"relayly: no paired peer '{peer_id}' — pair first")

        ciphertext = peer.send(payload)  # raises NotReadyError if not ready
        await self._enqueue(encode_envelope(ENVELOPE_TRANSPORT, ciphertext))

    async def request_pair_code(self) -> PairCode:
        """Ask the server for a 6-digit pairing code to share with another device."""
        loop = asyncio.get_running_loop()

        # Register waiter before enqueuing — avoids missing a fast server response.
        req_future: asyncio.Future[_PairResult] = loop.create_future()
        self._pair_waiters["__request__"] = _PairWaiter(req_future)
        await self._enqueue_control({"type": MSG_PAIR_REQUEST})

        try:
            result = await asyncio.wait_for(req_future, timeout=10.0)
        except asyncio.TimeoutError:
            self._pair_waiters.pop("__request__", None)
            raise TimeoutError("relayly: timed out waiting for pair code from server") from None
        if result.error:
            raise result.error

        complete_future: asyncio.Future[_PairResult] = loop.create_future()
        self._pair_waiters[result.code] = _PairWaiter(complete_future, we_are_acceptor=False)
        return PairCode(short=result.code, expires_in=result.expires_in, client=self, future=complete_future)

    async def accept_pair(self, code: str) -> Peer:
        """Accept a pairing code from another device. Blocks until the resulting Noise
        handshake completes (docs/PROTOCOL.md §5.3: the accepting device is the
        initiator), so the returned Peer can be sent to immediately."""
        loop = asyncio.get_running_loop()
        future: asyncio.Future[_PairResult] = loop.create_future()
        self._pair_waiters[code] = _PairWaiter(future, we_are_acceptor=True)
        await self._enqueue_control({"type": MSG_PAIR_ACCEPT, "code": code})

        try:
            result = await asyncio.wait_for(future, timeout=10.0)
        except asyncio.TimeoutError:
            self._pair_waiters.pop(code, None)
            raise TimeoutError("relayly: timed out waiting for pair completion") from None
        if result.error:
            raise result.error
        return Peer(id=result.peer_id, public_key=result.peer_public_key)

    async def messages(self):
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

        await self._send_queue.put(None)  # unblock write loop sentinel

        for task in self._tasks:
            task.cancel()
        await asyncio.gather(*self._tasks, return_exceptions=True)
        self._tasks.clear()

        for waiter in self._pair_waiters.values():
            if not waiter.future.done():
                waiter.future.set_exception(Exception("relayly: client closed"))
        self._pair_waiters.clear()

    # ------------------------------------------------------------------
    # Background loops
    # ------------------------------------------------------------------

    async def _read_loop(self) -> None:
        try:
            while True:
                ws = self._ws
                assert ws is not None
                # websockets' `async for` ends the iteration silently (no exception)
                # on a clean close (ConnectionClosedOK) and only raises on a protocol
                # error. Both cases mean the connection is gone, so both must be
                # handled the same way below — falling through to a bare `continue`
                # here would re-enter `async for` on the same now-closed socket, which
                # also ends instantly, spinning the loop at 100% CPU and starving the
                # event loop (this was a real hang, caught by the self-pair
                # integration test never returning from close()).
                exc: Exception | None = None
                try:
                    async for raw in ws:
                        if isinstance(raw, bytes):
                            self._handle_binary_frame(raw)
                        else:
                            self._handle_control_message(raw)
                except Exception as caught:
                    exc = caught

                if self._closed:
                    return
                if not await self._reconnect_with_backoff(exc or ConnectionError("relayly: connection closed")):
                    return
        finally:
            await self._message_queue.put(None)  # signal messages() to stop

    def _handle_binary_frame(self, raw: bytes) -> None:
        decoded = decode_envelope(raw)
        if decoded is None:
            return
        kind, payload = decoded
        if kind == ENVELOPE_HANDSHAKE:
            self._handle_handshake_envelope(payload)
        elif kind == ENVELOPE_TRANSPORT:
            self._handle_transport_envelope(payload)

    async def _write_loop(self) -> None:
        while True:
            msg = await self._send_queue.get()
            if msg is None:
                return
            ws = self._ws
            if ws is None:
                continue
            try:
                await ws.send(msg)
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
            await self._enqueue_control({"type": MSG_PING})

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
    # Connect / dial
    # ------------------------------------------------------------------

    async def _dial(self) -> None:
        parsed = urlparse(self._server_url)
        query = dict(parse_qsl(parsed.query))
        query["device_id"] = self._opts.device_id
        query["token"] = self._opts.device_token
        url = urlunparse(parsed._replace(query=urlencode(query)))

        ws = await websockets.connect(url, open_timeout=10)
        self._ws = ws

        raw = await asyncio.wait_for(ws.recv(), timeout=10.0)
        if isinstance(raw, bytes):
            await ws.close()
            raise ConnectionError("relayly: expected a text welcome frame")
        frame = _json_loads(raw)
        if frame is None:
            await ws.close()
            raise ConnectionError("relayly: invalid welcome frame")

        if frame.get("type") == MSG_ERROR:
            await ws.close()
            raise error_for_code(frame.get("code", ""), frame.get("message", ""))
        if frame.get("type") != MSG_WELCOME:
            await ws.close()
            raise ConnectionError(f"relayly: unexpected first frame type: {frame.get('type')}")
        if frame.get("protocol_version") != PROTOCOL_VERSION:
            await ws.close()
            raise ConnectionError(
                f"relayly: unsupported protocol_version {frame.get('protocol_version')} "
                f"(this SDK implements {PROTOCOL_VERSION})"
            )

        # A reconnect must not discard an already-healthy PeerConn just because the
        # control channel briefly reset — only register peers we don't already track.
        for p in frame.get("peers") or []:
            if p.get("id") and p["id"] not in self._peers:
                self._peers[p["id"]] = PeerConn(p["id"], p.get("static_key", ""))

        pub_b64 = self._opts.private_key.public_key.to_base64()
        await ws.send(_json_dumps({"type": MSG_ANNOUNCE_KEY, "static_key": pub_b64}))

    # ------------------------------------------------------------------
    # Control dispatch
    # ------------------------------------------------------------------

    def _handle_control_message(self, raw: str) -> None:
        frame = _json_loads(raw)
        if frame is None:
            return  # skip malformed frames

        t = frame.get("type", "")
        if t == MSG_PAIR_CODE:
            self._handle_pair_code(frame)
        elif t == MSG_PAIR_COMPLETE:
            self._handle_pair_complete(frame)
        elif t == MSG_PEER_STATUS:
            self._handle_peer_status(frame)
        elif t == MSG_ERROR:
            self._handle_error(frame)
        elif t == MSG_PONG:
            pass  # keepalive, nothing to do
        # unknown type: ignored per §5's forward-compatibility rule

    def _handle_pair_code(self, frame: dict[str, Any]) -> None:
        waiter = self._pair_waiters.pop("__request__", None)
        if waiter is not None and not waiter.future.done():
            waiter.future.set_result(_PairResult(code=frame.get("code", ""), expires_in=frame.get("expires_in", 0)))

    def _handle_pair_complete(self, frame: dict[str, Any]) -> None:
        """Implements §5.3: link the peer, and if we're the accepting device, start
        the Noise handshake as initiator immediately."""
        peer_id = frame.get("peer_id")
        if not peer_id:
            return

        code = frame.get("code", "")
        waiter = self._pair_waiters.pop(code, None) if code else None

        peer = self._peers.get(peer_id)
        if peer is None:
            peer = PeerConn(peer_id, frame.get("peer_static_key", ""))
            self._peers[peer_id] = peer
        else:
            peer.announced_static_key = frame.get("peer_static_key", "")

        if waiter is None:
            return  # neither request_pair_code nor accept_pair is waiting on this code

        peer.set_first_pair_waiter(waiter.future)
        if not waiter.we_are_acceptor:
            return  # we requested; wait for the accepting device's incoming msg1

        try:
            msg1 = peer.start_as_initiator(self._opts.private_key)
        except Exception as exc:
            first_waiter = peer.take_first_pair_waiter()
            if first_waiter is not None and not first_waiter.done():
                first_waiter.set_result(_PairResult(error=exc))
            return
        self._enqueue_nowait(encode_envelope(ENVELOPE_HANDSHAKE, msg1))

    def _handle_peer_status(self, frame: dict[str, Any]) -> None:
        """Implements §6's reconnect rule: whichever side's device_id is
        lexicographically smaller re-initiates a fresh handshake whenever the peer
        comes online."""
        online = frame.get("online") is True
        peer_id = frame.get("peer_id")
        if peer_id and self._opts.on_peer_status is not None:
            try:
                self._opts.on_peer_status(peer_id, online)
            except Exception:
                pass
        if not online or not peer_id:
            return

        peer = self._peers.get(peer_id)
        if peer is None:
            return
        if self._opts.device_id >= peer_id:
            return  # larger ID: wait for the incoming msg1

        try:
            msg1 = peer.start_rekey_as_initiator(self._opts.private_key)
        except Exception:
            return  # best-effort; a future peer_status or AEAD failure can retry
        self._enqueue_nowait(encode_envelope(ENVELOPE_HANDSHAKE, msg1))

    def _handle_error(self, frame: dict[str, Any]) -> None:
        exc = error_for_code(frame.get("code", ""), frame.get("message", ""))
        for waiter in list(self._pair_waiters.values()):
            if not waiter.future.done():
                waiter.future.set_result(_PairResult(error=exc))
        self._pair_waiters.clear()

    # ------------------------------------------------------------------
    # Binary envelope dispatch
    # ------------------------------------------------------------------

    def _get_sole_peer(self) -> PeerConn | None:
        """v1 supports exactly one linked peer per device (docs/PROTOCOL.md §5.3).
        Incoming binary envelopes carry no sender field — the relay only ever forwards
        them from the paired device."""
        return next(iter(self._peers.values()), None)

    def _handle_handshake_envelope(self, payload: bytes) -> None:
        peer = self._get_sole_peer()
        if peer is None:
            return

        reply, completed, was_pending = peer.handle_handshake_envelope(self._opts.private_key, payload)
        if reply is not None:
            self._enqueue_nowait(encode_envelope(ENVELOPE_HANDSHAKE, reply))
        if completed is not None:
            self._resolve_handshake(peer, completed, was_pending)

    def _resolve_handshake(self, peer: PeerConn, session: NoiseSession, was_pending: bool) -> None:
        """Runs once, exactly when a Noise handshake attempt finishes: applies §7's
        client-side pin (the real boundary) and server-announced-key cross-check,
        promotes or abandons a rekey attempt per §6's make-before-break rule, notifies
        a first-pairing waiter if one is registered, and calls on_ready."""
        error: Exception | None = None
        public_key: bytes | None = None

        if session.failed:
            error = RelaylyError(f"relayly: handshake with {peer.id} failed")
        else:
            authenticated = session.peer_static_key
            authenticated_b64 = base64.b64encode(authenticated).decode()
            try:
                self._peer_store.pin_or_verify(peer.id, authenticated_b64)
                if peer.announced_static_key and peer.announced_static_key != authenticated_b64:
                    error = error_for_code(
                        "key_mismatch",
                        f"server-announced key for peer {peer.id} does not match the authenticated handshake",
                    )
                else:
                    public_key = authenticated
            except PeerKeyMismatchError as exc:
                error = exc

        if was_pending:
            if error is not None:
                peer.abandon(session)  # existing active session, if any, is untouched
                return
            peer.promote(session)

        waiter = peer.take_first_pair_waiter()
        if error is not None:
            if waiter is not None and not waiter.done():
                waiter.set_result(_PairResult(error=error))
            return

        if waiter is not None and not waiter.done():
            waiter.set_result(_PairResult(peer_id=peer.id, peer_public_key=public_key or b""))
        if self._opts.on_ready is not None:
            try:
                self._opts.on_ready(peer.id)
            except Exception:
                pass

    def _handle_transport_envelope(self, ciphertext: bytes) -> None:
        peer = self._get_sole_peer()
        if peer is None:
            return

        try:
            plaintext = peer.recv(ciphertext)
        except Exception:
            return  # not ready, or decryption failed — drop silently

        try:
            self._message_queue.put_nowait(
                Message(from_device=peer.id, payload=plaintext, timestamp=datetime.now())
            )
        except asyncio.QueueFull:
            pass  # drop; caller should consume promptly

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    async def _enqueue(self, msg: str | bytes) -> None:
        if self._closed:
            raise RuntimeError("relayly: client is closed")
        await self._send_queue.put(msg)

    async def _enqueue_control(self, msg: dict[str, Any]) -> None:
        await self._enqueue(_json_dumps(msg))

    def _enqueue_nowait(self, msg: str | bytes) -> None:
        try:
            self._send_queue.put_nowait(msg)
        except asyncio.QueueFull:
            pass


# ---------------------------------------------------------------------------
# Factory
# ---------------------------------------------------------------------------


async def connect(server_url: str, opts: Options) -> Client:
    """Dial a Relayly server and return an authenticated, ready-to-use Client.

    Example::

        key = load_or_generate_key("~/.relayly/device.key")
        client = await connect("wss://relay.example.com", Options(
            device_id="my-laptop",
            device_token=token,  # from POST /api/v1/devices
            private_key=key,
        ))
        async for msg in client.messages():
            print(msg.payload.decode())
    """
    if not opts.device_id:
        raise ValueError("relayly: Options.device_id is required")
    if not opts.device_token:
        raise ValueError("relayly: Options.device_token is required")

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


def _json_dumps(obj: dict[str, Any]) -> str:
    return json.dumps(obj)


def _json_loads(raw: str) -> dict[str, Any] | None:
    try:
        return json.loads(raw)
    except Exception:
        return None
