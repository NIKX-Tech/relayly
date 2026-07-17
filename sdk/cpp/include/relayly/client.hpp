#pragma once

#include <chrono>
#include <cstdint>
#include <functional>
#include <future>
#include <memory>
#include <span>
#include <string>
#include <vector>

#include "relayly/crypto.hpp"

namespace relayly {

inline constexpr std::chrono::seconds kDefaultPingInterval{30};
inline constexpr std::chrono::seconds kDefaultReconnectDelay{1};
inline constexpr std::chrono::seconds kDefaultMaxReconnectDelay{60};

/// A paired remote device.
struct Peer {
  std::string id;
  PublicKey public_key;
};

/// An incoming decrypted message from a paired peer.
struct Message {
  std::string from;
  std::vector<std::uint8_t> payload;
  /// When this client received and decrypted the message. The E2E transport
  /// envelope (docs/PROTOCOL.md §6) carries no timestamp of its own, so this is a
  /// local receipt time, not a server-assigned one.
  std::chrono::system_clock::time_point timestamp;
};

/// Configures a Client connection. All callbacks fire on IXWebSocket's internal
/// I/O thread, never the calling thread — see README.md's threading model section.
struct Options {
  /// Unique identifier for this device, registered with the server. Required.
  std::string device_id;
  /// Authenticates this device to the relay (docs/PROTOCOL.md §2, §3). Obtain one
  /// from POST /api/v1/devices or the relayly CLI's `pair` command. Required.
  std::string device_token;
  /// This device's X25519 static identity, used as the Noise XX static keypair.
  /// Generate one with PrivateKey::Generate() or PrivateKey::LoadOrGenerate().
  /// Required.
  PrivateKey private_key;
  /// Where pinned peer static keys (docs/PROTOCOL.md §7.1) are persisted. Defaults
  /// to kDefaultPeerStorePath if empty.
  std::string peer_store_path;

  std::chrono::seconds ping_interval{kDefaultPingInterval};
  /// Initial delay before reconnect attempts. Set negative to disable automatic
  /// reconnection entirely.
  std::chrono::seconds reconnect_delay{kDefaultReconnectDelay};
  std::chrono::seconds max_reconnect_delay{kDefaultMaxReconnectDelay};

  /// Called each time the client successfully reconnects (after the very first
  /// connect, which Connect() itself blocks on).
  std::function<void()> on_reconnect;
  /// Called when the connection is lost or a background connect attempt fails.
  std::function<void(const std::string& reason)> on_disconnect;
  /// Called whenever a peer's Noise session becomes usable for Send — both after
  /// the very first pairing and after any later re-handshake following a reconnect
  /// (docs/PROTOCOL.md §6). request_pair_code/accept_pair/PairCode::wait already
  /// resolve once the first handshake completes, so this is mainly useful for
  /// noticing when a peer recovers after a reconnect.
  std::function<void(const std::string& peer_id)> on_ready;
  /// Called whenever the server reports the paired peer's online/offline
  /// transition (docs/PROTOCOL.md §5.1's peer_status).
  std::function<void(const std::string& peer_id, bool online)> on_peer_status;
  /// Called for every incoming decrypted message.
  std::function<void(const Message&)> on_message;
};

class Client;

/// Returned by Client::RequestPairCode. Share short_code() with the other device,
/// then call wait() to get a future that resolves once that device accepts the
/// pairing (including the resulting Noise handshake completing).
class PairCode {
 public:
  const std::string& short_code() const { return short_code_; }
  int expires_in() const { return expires_in_; }

  /// A URL encoding both the server address and pairing code, of the form
  /// `<server_url>/pair?code=<short_code>`, suitable for generating a QR code.
  std::string QrCodeUrl(const std::string& server_url) const;

  std::future<Peer> wait() { return promise_->get_future(); }

 private:
  friend class Client;
  std::string short_code_;
  int expires_in_ = 0;
  std::shared_ptr<std::promise<Peer>> promise_;
};

/// A connected Relayly client. Use Client::Connect to create one.
class Client {
 public:
  /// Dials a Relayly server, authenticates, and blocks until the initial control
  /// handshake (welcome + announce_key) completes. Throws relayly::Error on
  /// failure (invalid options, unreachable server, protocol mismatch, auth
  /// failure).
  static std::unique_ptr<Client> Connect(const std::string& server_url, Options options);

  ~Client();
  Client(const Client&) = delete;
  Client& operator=(const Client&) = delete;

  /// Encrypts and sends payload to a paired peer. Throws relayly::Error(kPeerNotFound)
  /// if peer_id isn't paired, or relayly::Error(kNotReady) if its Noise session
  /// isn't up yet (notably during the brief window after a reconnect forces a
  /// re-handshake, §6).
  void Send(const std::string& peer_id, std::span<const std::byte> payload);

  /// Asks the server for a short pairing code. Blocks until the code arrives (a
  /// prompt server round trip); the returned PairCode's wait() then resolves once
  /// the other device accepts.
  PairCode RequestPairCode();

  /// Uses a 6-digit code from another device to complete pairing. The returned
  /// future resolves once the resulting Noise handshake completes (§5.3: the
  /// accepting device is the initiator), so the Peer it yields can be Sent to
  /// immediately.
  std::future<Peer> AcceptPair(const std::string& code);

  /// Gracefully closes the connection. Safe to call more than once.
  void Close();

 private:
  Client() = default;

  struct Impl;
  std::unique_ptr<Impl> impl_;
};

}  // namespace relayly
