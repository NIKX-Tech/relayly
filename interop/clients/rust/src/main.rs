//! A thin CLI wrapper around sdk/rust's public API, driven by newline-delimited JSON
//! over stdin/stdout. Exists only for the interop harness (interop/harness/) to drive
//! a real relayly::Client as a subprocess — no internal/test-only hooks, proving the
//! public API alone is enough for interop testing. Protocol matches
//! interop/clients/go/main.go exactly (see its doc comment for the full command/
//! event list).

use std::{
    io::Write,
    sync::Mutex,
};

use base64::{engine::general_purpose::STANDARD, Engine};
use relayly::{connect, generate_key, Options};
use serde_json::{json, Value};
use tokio::io::{AsyncBufReadExt, BufReader};

static STDOUT_LOCK: Mutex<()> = Mutex::new(());

fn emit(v: Value) {
    let _guard = STDOUT_LOCK.lock().unwrap();
    let mut out = std::io::stdout();
    let _ = writeln!(out, "{v}");
    let _ = out.flush();
}

fn arg(flag: &str) -> Option<String> {
    let args: Vec<String> = std::env::args().collect();
    args.iter().position(|a| a == flag).and_then(|i| args.get(i + 1)).cloned()
}

#[tokio::main]
async fn main() {
    let server = arg("--server").expect("--server is required");
    let device_id = arg("--device-id").expect("--device-id is required");
    let device_token = arg("--device-token").expect("--device-token is required");
    let peer_store_path = arg("--peer-store-path");

    let mut opts = Options {
        device_id,
        device_token,
        private_key: generate_key(),
        on_ready: Some(Box::new(|peer_id: &str| {
            emit(json!({"event": "ready_signal", "peer_id": peer_id}));
        })),
        on_peer_status: Some(Box::new(|peer_id: &str, online: bool| {
            emit(json!({"event": "peer_status", "peer_id": peer_id, "online": online}));
        })),
        ..Default::default()
    };
    if let Some(path) = peer_store_path {
        opts.peer_store_path = Some(path.into());
    }

    let (client, mut messages) = match connect(&server, opts).await {
        Ok(pair) => pair,
        Err(e) => {
            emit(json!({"event": "connect_error", "message": e.to_string()}));
            std::process::exit(1);
        }
    };

    tokio::spawn(async move {
        while let Some(msg) = messages.recv().await {
            emit(json!({
                "event": "message",
                "from": msg.from,
                "payload_b64": STANDARD.encode(&msg.payload),
            }));
        }
    });

    emit(json!({"event": "ready"}));

    let stdin = tokio::io::stdin();
    let mut lines = BufReader::new(stdin).lines();
    while let Ok(Some(line)) = lines.next_line().await {
        let Ok(cmd) = serde_json::from_str::<Value>(&line) else { continue };
        let is_close = cmd["cmd"] == "close";
        handle_command(&client, cmd).await;
        if is_close {
            break;
        }
    }
}

async fn handle_command(client: &relayly::Client, cmd: Value) {
    match cmd["cmd"].as_str() {
        Some("request_pair_code") => {
            let client = client.clone();
            tokio::spawn(async move {
                match client.request_pair_code().await {
                    Ok(code) => {
                        emit(json!({"event": "pair_code", "code": code.short, "expires_in": code.expires_in}));
                        match code.wait().await {
                            Ok(peer) => emit(json!({
                                "event": "paired",
                                "peer_id": peer.id,
                                "peer_public_key_b64": peer.public_key.to_base64(),
                            })),
                            Err(e) => emit(json!({"event": "pair_error", "message": e.to_string()})),
                        }
                    }
                    Err(e) => emit(json!({"event": "pair_error", "message": e.to_string()})),
                }
            });
        }

        Some("accept_pair") => {
            let client = client.clone();
            let code = cmd["code"].as_str().unwrap_or_default().to_string();
            tokio::spawn(async move {
                match client.accept_pair(&code).await {
                    Ok(peer) => emit(json!({
                        "event": "paired",
                        "peer_id": peer.id,
                        "peer_public_key_b64": peer.public_key.to_base64(),
                    })),
                    Err(e) => emit(json!({"event": "pair_error", "message": e.to_string()})),
                }
            });
        }

        Some("send") => {
            let client = client.clone();
            let peer_id = cmd["peer_id"].as_str().unwrap_or_default().to_string();
            let payload_b64 = cmd["payload_b64"].as_str().unwrap_or_default().to_string();
            tokio::spawn(async move {
                let payload = match STANDARD.decode(&payload_b64) {
                    Ok(p) => p,
                    Err(e) => {
                        emit(json!({"event": "send_error", "message": e.to_string()}));
                        return;
                    }
                };
                match client.send(&peer_id, &payload).await {
                    Ok(()) => emit(json!({"event": "sent"})),
                    Err(e) => emit(json!({"event": "send_error", "message": e.to_string()})),
                }
            });
        }

        Some("close") => {
            client.close().await;
        }

        _ => {}
    }
}
