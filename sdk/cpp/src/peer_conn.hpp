#pragma once

#include <chrono>
#include <optional>
#include <string>
#include <vector>

#include "noise/session.hpp"

namespace relayly {

/// Throttles how often this client starts a brand new responder session for an
/// unsolicited msg1 arriving on an already-healthy peer connection. The relay is
/// untrusted (docs/PROTOCOL.md §6, §7); without this a malicious/compromised relay
/// could force perpetual handshake churn by injecting 0x01 frames.
inline constexpr std::chrono::seconds kUnsolicitedMsg1MinInterval{2};

/// Result of feeding one handshake envelope to a PeerConn (see
/// HandleHandshakeEnvelope). Carries everything the caller needs to finish resolving
/// a completed handshake; PeerConn keeps the NoiseSession itself internal and
/// exposes PromotePending/AbandonPending for the caller to decide its fate — mirrors
/// sdk/rust's HandshakeOutcome design (Rust's ownership rules forced that shape;
/// here it's used for symmetry with the rest of this codebase, not necessity, but
/// there is no reason to diverge).
struct HandshakeOutcome {
  std::optional<std::vector<std::uint8_t>> reply;
  bool done = false;
  /// True if this was a rekey attempt (vs. the peer's first-ever handshake).
  bool was_pending = false;
  /// The authenticated peer static key. Set only when done and the handshake
  /// succeeded.
  std::optional<noise::Key> peer_static_key;
  /// Set when done and the handshake failed.
  bool failed = false;
};

/// Tracks everything this client knows about one paired device: the currently
/// active Noise session, and (while a rekey is in flight) a pending replacement
/// session that must not replace active until it actually completes —
/// docs/PROTOCOL.md §6's make-before-break rule. Mirrors sdk/go's peer.go.
class PeerConn {
 public:
  explicit PeerConn(std::string announced_static_key) : announced_static_key(std::move(announced_static_key)) {}

  /// Begins the very first handshake for this peer (§5.3: the accepting device
  /// initiates). Returns the msg1 payload to send.
  std::vector<std::uint8_t> StartAsInitiator(const noise::KeyPair& private_key);

  /// Begins a replacement handshake (§6: triggered when this device's ID is
  /// lexicographically smaller than the peer's, on any reconnect). The existing
  /// active session, if any, keeps working until the replacement completes.
  std::vector<std::uint8_t> StartRekeyAsInitiator(const noise::KeyPair& private_key);

  /// Feeds one received ENVELOPE_HANDSHAKE payload to the right session, starting a
  /// new responder session if needed and applying make-before-break plus
  /// unsolicited-msg1 rate limiting.
  HandshakeOutcome HandleHandshakeEnvelope(const noise::KeyPair& private_key, const std::vector<std::uint8_t>& data);

  /// Swaps a just-completed pending session into active. No-op if there was no
  /// pending session (the peer's first-ever handshake has nothing to promote —
  /// it's already sitting in active).
  void PromotePending();

  /// Drops a failed pending replacement, leaving the existing active session
  /// (still healthy) untouched.
  void AbandonPending();

  /// Encrypts payload for this peer using the active session, or throws
  /// noise::NotReadyError if there isn't a ready one yet.
  std::vector<std::uint8_t> Send(const std::vector<std::uint8_t>& payload);

  /// Decrypts an incoming transport envelope using the active session.
  std::vector<std::uint8_t> Recv(const std::vector<std::uint8_t>& ciphertext);

  bool is_active_ready() const { return active_.has_value() && active_->ready(); }

  std::string announced_static_key;

 private:
  std::optional<noise::NoiseSession> active_;
  std::optional<noise::NoiseSession> pending_;
  std::optional<std::chrono::steady_clock::time_point> last_unsolicited_msg1_;
};

}  // namespace relayly
