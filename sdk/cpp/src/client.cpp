#include "relayly/client.hpp"

#include <ixwebsocket/IXWebSocket.h>

#include <atomic>
#include <cctype>
#include <chrono>
#include <cstring>
#include <mutex>
#include <sstream>
#include <unordered_map>

#include "noise/session.hpp"
#include "peer_conn.hpp"
#include "relayly/errors.hpp"
#include "relayly/peer_store.hpp"
#include "wire.hpp"

namespace relayly {

namespace {

// The protocol_version welcome must report (docs/PROTOCOL.md §5.1). A client whose
// implemented version differs disconnects with an error to its caller.
constexpr std::uint32_t kProtocolVersion = 1;
constexpr int kHandshakeTimeoutSecs = 10;
constexpr int kConnectTimeoutSecs = 15;

std::string BytesToString(const std::vector<std::uint8_t>& bytes) {
  return std::string(reinterpret_cast<const char*>(bytes.data()), bytes.size());
}

std::vector<std::uint8_t> StringToBytes(const std::string& s) {
  return std::vector<std::uint8_t>(s.begin(), s.end());
}

std::string PercentEncode(const std::string& raw) {
  std::ostringstream out;
  for (unsigned char c : raw) {
    if (std::isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~') {
      out << c;
    } else {
      out << '%' << std::uppercase << std::hex << static_cast<int>(c) << std::nouppercase << std::dec;
    }
  }
  return out.str();
}

// Normalizes http(s) -> ws(s) and appends the HTTP-layer auth query params
// (docs/PROTOCOL.md §3: no in-band auth frame, auth is device_id/token on the URL).
std::string BuildConnectUrl(const std::string& server_url, const std::string& device_id, const std::string& token) {
  std::string url = server_url;
  if (url.rfind("http://", 0) == 0) {
    url = "ws://" + url.substr(7);
  } else if (url.rfind("https://", 0) == 0) {
    url = "wss://" + url.substr(8);
  }

  char separator = (url.find('?') == std::string::npos) ? '?' : '&';
  url += separator;
  url += "device_id=" + PercentEncode(device_id) + "&token=" + PercentEncode(token);
  return url;
}

noise::KeyPair ToNoiseKeyPair(const PrivateKey& key) {
  noise::KeyPair kp;
  kp.private_key = key.bytes();
  kp.public_key = key.GetPublicKey().bytes();
  return kp;
}

/// What a RequestPairCode call is waiting on before it can construct a PairCode.
struct PairCodeEvent {
  std::string code;
  int expires_in = 0;
};

/// What a pending pairing (by code) is waiting on: the eventual Peer once the
/// Noise handshake following pair_complete finishes. Shared (by promise identity,
/// not by value) between pair_waiters and first_pair_waiters — see
/// HandlePairComplete, which mirrors sdk/go's peer.setFirstPairWaiter channel
/// transfer.
struct PairWaiter {
  bool we_are_acceptor = false;
  std::shared_ptr<std::promise<Peer>> promise;
};

}  // namespace

struct Client::Impl {
  explicit Impl(Options options) : opts(std::move(options)) {}

  // Explicit code/reason, not ws.stop()'s bare default: the defaults are
  // ix::WebSocketCloseConstants::kNormalClosureCode/kNormalClosureMessage,
  // static const class data members with no dllexport annotation anywhere
  // in ixwebsocket's own source. A consumer relying on the default embeds
  // a reference to that external data symbol at the call site, which a
  // Windows DLL build of ixwebsocket never exports (data symbols need
  // explicit annotation on MSVC, unlike functions) - "unresolved external
  // symbol ix::WebSocketCloseConstants::kNormalClosureCode" linking
  // relayly.dll, found only by an actual Windows CI build with ixwebsocket
  // forced shared (karshipta gateway's own relay-transport.md explains why
  // it must be shared there). 1000/"Normal closure" are RFC 6455's own
  // normal-closure code/reason, the exact values these constants hold.
  ~Impl() { ws.stop(1000, "Normal closure"); }

  Options opts;
  noise::KeyPair static_keypair{};
  PeerStore peer_store{PeerStore::Load()};
  ix::WebSocket ws;

  std::atomic<bool> closed{false};
  std::atomic<bool> connect_settled{false};
  std::promise<void> connect_promise;
  bool awaiting_welcome = true;

  std::mutex peers_mu;
  std::unordered_map<std::string, PeerConn> peers;

  std::mutex code_waiter_mu;
  std::shared_ptr<std::promise<PairCodeEvent>> code_waiter;  // at most one RequestPairCode in flight

  std::mutex pair_waiters_mu;
  std::unordered_map<std::string, PairWaiter> pair_waiters;  // keyed by pairing code

  std::mutex first_pair_waiters_mu;
  std::unordered_map<std::string, std::shared_ptr<std::promise<Peer>>> first_pair_waiters;  // keyed by peer_id

  void OnMessage(const ix::WebSocketMessagePtr& msg);
  void OnOpen();
  void OnText(const std::string& text);
  void OnBinary(const std::string& data);
  void OnClose(const std::string& reason);
  void OnError(const std::string& reason);

  void HandleWelcome(const WireMessage& frame);
  void FailConnect(const Error& err);
  void ApplyWelcomePeers(const std::vector<WirePeer>& peers_list);

  void Dispatch(const WireMessage& frame);
  void HandlePairCode(const WireMessage& frame);
  void HandlePairComplete(const WireMessage& frame);
  void HandlePeerStatus(const WireMessage& frame);
  void HandleError(const WireMessage& frame);

  void HandleHandshakeEnvelope(const std::vector<std::uint8_t>& payload);
  void HandleTransportEnvelope(const std::vector<std::uint8_t>& ciphertext);
  void ResolveHandshake(const std::string& peer_id, const HandshakeOutcome& outcome);

  void EnqueueBinary(std::uint8_t kind, const std::vector<std::uint8_t>& payload);
};

void Client::Impl::OnMessage(const ix::WebSocketMessagePtr& msg) {
  switch (msg->type) {
    case ix::WebSocketMessageType::Open:
      OnOpen();
      break;
    case ix::WebSocketMessageType::Message:
      if (msg->binary) {
        OnBinary(msg->str);
      } else {
        OnText(msg->str);
      }
      break;
    case ix::WebSocketMessageType::Close:
      OnClose(msg->closeInfo.reason);
      break;
    case ix::WebSocketMessageType::Error:
      OnError(msg->errorInfo.reason);
      break;
    default:
      break;  // Ping/Pong/Fragment: IXWebSocket already handles WS-level ping/pong.
  }
}

void Client::Impl::OnOpen() { awaiting_welcome = true; }

void Client::Impl::FailConnect(const Error& err) {
  bool was_first_connect = !connect_settled.exchange(true);
  if (was_first_connect) {
    connect_promise.set_exception(std::make_exception_ptr(err));
  } else if (opts.on_disconnect) {
    opts.on_disconnect(err.what());
  }
}

void Client::Impl::ApplyWelcomePeers(const std::vector<WirePeer>& peers_list) {
  std::lock_guard<std::mutex> lock(peers_mu);
  for (const auto& p : peers_list) {
    if (peers.find(p.id) == peers.end()) {
      peers.emplace(p.id, PeerConn(p.static_key));
    }
  }
}

void Client::Impl::HandleWelcome(const WireMessage& frame) {
  awaiting_welcome = false;

  if (frame.type == "error") {
    FailConnect(ErrorForCode(frame.code.value_or(""), frame.message.value_or("")));
    return;
  }
  if (frame.type != "welcome") {
    FailConnect(Error(ErrorCode::kConnection, "relayly: unexpected first frame type: " + frame.type));
    return;
  }
  if (frame.protocol_version.value_or(0) != kProtocolVersion) {
    FailConnect(Error(ErrorCode::kConnection, "relayly: unsupported protocol_version (this SDK implements " +
                                                   std::to_string(kProtocolVersion) + ")"));
    return;
  }

  ApplyWelcomePeers(frame.peers.value_or(std::vector<WirePeer>{}));

  auto announce = WireMessage::AnnounceKey(opts.private_key.GetPublicKey().ToBase64());
  ws.sendText(announce.Encode());

  bool was_first_connect = !connect_settled.exchange(true);
  if (was_first_connect) {
    connect_promise.set_value();
  } else if (opts.on_reconnect) {
    opts.on_reconnect();
  }
}

void Client::Impl::OnText(const std::string& text) {
  auto frame = WireMessage::Decode(text);
  if (!frame) return;  // skip malformed frames

  if (awaiting_welcome) {
    HandleWelcome(*frame);
    return;
  }
  Dispatch(*frame);
}

void Client::Impl::OnBinary(const std::string& data) {
  auto decoded = noise::DecodeEnvelope(StringToBytes(data));
  if (!decoded) return;
  auto [kind, payload] = *decoded;
  if (kind == noise::kEnvelopeHandshake) {
    HandleHandshakeEnvelope(payload);
  } else if (kind == noise::kEnvelopeTransport) {
    HandleTransportEnvelope(payload);
  }
}

void Client::Impl::OnClose(const std::string& reason) {
  if (closed) return;  // our own intentional close
  if (opts.on_disconnect) opts.on_disconnect(reason);
}

void Client::Impl::OnError(const std::string& reason) {
  FailConnect(Error(ErrorCode::kConnection, "relayly: connection error: " + reason));
}

void Client::Impl::Dispatch(const WireMessage& frame) {
  if (frame.type == "pair_code") {
    HandlePairCode(frame);
  } else if (frame.type == "pair_complete") {
    HandlePairComplete(frame);
  } else if (frame.type == "peer_status") {
    HandlePeerStatus(frame);
  } else if (frame.type == "error") {
    HandleError(frame);
  }
  // "pong": keepalive, nothing to do. Anything else: unknown type, ignored per
  // §5's forward-compatibility rule.
}

void Client::Impl::HandlePairCode(const WireMessage& frame) {
  std::shared_ptr<std::promise<PairCodeEvent>> waiter;
  {
    std::lock_guard<std::mutex> lock(code_waiter_mu);
    waiter = code_waiter;
    code_waiter.reset();
  }
  if (!waiter) return;
  waiter->set_value(PairCodeEvent{frame.code.value_or(""), static_cast<int>(frame.expires_in.value_or(0))});
}

// Implements §5.3: link the peer, and if we're the accepting device, start the
// Noise handshake as initiator immediately.
void Client::Impl::HandlePairComplete(const WireMessage& frame) {
  std::string code = frame.code.value_or("");
  PairWaiter waiter;
  {
    std::lock_guard<std::mutex> lock(pair_waiters_mu);
    auto it = pair_waiters.find(code);
    if (it == pair_waiters.end()) return;
    waiter = it->second;
    pair_waiters.erase(it);
  }

  std::string peer_id = frame.peer_id.value_or("");
  {
    std::lock_guard<std::mutex> lock(peers_mu);
    peers.emplace(peer_id, PeerConn(frame.peer_static_key.value_or("")));
  }
  {
    std::lock_guard<std::mutex> lock(first_pair_waiters_mu);
    first_pair_waiters[peer_id] = waiter.promise;
  }

  if (!waiter.we_are_acceptor) {
    return;  // we requested; wait for the accepting device's incoming msg1
  }

  std::vector<std::uint8_t> msg1;
  {
    std::lock_guard<std::mutex> lock(peers_mu);
    auto it = peers.find(peer_id);
    if (it == peers.end()) return;
    msg1 = it->second.StartAsInitiator(static_keypair);
  }
  EnqueueBinary(noise::kEnvelopeHandshake, msg1);
}

// Implements §6's reconnect rule: whichever side's device_id is lexicographically
// smaller re-initiates a fresh handshake whenever the peer comes online (including
// this device's own reconnect, since welcome reports the peer's state to us too).
void Client::Impl::HandlePeerStatus(const WireMessage& frame) {
  std::string peer_id = frame.peer_id.value_or("");
  bool online = frame.online.value_or(false);
  if (opts.on_peer_status) opts.on_peer_status(peer_id, online);
  if (!online) return;

  std::vector<std::uint8_t> msg1;
  {
    std::lock_guard<std::mutex> lock(peers_mu);
    auto it = peers.find(peer_id);
    if (it == peers.end()) return;
    if (opts.device_id >= peer_id) return;  // larger ID: wait for the incoming msg1
    msg1 = it->second.StartRekeyAsInitiator(static_keypair);
  }
  EnqueueBinary(noise::kEnvelopeHandshake, msg1);
}

void Client::Impl::HandleError(const WireMessage& frame) {
  auto err = std::make_exception_ptr(ErrorForCode(frame.code.value_or(""), frame.message.value_or("")));

  std::shared_ptr<std::promise<PairCodeEvent>> code_w;
  {
    std::lock_guard<std::mutex> lock(code_waiter_mu);
    code_w = code_waiter;
    code_waiter.reset();
  }
  if (code_w) code_w->set_exception(err);

  std::lock_guard<std::mutex> lock(pair_waiters_mu);
  for (auto& [key, waiter] : pair_waiters) {
    waiter.promise->set_exception(err);
  }
  pair_waiters.clear();
}

void Client::Impl::HandleHandshakeEnvelope(const std::vector<std::uint8_t>& payload) {
  std::string peer_id;
  HandshakeOutcome outcome;
  {
    std::lock_guard<std::mutex> lock(peers_mu);
    if (peers.empty()) return;  // v1: at most one linked peer; envelopes carry no sender field
    auto it = peers.begin();
    peer_id = it->first;
    outcome = it->second.HandleHandshakeEnvelope(static_keypair, payload);
  }
  if (outcome.reply) {
    EnqueueBinary(noise::kEnvelopeHandshake, *outcome.reply);
  }
  if (!outcome.done) return;
  ResolveHandshake(peer_id, outcome);
}

// Implements docs/PROTOCOL.md §7 in full: the client-side pin (§7.1, the real
// security boundary), the server-announced-key cross-check (§7.2, defense in
// depth), the make-before-break promote/abandon decision (§6), and resolving
// whichever pairing waiter is attached to this peer's first handshake.
void Client::Impl::ResolveHandshake(const std::string& peer_id, const HandshakeOutcome& outcome) {
  std::string announced_key;
  {
    std::lock_guard<std::mutex> lock(peers_mu);
    auto it = peers.find(peer_id);
    if (it == peers.end()) return;
    announced_key = it->second.announced_static_key;
  }

  std::exception_ptr err;
  std::optional<PublicKey> result_pub;

  if (outcome.failed) {
    err = std::make_exception_ptr(Error(ErrorCode::kInternal, "relayly: handshake failed"));
  } else {
    try {
      PublicKey pub(*outcome.peer_static_key);
      auto authenticated_b64 = pub.ToBase64();
      try {
        peer_store.PinOrVerify(peer_id, authenticated_b64);
      } catch (const PeerKeyMismatchError& e) {
        throw Error(ErrorCode::kPeerKeyMismatch, e.what());
      }
      if (!announced_key.empty() && announced_key != authenticated_b64) {
        throw Error(ErrorCode::kKeyMismatch, "relayly: server-announced key for peer " + peer_id +
                                                  " does not match the authenticated handshake");
      }
      result_pub = pub;
    } catch (...) {
      err = std::current_exception();
    }
  }

  if (outcome.was_pending) {
    std::lock_guard<std::mutex> lock(peers_mu);
    auto it = peers.find(peer_id);
    if (it != peers.end()) {
      if (err) {
        it->second.AbandonPending();
      } else {
        it->second.PromotePending();
      }
    }
  }

  std::shared_ptr<std::promise<Peer>> waiter;
  {
    std::lock_guard<std::mutex> lock(first_pair_waiters_mu);
    auto it = first_pair_waiters.find(peer_id);
    if (it != first_pair_waiters.end()) {
      waiter = it->second;
      first_pair_waiters.erase(it);
    }
  }
  if (waiter) {
    if (err) {
      waiter->set_exception(err);
    } else {
      waiter->set_value(Peer{peer_id, *result_pub});
    }
  }
  if (!err && opts.on_ready) opts.on_ready(peer_id);
}

void Client::Impl::HandleTransportEnvelope(const std::vector<std::uint8_t>& ciphertext) {
  std::string peer_id;
  std::vector<std::uint8_t> plaintext;
  {
    std::lock_guard<std::mutex> lock(peers_mu);
    if (peers.empty()) return;
    auto it = peers.begin();
    peer_id = it->first;
    try {
      plaintext = it->second.Recv(ciphertext);
    } catch (...) {
      return;  // not ready, or decryption failed — drop silently
    }
  }
  if (opts.on_message) {
    opts.on_message(Message{peer_id, std::move(plaintext), std::chrono::system_clock::now()});
  }
}

void Client::Impl::EnqueueBinary(std::uint8_t kind, const std::vector<std::uint8_t>& payload) {
  // IXWebSocket serializes concurrent send*() calls internally (its own write
  // mutex), so no app-level send queue is needed here, unlike sdk/go's explicit
  // sends channel + writeLoop.
  ws.sendBinary(BytesToString(noise::EncodeEnvelope(kind, payload)));
}

std::unique_ptr<Client> Client::Connect(const std::string& server_url, Options options) {
  if (options.device_id.empty()) {
    throw Error(ErrorCode::kAuth, "relayly: device_id is required");
  }
  if (options.device_token.empty()) {
    throw Error(ErrorCode::kAuth, "relayly: device_token is required");
  }

  auto impl = std::make_unique<Client::Impl>(std::move(options));
  impl->static_keypair = ToNoiseKeyPair(impl->opts.private_key);
  impl->peer_store = PeerStore::Load(impl->opts.peer_store_path.empty() ? kDefaultPeerStorePath
                                                                         : impl->opts.peer_store_path);

  impl->ws.setUrl(BuildConnectUrl(server_url, impl->opts.device_id, impl->opts.device_token));
  impl->ws.setHandshakeTimeout(kHandshakeTimeoutSecs);
  impl->ws.setPingInterval(static_cast<int>(impl->opts.ping_interval.count()));

  if (impl->opts.reconnect_delay.count() < 0) {
    impl->ws.disableAutomaticReconnection();
  } else {
    impl->ws.enableAutomaticReconnection();
    auto min_delay = impl->opts.reconnect_delay.count() > 0 ? impl->opts.reconnect_delay : kDefaultReconnectDelay;
    auto max_delay =
        impl->opts.max_reconnect_delay.count() > 0 ? impl->opts.max_reconnect_delay : kDefaultMaxReconnectDelay;
    impl->ws.setMinWaitBetweenReconnectionRetries(
        static_cast<uint32_t>(std::chrono::duration_cast<std::chrono::milliseconds>(min_delay).count()));
    impl->ws.setMaxWaitBetweenReconnectionRetries(
        static_cast<uint32_t>(std::chrono::duration_cast<std::chrono::milliseconds>(max_delay).count()));
  }

  auto fut = impl->connect_promise.get_future();
  Client::Impl* impl_ptr = impl.get();
  impl->ws.setOnMessageCallback([impl_ptr](const ix::WebSocketMessagePtr& msg) { impl_ptr->OnMessage(msg); });
  impl->ws.start();

  if (fut.wait_for(std::chrono::seconds(kConnectTimeoutSecs)) != std::future_status::ready) {
    // Explicit code/reason: see Impl::~Impl()'s comment above.
    impl->ws.stop(1000, "Normal closure");
    throw Error(ErrorCode::kTimeout, "relayly: timed out connecting to " + server_url);
  }
  fut.get();  // rethrows if FailConnect ran instead of a successful welcome

  auto client = std::unique_ptr<Client>(new Client());
  client->impl_ = std::move(impl);
  return client;
}

Client::~Client() {
  // Explicit code/reason: see Impl::~Impl()'s comment above.
  if (impl_) impl_->ws.stop(1000, "Normal closure");
}

void Client::Send(const std::string& peer_id, std::span<const std::byte> payload) {
  std::vector<std::uint8_t> bytes(payload.size());
  std::memcpy(bytes.data(), payload.data(), payload.size());

  std::vector<std::uint8_t> ciphertext;
  {
    std::lock_guard<std::mutex> lock(impl_->peers_mu);
    auto it = impl_->peers.find(peer_id);
    if (it == impl_->peers.end()) {
      throw Error(ErrorCode::kPeerNotFound, "relayly: no paired peer with ID \"" + peer_id + "\"");
    }
    try {
      ciphertext = it->second.Send(bytes);
    } catch (const noise::NotReadyError&) {
      throw Error(ErrorCode::kNotReady, "relayly: peer session is not ready");
    }
  }
  impl_->EnqueueBinary(noise::kEnvelopeTransport, ciphertext);
}

PairCode Client::RequestPairCode() {
  auto code_promise = std::make_shared<std::promise<PairCodeEvent>>();
  auto fut = code_promise->get_future();
  {
    std::lock_guard<std::mutex> lock(impl_->code_waiter_mu);
    impl_->code_waiter = code_promise;
  }

  impl_->ws.sendText(WireMessage::PairRequest().Encode());
  PairCodeEvent event = fut.get();  // throws relayly::Error on a server error frame

  PairCode pc;
  pc.short_code_ = event.code;
  pc.expires_in_ = event.expires_in;
  pc.promise_ = std::make_shared<std::promise<Peer>>();
  {
    std::lock_guard<std::mutex> lock(impl_->pair_waiters_mu);
    impl_->pair_waiters[event.code] = PairWaiter{false, pc.promise_};
  }
  return pc;
}

std::future<Peer> Client::AcceptPair(const std::string& code) {
  auto promise = std::make_shared<std::promise<Peer>>();
  {
    std::lock_guard<std::mutex> lock(impl_->pair_waiters_mu);
    impl_->pair_waiters[code] = PairWaiter{true, promise};
  }
  impl_->ws.sendText(WireMessage::PairAccept(code).Encode());
  return promise->get_future();
}

void Client::Close() {
  if (impl_->closed.exchange(true)) return;
  // Explicit code/reason: see Impl::~Impl()'s comment above.
  impl_->ws.stop(1000, "Normal closure");
}

std::string PairCode::QrCodeUrl(const std::string& server_url) const {
  std::string base = server_url;
  while (!base.empty() && base.back() == '/') base.pop_back();
  return base + "/pair?code=" + short_code_;
}

}  // namespace relayly
