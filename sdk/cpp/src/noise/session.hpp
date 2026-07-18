#pragma once

#include <optional>
#include <stdexcept>
#include <utility>
#include <vector>

#include "noise/handshake_state.hpp"

namespace relayly::noise {

/// The 1-byte envelope prefix identifying a binary WebSocket frame's payload
/// (docs/PROTOCOL.md §4). The relay forwards these verbatim without inspecting them.
inline constexpr std::uint8_t kEnvelopeHandshake = 0x01;
inline constexpr std::uint8_t kEnvelopeTransport = 0x02;

/// Prefixes payload with kind, ready to send as a single binary WS frame.
std::vector<std::uint8_t> EncodeEnvelope(std::uint8_t kind, const std::vector<std::uint8_t>& payload);

/// Splits a received binary WS frame into (kind, payload). Returns nullopt if frame
/// is empty (malformed — every envelope has at least the 1-byte kind prefix).
std::optional<std::pair<std::uint8_t, std::vector<std::uint8_t>>> DecodeEnvelope(const std::vector<std::uint8_t>& frame);

/// Thrown by Encrypt/Decrypt when the session hasn't finished handshaking yet.
class NotReadyError : public std::runtime_error {
 public:
  NotReadyError() : std::runtime_error("relayly: peer session is not ready") {}
};

enum class SessionStatus { kHandshaking, kReady, kFailed };

/// Drives exactly one Noise XX handshake, as either role, and once it completes,
/// encrypts/decrypts transport messages for that session. Does not itself implement
/// the make-before-break replacement policy — see peer_conn.hpp for the wrapper that
/// decides when a new NoiseSession may replace an existing one. Mirrors sdk/go's
/// noise.go / sdk/ts's session.ts.
class NoiseSession {
 public:
  /// Starts a handshake as the Noise initiator and returns (session, msg1) — msg1 is
  /// the first message to send as an ENVELOPE_HANDSHAKE frame.
  static std::pair<NoiseSession, std::vector<std::uint8_t>> AsInitiator(const KeyPair& static_keypair);

  /// Starts a handshake as the Noise responder, ready to receive msg1.
  static NoiseSession AsResponder(const KeyPair& static_keypair);

  struct HandshakeResult {
    std::optional<std::vector<std::uint8_t>> reply;
    bool done = false;
  };

  /// Feeds one received ENVELOPE_HANDSHAKE payload into the state machine. done is
  /// true once the handshake has completed (successfully or not) — check failed()
  /// in that case. Never throws: any failure (bad AEAD tag, malformed message,
  /// wrong state) transitions to kFailed rather than propagating.
  HandshakeResult HandleHandshakeMessage(const std::vector<std::uint8_t>& data);

  bool ready() const { return status_ == SessionStatus::kReady; }
  bool failed() const { return status_ == SessionStatus::kFailed; }

  /// The authenticated peer static key. Only valid once ready() is true.
  const Key& peer_static_key() const;

  std::vector<std::uint8_t> Encrypt(const std::vector<std::uint8_t>& plaintext);
  std::vector<std::uint8_t> Decrypt(const std::vector<std::uint8_t>& ciphertext);

 private:
  NoiseSession() = default;
  void Finish(const HandshakeState::SplitResult& split);

  SessionStatus status_ = SessionStatus::kHandshaking;
  std::optional<HandshakeState> handshake_;
  std::optional<CipherState> send_;
  std::optional<CipherState> recv_;
  Key peer_static_{};
};

}  // namespace relayly::noise
