"""Self-pair integration test: builds and runs the real cmd/relayly server binary (not
an in-process test double) and drives two sdk/py Clients through it end to end —
register, connect, pair, a real Noise XX handshake, and bidirectional encrypted
delivery. This is the "each SDK against itself" leg of the interop matrix
(docs/tasks/02-sdks-and-interop.md), landed for the same reason sdk/go's
client_test.go and sdk/ts's client.test.ts are: it is what actually catches a wiring
bug like a missing auth query param, which unit tests of individual pieces cannot.
"""
from __future__ import annotations

import asyncio
import json
import socket
import subprocess
import tempfile
import time
import urllib.request
from pathlib import Path

import pytest

import relayly


def _repo_root() -> Path:
    # sdk/py/tests is three levels under the repo root.
    return Path(__file__).resolve().parents[3]


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _build_server(build_dir: Path) -> Path:
    bin_path = build_dir / "relayly-server"
    subprocess.run(
        ["go", "build", "-o", str(bin_path), "./cmd/relayly"],
        cwd=_repo_root(),
        check=True,
        capture_output=True,
    )
    return bin_path


def _wait_for_health(base_url: str, deadline: float) -> None:
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(f"{base_url}/health", timeout=1) as resp:
                if resp.status == 200:
                    return
        except Exception:
            pass
        time.sleep(0.05)
    raise TimeoutError("relayly: server did not become healthy in time")


def _register_device(base_url: str, name: str) -> dict:
    body = json.dumps({"name": name}).encode()
    req = urllib.request.Request(
        f"{base_url}/api/v1/devices", data=body, headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        return json.loads(resp.read())


@pytest.fixture
def running_server(tmp_path):
    build_dir = tmp_path / "build"
    build_dir.mkdir()
    bin_path = _build_server(build_dir)

    port = _free_port()
    db_path = tmp_path / "relayly.db"
    # cwd intentionally left as the test runner's cwd (sdk/py), which has no
    # config/relayly.yaml reachable via viper's "./config"/"." search paths, so the
    # server runs on its correctly-typed built-in defaults (30s ping/60s deadline).
    proc = subprocess.Popen(
        [
            str(bin_path),
            "start",
            "--host",
            "127.0.0.1",
            "--port",
            str(port),
            "--db.path",
            str(db_path),
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    base_url = f"http://127.0.0.1:{port}"
    try:
        _wait_for_health(base_url, time.monotonic() + 10)
        yield base_url, f"ws://127.0.0.1:{port}/ws"
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()


async def test_self_pair_register_connect_pair_and_exchange_messages(running_server, tmp_path):
    base_url, ws_url = running_server

    dev_a = _register_device(base_url, "device-a")
    dev_b = _register_device(base_url, "device-b")

    client_a = await relayly.connect(
        ws_url,
        relayly.Options(
            device_id=dev_a["device_id"],
            device_token=dev_a["device_token"],
            private_key=relayly.generate_key(),
            peer_store_path=str(tmp_path / "peers_a.json"),
        ),
    )
    client_b = await relayly.connect(
        ws_url,
        relayly.Options(
            device_id=dev_b["device_id"],
            device_token=dev_b["device_token"],
            private_key=relayly.generate_key(),
            peer_store_path=str(tmp_path / "peers_b.json"),
        ),
    )

    try:
        code = await client_a.request_pair_code()
        assert len(code.short) == 6

        peer_b = await client_b.accept_pair(code.short)
        peer_a = await code.wait()

        assert peer_a.id == dev_b["device_id"]
        assert peer_b.id == dev_a["device_id"]

        await client_a.send(peer_a.id, b"hello from A")
        msg_at_b = await asyncio.wait_for(client_b.messages().__anext__(), timeout=5)
        assert msg_at_b.payload == b"hello from A"
        assert msg_at_b.from_device == dev_a["device_id"]

        await client_b.send(peer_b.id, b"hello from B")
        msg_at_a = await asyncio.wait_for(client_a.messages().__anext__(), timeout=5)
        assert msg_at_a.payload == b"hello from B"
        assert msg_at_a.from_device == dev_b["device_id"]
    finally:
        await client_a.close()
        await client_b.close()
