//! Self-pair integration test: builds and runs the real cmd/relayly server binary
//! (not an in-process test double) and drives two sdk/rust Clients through it end to
//! end — register, connect, pair, a real Noise XX handshake, and bidirectional
//! encrypted delivery. This is the "each SDK against itself" leg of the interop
//! matrix (docs/tasks/02-sdks-and-interop.md), matching sdk/go's client_test.go,
//! sdk/ts's client.test.ts, and sdk/py's test_client.py. Writing this test is what
//! caught real wiring bugs in both the sdk/ts PR (missing auth query params hanging
//! connect()) and the sdk/py PR (an event-loop busy loop on close, and both pairing
//! sides racing to initiate the handshake) — it exercises exactly the paths those
//! bugs lived in.

use std::{
    io::{Read, Write},
    net::TcpStream,
    path::{Path, PathBuf},
    process::{Child, Command},
    time::{Duration, Instant},
};

use relayly::{connect, generate_key, Options};
use tempfile::TempDir;

fn repo_root() -> PathBuf {
    // sdk/rust is two levels under the repo root.
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).parent().unwrap().parent().unwrap().to_path_buf()
}

fn free_port() -> u16 {
    std::net::TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port()
}

fn build_server(bin_path: &Path) {
    let status = Command::new("go")
        .args(["build", "-o", bin_path.to_str().unwrap(), "./cmd/relayly"])
        .current_dir(repo_root())
        .status()
        .expect("failed to run go build");
    assert!(status.success(), "go build failed");
}

/// Minimal raw HTTP/1.1 client — deliberately hand-rolled instead of adding an HTTP
/// client dependency just for these two calls against a local test server.
fn http_request(port: u16, method: &str, path: &str, body: Option<&str>) -> (u16, String) {
    let mut stream = TcpStream::connect(("127.0.0.1", port)).unwrap();
    let body = body.unwrap_or("");
    let mut request = format!("{method} {path} HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n");
    if !body.is_empty() {
        request.push_str("Content-Type: application/json\r\n");
        request.push_str(&format!("Content-Length: {}\r\n", body.len()));
    }
    request.push_str("\r\n");
    request.push_str(body);

    stream.write_all(request.as_bytes()).unwrap();
    let mut response = String::new();
    stream.read_to_string(&mut response).unwrap();

    let status_line = response.lines().next().unwrap_or("");
    let status: u16 = status_line.split_whitespace().nth(1).and_then(|s| s.parse().ok()).unwrap_or(0);
    let body_start = response.find("\r\n\r\n").map(|i| i + 4).unwrap_or(response.len());
    (status, response[body_start..].to_string())
}

fn wait_for_health(port: u16, deadline: Instant) {
    while Instant::now() < deadline {
        let (status, _) = std::panic::catch_unwind(|| http_request(port, "GET", "/health", None))
            .unwrap_or((0, String::new()));
        if status == 200 {
            return;
        }
        std::thread::sleep(Duration::from_millis(50));
    }
    panic!("relayly: server did not become healthy in time");
}

struct DeviceCreds {
    device_id: String,
    device_token: String,
}

fn register_device(port: u16, name: &str) -> DeviceCreds {
    let body = format!(r#"{{"name":"{name}"}}"#);
    let (status, resp) = http_request(port, "POST", "/api/v1/devices", Some(&body));
    assert_eq!(status, 200, "registering device {name}: {resp}");
    let v: serde_json::Value = serde_json::from_str(&resp).unwrap();
    DeviceCreds {
        device_id: v["device_id"].as_str().unwrap().to_string(),
        device_token: v["device_token"].as_str().unwrap().to_string(),
    }
}

struct RunningServer {
    child: Child,
    _db_dir: TempDir,
    port: u16,
}

impl RunningServer {
    fn ws_url(&self) -> String {
        format!("ws://127.0.0.1:{}/ws", self.port)
    }
}

impl Drop for RunningServer {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn start_server(bin_path: &Path) -> RunningServer {
    let port = free_port();
    let db_dir = TempDir::new().unwrap();
    let db_path = db_dir.path().join("relayly.db");

    let child = Command::new(bin_path)
        .args([
            "start",
            "--host",
            "127.0.0.1",
            "--port",
            &port.to_string(),
            "--db.path",
            db_path.to_str().unwrap(),
        ])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .expect("failed to start relayly server");

    wait_for_health(port, Instant::now() + Duration::from_secs(10));
    RunningServer { child, _db_dir: db_dir, port }
}

#[tokio::test]
async fn self_pair_registers_connects_pairs_and_exchanges_messages_both_ways() {
    let build_dir = TempDir::new().unwrap();
    let bin_path = build_dir.path().join("relayly-server");
    build_server(&bin_path);

    let server = start_server(&bin_path);

    let dev_a = register_device(server.port, "device-a");
    let dev_b = register_device(server.port, "device-b");

    let (client_a, mut messages_a) = connect(
        &server.ws_url(),
        Options { device_id: dev_a.device_id.clone(), device_token: dev_a.device_token, private_key: generate_key(), ..Default::default() },
    )
    .await
    .unwrap();
    let (client_b, mut messages_b) = connect(
        &server.ws_url(),
        Options { device_id: dev_b.device_id.clone(), device_token: dev_b.device_token, private_key: generate_key(), ..Default::default() },
    )
    .await
    .unwrap();

    let code = client_a.request_pair_code().await.unwrap();
    assert_eq!(code.short.len(), 6);

    let peer_b = client_b.accept_pair(&code.short).await.unwrap();
    let peer_a = code.wait().await.unwrap();

    assert_eq!(peer_a.id, dev_b.device_id);
    assert_eq!(peer_b.id, dev_a.device_id);

    client_a.send(&peer_a.id, b"hello from A").await.unwrap();
    let msg_at_b = tokio::time::timeout(Duration::from_secs(5), messages_b.recv()).await.unwrap().unwrap();
    assert_eq!(msg_at_b.payload, b"hello from A");
    assert_eq!(msg_at_b.from, dev_a.device_id);

    client_b.send(&peer_b.id, b"hello from B").await.unwrap();
    let msg_at_a = tokio::time::timeout(Duration::from_secs(5), messages_a.recv()).await.unwrap().unwrap();
    assert_eq!(msg_at_a.payload, b"hello from B");
    assert_eq!(msg_at_a.from, dev_b.device_id);

    client_a.close().await;
    client_b.close().await;
}
