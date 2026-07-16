use std::{
    collections::HashMap,
    path::PathBuf,
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
use tokio_tungstenite::{connect_async, tungstenite::Message as WsMessage, MaybeTlsStream, WebSocketStream};
use url::Url;

use crate::{
    crypto::{PrivateKey, PublicKey},
    errors::error_for_code,
    noise::{decode_envelope, encode_envelope, ENVELOPE_HANDSHAKE, ENVELOPE_TRANSPORT},
    peer::{PairResult, PeerConn},
    peerstore::{PeerStore, DEFAULT_PEER_STORE_PATH},
    wire::WireMessage,
    Error,
};

const PROTOCOL_VERSION: u32 = 1;

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

/// Callback invoked with no arguments (e.g. `on_reconnect`).
pub type VoidCallback = Box<dyn Fn() + Send + Sync>;
/// Callback invoked with a single `&str` argument (e.g. `on_disconnect`'s reason,
/// `on_ready`'s peer ID).
pub type StrCallback = Box<dyn Fn(&str) + Send + Sync>;
/// Callback invoked with a peer ID and online/offline flag (`on_peer_status`).
pub type PeerStatusCallback = Box<dyn Fn(&str, bool) + Send + Sync>;

pub struct Options {
    pub device_id: String,
    /// Authenticates this device to the relay (docs/PROTOCOL.md §2, §3). Obtain one
    /// from `POST /api/v1/devices`.
    pub device_token: String,
    pub private_key: PrivateKey,
    /// Where pinned peer static keys (docs/PROTOCOL.md §7.1) are persisted. Defaults
    /// to `DEFAULT_PEER_STORE_PATH` ("~/.relayly/peers.json") if unset.
    pub peer_store_path: Option<PathBuf>,
    pub ping_interval: Duration,
    /// Initial reconnect delay. Set to `None` to disable auto-reconnect.
    pub reconnect_delay: Option<Duration>,
    pub max_reconnect_delay: Duration,
    pub on_reconnect: Option<VoidCallback>,
    pub on_disconnect: Option<StrCallback>,
    /// Called whenever a peer's Noise session becomes usable for `send()` — both
    /// after the very first pairing and after any later re-handshake following a
    /// reconnect (docs/PROTOCOL.md §6).
    pub on_ready: Option<StrCallback>,
    /// Called whenever the server reports the paired peer's online/offline
    /// transition (docs/PROTOCOL.md §5.1's peer_status).
    pub on_peer_status: Option<PeerStatusCallback>,
}

impl Default for Options {
    fn default() -> Self {
        Self {
            device_id: String::new(),
            device_token: String::new(),
            private_key: PrivateKey::generate(),
            peer_store_path: None,
            ping_interval: Duration::from_secs(30),
            reconnect_delay: Some(Duration::from_secs(1)),
            max_reconnect_delay: Duration::from_secs(60),
            on_reconnect: None,
            on_disconnect: None,
            on_ready: None,
            on_peer_status: None,
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

    /// Blocks until the other device accepts the pairing (including the resulting
    /// Noise handshake completing).
    pub async fn wait(self) -> Result<Peer, Error> {
        let r = self.waiter.await.map_err(|_| Error::Closed)?;
        if let Some(e) = r.error {
            return Err(e);
        }
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

/// What a pending request_pair_code/accept_pair call is waiting on. Both sides of a
/// pairing register a waiter under the same code, but only the accepting device
/// initiates the Noise handshake (docs/PROTOCOL.md §5.3) — we_are_acceptor is what
/// lets handle_pair_complete tell them apart. Without this distinction both sides
/// would call start_as_initiator() and send competing msg1s (a real bug hit and fixed
/// in the sdk/py port of this same logic).
struct PairWaiter {
    sender: oneshot::Sender<PairResult>,
    we_are_acceptor: bool,
}

enum OutFrame {
    Control(WireMessage),
    Binary(Vec<u8>),
}

struct Shared {
    opts: Options,
    server_url: String,
    peers: RwLock<HashMap<String, PeerConn>>,
    peer_store: Mutex<PeerStore>,
    pair_waiters: Mutex<HashMap<String, PairWaiter>>,
    closed: AtomicBool,
    stop: Notify,
    /// Cloned into `Client` too; a plain cheaply-cloneable channel handle, not a
    /// reference cycle — lets dispatch handlers (which only ever see `&Arc<Shared>`)
    /// enqueue outgoing handshake replies/msg1s without a separate parameter
    /// threaded through every call site.
    send_tx: mpsc::Sender<OutFrame>,
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

#[derive(Clone)]
pub struct Client {
    inner: Arc<Shared>,
    send_tx: mpsc::Sender<OutFrame>,
}

impl Client {
    /// Encrypts and sends a message to a paired peer device.
    ///
    /// Returns `Error::NotReady` if the peer's Noise session isn't up yet — only
    /// expected in the brief window after a reconnect forces a re-handshake.
    pub async fn send(&self, peer_id: &str, payload: &[u8]) -> Result<(), Error> {
        let ciphertext = {
            let mut peers = self.inner.peers.write().await;
            let peer = peers.get_mut(peer_id).ok_or_else(|| Error::PeerNotFound(peer_id.into()))?;
            peer.send(payload)?
        };
        let envelope = encode_envelope(ENVELOPE_TRANSPORT, &ciphertext);
        self.send_tx.send(OutFrame::Binary(envelope)).await.map_err(|_| Error::Closed)
    }

    /// Asks the server for a 6-digit pairing code to share with another device.
    pub async fn request_pair_code(&self) -> Result<PairCode, Error> {
        let (tx, rx) = oneshot::channel();
        self.inner
            .pair_waiters
            .lock()
            .await
            .insert("__request__".into(), PairWaiter { sender: tx, we_are_acceptor: false });

        if self.send_tx.send(OutFrame::Control(WireMessage::pair_request())).await.is_err() {
            self.inner.pair_waiters.lock().await.remove("__request__");
            return Err(Error::Closed);
        }

        let r = timeout(Duration::from_secs(10), rx)
            .await
            .map_err(|_| Error::Timeout)?
            .map_err(|_| Error::Closed)?;
        if let Some(e) = r.error {
            return Err(e);
        }

        let (complete_tx, complete_rx) = oneshot::channel();
        self.inner
            .pair_waiters
            .lock()
            .await
            .insert(r.code.clone(), PairWaiter { sender: complete_tx, we_are_acceptor: false });

        Ok(PairCode { short: r.code, expires_in: r.expires_in, waiter: complete_rx })
    }

    /// Accepts a pairing code from another device. Blocks until the resulting Noise
    /// handshake completes (docs/PROTOCOL.md §5.3: the accepting device is the
    /// initiator), so the returned Peer can be sent to immediately.
    pub async fn accept_pair(&self, code: &str) -> Result<Peer, Error> {
        let (tx, rx) = oneshot::channel();
        self.inner
            .pair_waiters
            .lock()
            .await
            .insert(code.into(), PairWaiter { sender: tx, we_are_acceptor: true });

        if self.send_tx.send(OutFrame::Control(WireMessage::pair_accept(code))).await.is_err() {
            self.inner.pair_waiters.lock().await.remove(code);
            return Err(Error::Closed);
        }

        let r = timeout(Duration::from_secs(10), rx)
            .await
            .map_err(|_| Error::Timeout)?
            .map_err(|_| Error::Closed)?;
        if let Some(e) = r.error {
            return Err(e);
        }
        Ok(Peer { id: r.peer_id, public_key: r.peer_public_key.ok_or(Error::Closed)? })
    }

    /// Permanently closes the connection. No further reconnects will occur.
    pub async fn close(&self) {
        self.inner.closed.store(true, Ordering::Relaxed);
        self.inner.stop.notify_waiters();
    }
}

// ---------------------------------------------------------------------------
// connect()
// ---------------------------------------------------------------------------

/// Dials a Relayly server and authenticates. Returns a ready-to-use Client.
pub async fn connect(server_url: &str, opts: Options) -> Result<(Client, Messages), Error> {
    if opts.device_id.is_empty() {
        return Err(Error::Auth("device_id is required".into()));
    }
    if opts.device_token.is_empty() {
        return Err(Error::Auth("device_token is required".into()));
    }

    let url = normalise_url(server_url)?;
    let peer_store_path = opts.peer_store_path.clone().unwrap_or_else(|| PathBuf::from(DEFAULT_PEER_STORE_PATH));
    let peer_store = PeerStore::load(&peer_store_path)?;

    let (send_tx, send_rx) = mpsc::channel::<OutFrame>(64);
    let (msg_tx, msg_rx) = mpsc::channel::<Message>(64);

    let shared = Arc::new(Shared {
        server_url: url,
        opts,
        peers: RwLock::new(HashMap::new()),
        peer_store: Mutex::new(peer_store),
        pair_waiters: Mutex::new(HashMap::new()),
        closed: AtomicBool::new(false),
        stop: Notify::new(),
        send_tx: send_tx.clone(),
    });

    let ws = dial(&shared).await?;
    let (ws_write, ws_read) = ws.split();

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
    mut send_rx: mpsc::Receiver<OutFrame>,
    ping_tx: mpsc::Sender<OutFrame>,
    msg_tx: mpsc::Sender<Message>,
    shared: Arc<Shared>,
) {
    let ping_shared = shared.clone();
    let ping_task = tokio::spawn(async move {
        loop {
            tokio::select! {
                _ = tokio::time::sleep(ping_shared.opts.ping_interval) => {
                    if ping_shared.closed.load(Ordering::Relaxed) { break; }
                    if ping_tx.send(OutFrame::Control(WireMessage::ping())).await.is_err() { break; }
                }
                _ = ping_shared.stop.notified() => break,
            }
        }
    });

    let mut delay = shared.opts.reconnect_delay.unwrap_or(Duration::from_secs(1));

    loop {
        let disconnect_reason: Option<String> = tokio::select! {
            err = run_read(&mut ws_read, &shared, &msg_tx) => Some(err),
            _ = run_write(&mut ws_write, &mut send_rx) => None,
        };

        let Some(reason) = disconnect_reason else { break };
        if shared.closed.load(Ordering::Relaxed) {
            break;
        }

        let initial_delay = match shared.opts.reconnect_delay {
            None => break,
            Some(d) => d,
        };

        if let Some(f) = &shared.opts.on_disconnect {
            f(&reason);
        }

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

            match dial(&shared).await {
                Ok(new_ws) => {
                    let (new_write, new_read) = new_ws.split();
                    ws_write = new_write;
                    ws_read = new_read;
                    delay = initial_delay;
                    if let Some(f) = &shared.opts.on_reconnect {
                        f();
                    }
                    break;
                }
                Err(_) => {
                    delay = (delay * 2).min(shared.opts.max_reconnect_delay);
                }
            }
        }
    }

    ping_task.abort();
    drop(msg_tx);
}

async fn run_read(read: &mut WsRead, shared: &Arc<Shared>, msg_tx: &mpsc::Sender<Message>) -> String {
    loop {
        match read.next().await {
            Some(Ok(WsMessage::Text(text))) => {
                if let Ok(frame) = serde_json::from_str::<WireMessage>(&text) {
                    dispatch(frame, shared).await;
                }
            }
            Some(Ok(WsMessage::Binary(data))) => {
                let Some((kind, payload)) = decode_envelope(&data) else { continue };
                match kind {
                    ENVELOPE_HANDSHAKE => handle_handshake_envelope(payload, shared).await,
                    ENVELOPE_TRANSPORT => handle_transport_envelope(payload, shared, msg_tx).await,
                    _ => {}
                }
            }
            Some(Ok(_)) => {} // ping/pong/close frames handled by tungstenite internally
            Some(Err(e)) => return e.to_string(),
            None => return "connection closed".into(),
        }
    }
}

async fn run_write(write: &mut WsWrite, recv: &mut mpsc::Receiver<OutFrame>) {
    while let Some(frame) = recv.recv().await {
        let ws_msg = match frame {
            OutFrame::Control(msg) => match serde_json::to_string(&msg) {
                Ok(json) => WsMessage::Text(json),
                Err(_) => continue,
            },
            OutFrame::Binary(data) => WsMessage::Binary(data),
        };
        let _ = write.send(ws_msg).await;
    }
}

// ---------------------------------------------------------------------------
// Control dispatch
// ---------------------------------------------------------------------------

async fn dispatch(frame: WireMessage, shared: &Arc<Shared>) {
    match frame.msg_type.as_str() {
        "pair_code" => handle_pair_code(frame, shared).await,
        "pair_complete" => handle_pair_complete(frame, shared).await,
        "peer_status" => handle_peer_status(frame, shared).await,
        "error" => handle_error(frame, shared).await,
        "pong" => {} // keepalive, nothing to do
        _ => {}      // unknown type: ignored per §5's forward-compatibility rule
    }
}

async fn handle_pair_code(frame: WireMessage, shared: &Arc<Shared>) {
    let mut waiters = shared.pair_waiters.lock().await;
    if let Some(w) = waiters.remove("__request__") {
        let _ = w.sender.send(PairResult {
            code: frame.code.unwrap_or_default(),
            expires_in: frame.expires_in.unwrap_or(0),
            ..Default::default()
        });
    }
}

/// Implements §5.3: link the peer, and if we're the accepting device, start the
/// Noise handshake as initiator immediately.
async fn handle_pair_complete(frame: WireMessage, shared: &Arc<Shared>) {
    let Some(peer_id) = frame.peer_id else { return };
    let code = frame.code.unwrap_or_default();

    let waiter = if code.is_empty() { None } else { shared.pair_waiters.lock().await.remove(&code) };

    let mut peers = shared.peers.write().await;
    let peer = peers
        .entry(peer_id.clone())
        .or_insert_with(|| PeerConn::new(frame.peer_static_key.clone().unwrap_or_default()));
    if let Some(k) = &frame.peer_static_key {
        peer.announced_static_key = k.clone();
    }

    let Some(waiter) = waiter else { return }; // neither request_pair_code nor accept_pair is waiting on this code

    peer.set_first_pair_waiter(waiter.sender);
    if !waiter.we_are_acceptor {
        return; // we requested; wait for the accepting device's incoming msg1
    }

    match peer.start_as_initiator(&shared.opts.private_key) {
        Ok(msg1) => {
            drop(peers);
            enqueue_binary(shared, ENVELOPE_HANDSHAKE, msg1).await;
        }
        Err(e) => {
            if let Some(w) = peer.take_first_pair_waiter() {
                let _ = w.send(PairResult { error: Some(e), ..Default::default() });
            }
        }
    }
}

/// Implements §6's reconnect rule: whichever side's device_id is lexicographically
/// smaller re-initiates a fresh handshake whenever the peer comes online.
async fn handle_peer_status(frame: WireMessage, shared: &Arc<Shared>) {
    let online = frame.online.unwrap_or(false);
    let Some(peer_id) = frame.peer_id else { return };

    if let Some(f) = &shared.opts.on_peer_status {
        f(&peer_id, online);
    }
    if !online {
        return;
    }
    if shared.opts.device_id >= peer_id {
        return; // larger ID: wait for the incoming msg1
    }

    let msg1 = {
        let mut peers = shared.peers.write().await;
        let Some(peer) = peers.get_mut(&peer_id) else { return };
        match peer.start_rekey_as_initiator(&shared.opts.private_key) {
            Ok(msg1) => msg1,
            Err(_) => return, // best-effort; a future peer_status or AEAD failure can retry
        }
    };
    enqueue_binary(shared, ENVELOPE_HANDSHAKE, msg1).await;
}

async fn handle_error(frame: WireMessage, shared: &Arc<Shared>) {
    let err = error_for_code(frame.code.as_deref().unwrap_or(""), frame.message.as_deref().unwrap_or(""));
    let mut waiters = shared.pair_waiters.lock().await;
    for (_, w) in waiters.drain() {
        let _ = w.sender.send(PairResult { error: Some(err_clone(&err)), ..Default::default() });
    }
}

/// `Error` doesn't implement `Clone` (thiserror's `#[from] std::io::Error` blocks a
/// blanket derive), but `handle_error` may need to notify several waiters with
/// logically the same failure — render it back down to a fresh Internal-shaped error.
fn err_clone(err: &Error) -> Error {
    error_for_code("", &err.to_string())
}

// ---------------------------------------------------------------------------
// Binary envelope dispatch
// ---------------------------------------------------------------------------

/// v1 supports exactly one linked peer per device (docs/PROTOCOL.md §5.3). Incoming
/// binary envelopes carry no sender field — the relay only ever forwards them from
/// the paired device.
async fn handle_handshake_envelope(payload: &[u8], shared: &Arc<Shared>) {
    let mut outcome = {
        let mut peers = shared.peers.write().await;
        let Some((_, peer)) = peers.iter_mut().next() else { return };
        peer.handle_handshake_envelope(&shared.opts.private_key, payload)
    };

    if let Some(reply) = outcome.reply.take() {
        enqueue_binary(shared, ENVELOPE_HANDSHAKE, reply).await;
    }
    if outcome.done {
        resolve_handshake(shared, outcome).await;
    }
}

/// Runs once, exactly when a Noise handshake attempt finishes: applies §7's
/// client-side pin (the real boundary) and server-announced-key cross-check,
/// promotes or abandons a rekey attempt per §6's make-before-break rule, notifies a
/// first-pairing waiter if one is registered, and calls on_ready.
async fn resolve_handshake(shared: &Arc<Shared>, outcome: crate::peer::HandshakeOutcome) {
    let mut peers = shared.peers.write().await;
    let Some((peer_id, peer)) = peers.iter_mut().next() else { return };
    let peer_id = peer_id.clone();

    let mut error: Option<Error> = None;
    let mut public_key: Option<PublicKey> = None;

    if outcome.failed {
        error = Some(Error::Internal(format!("handshake with {peer_id} failed")));
    } else {
        let authenticated = outcome.peer_static_key.expect("done && !failed implies a peer static key");
        let authenticated_b64 = STANDARD.encode(&authenticated);
        let pin_result = shared.peer_store.lock().await.pin_or_verify(&peer_id, &authenticated_b64);
        match pin_result {
            Err(e) => error = Some(e),
            Ok(()) => {
                if !peer.announced_static_key.is_empty() && peer.announced_static_key != authenticated_b64 {
                    error = Some(error_for_code(
                        "key_mismatch",
                        &format!("server-announced key for peer {peer_id} does not match the authenticated handshake"),
                    ));
                } else {
                    public_key = PublicKey::from_bytes(&authenticated).ok();
                }
            }
        }
    }

    if outcome.was_pending {
        if error.is_some() {
            peer.abandon_pending();
            return;
        }
        peer.promote_pending();
    }

    let waiter = peer.take_first_pair_waiter();
    if let Some(e) = error {
        if let Some(w) = waiter {
            let _ = w.send(PairResult { error: Some(e), ..Default::default() });
        }
        return;
    }

    if let Some(w) = waiter {
        let _ = w.send(PairResult { peer_id: peer_id.clone(), peer_public_key: public_key, ..Default::default() });
    }
    if let Some(f) = &shared.opts.on_ready {
        f(&peer_id);
    }
}

async fn handle_transport_envelope(ciphertext: &[u8], shared: &Arc<Shared>, msg_tx: &mpsc::Sender<Message>) {
    let plaintext = {
        let mut peers = shared.peers.write().await;
        let Some((peer_id, peer)) = peers.iter_mut().next() else { return };
        match peer.recv(ciphertext) {
            Ok(pt) => (peer_id.clone(), pt),
            Err(_) => return, // not ready, or decryption failed — drop silently
        }
    };
    let (from, payload) = plaintext;
    let _ = msg_tx.try_send(Message { from, payload, timestamp: Some(crate::peerstore::now_rfc3339()) });
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async fn dial(shared: &Arc<Shared>) -> Result<WsStream, Error> {
    let mut u = Url::parse(&shared.server_url).map_err(|e| Error::Connection(e.to_string()))?;
    u.query_pairs_mut()
        .append_pair("device_id", &shared.opts.device_id)
        .append_pair("token", &shared.opts.device_token);

    let (mut ws, _) = connect_async(u.as_str()).await.map_err(|e| Error::Connection(e.to_string()))?;

    let resp = timeout(Duration::from_secs(10), ws.next())
        .await
        .map_err(|_| Error::Timeout)?
        .ok_or_else(|| Error::Connection("closed during welcome".into()))?
        .map_err(|e| Error::Connection(e.to_string()))?;

    let text = match resp {
        WsMessage::Text(t) => t,
        _ => return Err(Error::Auth("expected a text welcome frame".into())),
    };
    let frame: WireMessage = serde_json::from_str(&text).map_err(|e| Error::Auth(e.to_string()))?;

    match frame.msg_type.as_str() {
        "welcome" => {}
        "error" => {
            return Err(error_for_code(frame.code.as_deref().unwrap_or(""), frame.message.as_deref().unwrap_or("")))
        }
        t => return Err(Error::Auth(format!("unexpected first frame type: {t}"))),
    }
    if frame.protocol_version != Some(PROTOCOL_VERSION) {
        return Err(Error::Auth(format!(
            "unsupported protocol_version {:?} (this SDK implements {PROTOCOL_VERSION})",
            frame.protocol_version
        )));
    }

    // A reconnect must not discard an already-healthy PeerConn just because the
    // control channel briefly reset — only register peers we don't already track.
    {
        let mut peers = shared.peers.write().await;
        for p in frame.peers.unwrap_or_default() {
            peers.entry(p.id.clone()).or_insert_with(|| PeerConn::new(p.static_key));
        }
    }

    let pub_b64 = shared.opts.private_key.public_key().to_base64();
    let announce = serde_json::to_string(&WireMessage::announce_key(&pub_b64)).unwrap();
    ws.send(WsMessage::Text(announce)).await.map_err(|e| Error::Connection(e.to_string()))?;

    Ok(ws)
}

async fn enqueue_binary(shared: &Arc<Shared>, kind: u8, payload: Vec<u8>) {
    let _ = shared.send_tx.send(OutFrame::Binary(encode_envelope(kind, &payload))).await;
}

fn normalise_url(url: &str) -> Result<String, Error> {
    let mut parsed = Url::parse(url).map_err(|e| Error::Connection(e.to_string()))?;
    match parsed.scheme() {
        "http" => {
            parsed.set_scheme("ws").unwrap();
        }
        "https" => {
            parsed.set_scheme("wss").unwrap();
        }
        "ws" | "wss" => {}
        s => return Err(Error::Connection(format!("unsupported scheme: {s}"))),
    }
    Ok(parsed.to_string())
}
