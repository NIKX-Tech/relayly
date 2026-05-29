use std::{
    collections::HashMap,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
    time::Duration,
};

use base64::{engine::general_purpose::STANDARD, Engine};
use futures_util::{SinkExt, StreamExt};
use tokio::{
    sync::{mpsc, oneshot, Mutex, Notify, RwLock},
    time::timeout,
};
use tokio_tungstenite::{
    connect_async,
    tungstenite::Message as WsMessage,
    MaybeTlsStream, WebSocketStream,
};
use url::Url;

use crate::{
    crypto::{PrivateKey, PublicKey},
    wire::WireMessage,
    Error,
};

type WsStream = WebSocketStream<MaybeTlsStream<tokio::net::TcpStream>>;
type WsWrite = futures_util::stream::SplitSink<WsStream, WsMessage>;
type WsRead = futures_util::stream::SplitStream<WsStream>;

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
pub struct Peer {
    pub id: String,
    pub public_key: PublicKey,
}

#[derive(Debug, Clone)]
pub struct Message {
    pub from: String,
    pub payload: Vec<u8>,
    pub timestamp: Option<String>,
}

pub struct Options {
    pub device_id: String,
    pub private_key: PrivateKey,
    pub ping_interval: Duration,
    /// Initial reconnect delay. Set to `None` to disable auto-reconnect.
    pub reconnect_delay: Option<Duration>,
    pub max_reconnect_delay: Duration,
    pub on_reconnect: Option<Box<dyn Fn() + Send + Sync>>,
    pub on_disconnect: Option<Box<dyn Fn(&str) + Send + Sync>>,
}

impl Default for Options {
    fn default() -> Self {
        Self {
            device_id: String::new(),
            private_key: PrivateKey::generate(),
            ping_interval: Duration::from_secs(30),
            reconnect_delay: Some(Duration::from_secs(1)),
            max_reconnect_delay: Duration::from_secs(60),
            on_reconnect: None,
            on_disconnect: None,
        }
    }
}

pub struct PairCode {
    pub short: String,
    pub expires_in: u64,
    waiter: oneshot::Receiver<PairResult>,
}

impl PairCode {
    pub fn qr_code_url(&self, server_url: &str) -> String {
        format!("{}/pair?code={}", server_url.trim_end_matches('/'), self.short)
    }

    pub async fn wait(self) -> Result<Peer, Error> {
        let r = self.waiter.await.map_err(|_| Error::Closed)?;
        if let Some(e) = r.error { return Err(e); }
        Ok(Peer { id: r.peer_id, public_key: r.peer_public_key.ok_or(Error::Closed)? })
    }
}

pub struct Messages(mpsc::Receiver<Message>);

impl Messages {
    pub async fn recv(&mut self) -> Option<Message> {
        self.0.recv().await
    }
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

struct PairResult {
    code: String,
    expires_in: u64,
    peer_id: String,
    peer_public_key: Option<PublicKey>,
    error: Option<Error>,
}

struct Shared {
    opts: Options,
    server_url: String,
    peers: RwLock<HashMap<String, Peer>>,
    pair_waiters: Mutex<HashMap<String, oneshot::Sender<PairResult>>>,
    closed: AtomicBool,
    stop: Notify,
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

#[derive(Clone)]
pub struct Client {
    inner: Arc<Shared>,
    send_tx: mpsc::Sender<WireMessage>,
}

impl Client {
    pub async fn send(&self, peer_id: &str, payload: &[u8]) -> Result<(), Error> {
        let peers = self.inner.peers.read().await;
        let peer = peers
            .get(peer_id)
            .ok_or_else(|| Error::PeerNotFound(peer_id.into()))?
            .clone();
        drop(peers);

        let (ct, nonce) = self.inner.opts.private_key.encrypt(payload, &peer.public_key)?;
        let msg = WireMessage::send_msg(
            peer_id,
            &STANDARD.encode(&ct),
            &STANDARD.encode(nonce),
        );
        self.send_tx.send(msg).await.map_err(|_| Error::Closed)
    }

    pub async fn request_pair_code(&self) -> Result<PairCode, Error> {
        let (tx, rx) = oneshot::channel();
        self.inner.pair_waiters.lock().await.insert("__request__".into(), tx);

        if let Err(_) = self.send_tx.send(WireMessage::pair_request()).await {
            self.inner.pair_waiters.lock().await.remove("__request__");
            return Err(Error::Closed);
        }

        let r = timeout(Duration::from_secs(10), rx)
            .await
            .map_err(|_| Error::Timeout)?
            .map_err(|_| Error::Closed)?;
        if let Some(e) = r.error { return Err(e); }

        let (complete_tx, complete_rx) = oneshot::channel();
        self.inner.pair_waiters.lock().await.insert(r.code.clone(), complete_tx);

        Ok(PairCode { short: r.code, expires_in: r.expires_in, waiter: complete_rx })
    }

    pub async fn accept_pair(&self, code: &str) -> Result<Peer, Error> {
        let (tx, rx) = oneshot::channel();
        self.inner.pair_waiters.lock().await.insert(code.into(), tx);

        if let Err(_) = self.send_tx.send(WireMessage::pair_accept(code)).await {
            self.inner.pair_waiters.lock().await.remove(code);
            return Err(Error::Closed);
        }

        let r = timeout(Duration::from_secs(10), rx)
            .await
            .map_err(|_| Error::Timeout)?
            .map_err(|_| Error::Closed)?;
        if let Some(e) = r.error { return Err(e); }
        Ok(Peer { id: r.peer_id, public_key: r.peer_public_key.ok_or(Error::Closed)? })
    }

    pub async fn close(&self) {
        self.inner.closed.store(true, Ordering::Relaxed);
        self.inner.stop.notify_waiters();
    }
}

// ---------------------------------------------------------------------------
// connect()
// ---------------------------------------------------------------------------

pub async fn connect(server_url: &str, opts: Options) -> Result<(Client, Messages), Error> {
    if opts.device_id.is_empty() {
        return Err(Error::Auth("device_id is required".into()));
    }

    let url = normalise_url(server_url)?;
    let ws = dial(&url, &opts).await?;
    let (ws_write, ws_read) = ws.split();

    let (send_tx, send_rx) = mpsc::channel::<WireMessage>(64);
    let (msg_tx, msg_rx) = mpsc::channel::<Message>(64);

    let shared = Arc::new(Shared {
        server_url: url,
        opts,
        peers: RwLock::new(HashMap::new()),
        pair_waiters: Mutex::new(HashMap::new()),
        closed: AtomicBool::new(false),
        stop: Notify::new(),
    });

    let client = Client { inner: shared.clone(), send_tx: send_tx.clone() };

    tokio::spawn(connection_task(ws_write, ws_read, send_rx, send_tx, msg_tx, shared));

    Ok((client, Messages(msg_rx)))
}

// ---------------------------------------------------------------------------
// Background task
// ---------------------------------------------------------------------------

async fn connection_task(
    mut ws_write: WsWrite,
    mut ws_read: WsRead,
    mut send_rx: mpsc::Receiver<WireMessage>,
    ping_tx: mpsc::Sender<WireMessage>,
    msg_tx: mpsc::Sender<Message>,
    shared: Arc<Shared>,
) {
    // Ping loop - runs for the lifetime of the connection task
    let ping_shared = shared.clone();
    let ping_task = tokio::spawn(async move {
        loop {
            tokio::select! {
                _ = tokio::time::sleep(ping_shared.opts.ping_interval) => {
                    if ping_shared.closed.load(Ordering::Relaxed) { break; }
                    if ping_tx.send(WireMessage::ping()).await.is_err() { break; }
                }
                _ = ping_shared.stop.notified() => break,
            }
        }
    });

    let mut delay = shared.opts.reconnect_delay.unwrap_or(Duration::from_secs(1));

    loop {
        // Run read and write loops concurrently until one finishes
        let disconnect_reason: Option<String> = tokio::select! {
            err = run_read(&mut ws_read, &shared, &msg_tx) => Some(err),
            _ = run_write(&mut ws_write, &mut send_rx) => None,
        };

        // run_write returned None - send_rx closed, client was closed
        let Some(reason) = disconnect_reason else { break };

        if shared.closed.load(Ordering::Relaxed) { break; }

        // Reconnect disabled?
        let initial_delay = match shared.opts.reconnect_delay {
            None => break,
            Some(d) => d,
        };

        if let Some(f) = &shared.opts.on_disconnect {
            f(&reason);
        }

        // Backoff reconnect loop
        loop {
            tokio::select! {
                _ = tokio::time::sleep(delay) => {}
                _ = shared.stop.notified() => {
                    ping_task.abort();
                    return;
                }
            }

            if shared.closed.load(Ordering::Relaxed) {
                ping_task.abort();
                return;
            }

            match dial(&shared.server_url, &shared.opts).await {
                Ok(new_ws) => {
                    let (new_write, new_read) = new_ws.split();
                    ws_write = new_write;
                    ws_read = new_read;
                    delay = initial_delay;
                    if let Some(f) = &shared.opts.on_reconnect { f(); }
                    break;
                }
                Err(_) => {
                    delay = (delay * 2).min(shared.opts.max_reconnect_delay);
                }
            }
        }
    }

    ping_task.abort();
    drop(msg_tx); // closes the Messages channel
}

async fn run_read(
    read: &mut WsRead,
    shared: &Arc<Shared>,
    msg_tx: &mpsc::Sender<Message>,
) -> String {
    loop {
        match read.next().await {
            Some(Ok(WsMessage::Text(text))) => {
                if let Ok(frame) = serde_json::from_str::<WireMessage>(&text) {
                    dispatch(frame, shared, msg_tx).await;
                }
            }
            Some(Ok(_)) => {} // ignore binary/ping/pong frames
            Some(Err(e)) => return e.to_string(),
            None => return "connection closed".into(),
        }
    }
}

async fn run_write(write: &mut WsWrite, recv: &mut mpsc::Receiver<WireMessage>) {
    while let Some(msg) = recv.recv().await {
        if let Ok(json) = serde_json::to_string(&msg) {
            let _ = write.send(WsMessage::Text(json)).await;
        }
    }
}

async fn dispatch(frame: WireMessage, shared: &Arc<Shared>, msg_tx: &mpsc::Sender<Message>) {
    match frame.msg_type.as_str() {
        "message" => handle_message(frame, shared, msg_tx).await,

        "pair_code" => {
            let mut w = shared.pair_waiters.lock().await;
            if let Some(tx) = w.remove("__request__") {
                let _ = tx.send(PairResult {
                    code: frame.code.unwrap_or_default(),
                    expires_in: frame.expires_in.unwrap_or(0),
                    peer_id: String::new(),
                    peer_public_key: None,
                    error: None,
                });
            }
        }

        "pair_complete" => {
            let peer_id = frame.peer_id.clone().unwrap_or_default();
            let pub_key = frame
                .peer_public_key
                .as_deref()
                .and_then(|s| PublicKey::from_base64(s).ok());

            if let Some(pk) = &pub_key {
                shared
                    .peers
                    .write()
                    .await
                    .insert(peer_id.clone(), Peer { id: peer_id.clone(), public_key: pk.clone() });
            }

            let code = frame.code.unwrap_or_default();
            let mut w = shared.pair_waiters.lock().await;
            if let Some(tx) = w.remove(&code) {
                let _ = tx.send(PairResult {
                    code,
                    expires_in: 0,
                    peer_id,
                    peer_public_key: pub_key,
                    error: None,
                });
            }
        }

        "error" => {
            let msg = frame.message.clone().unwrap_or_default();
            let mut w = shared.pair_waiters.lock().await;
            for (_, tx) in w.drain() {
                let _ = tx.send(PairResult {
                    code: String::new(),
                    expires_in: 0,
                    peer_id: String::new(),
                    peer_public_key: None,
                    error: Some(Error::Auth(msg.clone())),
                });
            }
        }

        _ => {}
    }
}

async fn handle_message(frame: WireMessage, shared: &Arc<Shared>, msg_tx: &mpsc::Sender<Message>) {
    let sender_id = frame.from.as_deref().unwrap_or_default();
    let peer = {
        let peers = shared.peers.read().await;
        peers.get(sender_id).cloned()
    };
    let Some(peer) = peer else { return };

    let ct = frame.payload.as_deref().and_then(|s| STANDARD.decode(s).ok());
    let nonce = frame.nonce.as_deref().and_then(|s| STANDARD.decode(s).ok());
    let (Some(ct), Some(nonce)) = (ct, nonce) else { return };

    let Ok(plaintext) = shared.opts.private_key.decrypt(&ct, &nonce, &peer.public_key) else { return };

    let _ = msg_tx
        .try_send(Message { from: sender_id.into(), payload: plaintext, timestamp: frame.timestamp });
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async fn dial(url: &str, opts: &Options) -> Result<WsStream, Error> {
    let (mut ws, _) = connect_async(url)
        .await
        .map_err(|e| Error::Connection(e.to_string()))?;

    let pub_b64 = opts.private_key.public_key().to_base64();
    let auth_json =
        serde_json::to_string(&WireMessage::auth(&opts.device_id, &pub_b64)).unwrap();
    ws.send(WsMessage::Text(auth_json))
        .await
        .map_err(|e| Error::Connection(e.to_string()))?;

    let resp = timeout(Duration::from_secs(10), ws.next())
        .await
        .map_err(|_| Error::Timeout)?
        .ok_or_else(|| Error::Connection("closed during auth".into()))?
        .map_err(|e| Error::Connection(e.to_string()))?;

    let text = match resp {
        WsMessage::Text(t) => t,
        _ => return Err(Error::Auth("expected text frame".into())),
    };

    let frame: WireMessage =
        serde_json::from_str(&text).map_err(|e| Error::Auth(e.to_string()))?;

    match frame.msg_type.as_str() {
        "auth_ok" => Ok(ws),
        "error" => Err(Error::Auth(frame.message.unwrap_or_default())),
        t => Err(Error::Auth(format!("unexpected response: {t}"))),
    }
}

fn normalise_url(url: &str) -> Result<String, Error> {
    let mut parsed = Url::parse(url).map_err(|e| Error::Connection(e.to_string()))?;
    match parsed.scheme() {
        "http" => { parsed.set_scheme("ws").unwrap(); }
        "https" => { parsed.set_scheme("wss").unwrap(); }
        "ws" | "wss" => {}
        s => return Err(Error::Connection(format!("unsupported scheme: {s}"))),
    }
    Ok(parsed.to_string())
}
