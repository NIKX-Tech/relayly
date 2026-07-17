#include "noise/session.hpp"

#include <stdexcept>

namespace relayly::noise {

std::vector<std::uint8_t> EncodeEnvelope(std::uint8_t kind, const std::vector<std::uint8_t>& payload) {
  std::vector<std::uint8_t> frame;
  frame.reserve(payload.size() + 1);
  frame.push_back(kind);
  frame.insert(frame.end(), payload.begin(), payload.end());
  return frame;
}

std::optional<std::pair<std::uint8_t, std::vector<std::uint8_t>>> DecodeEnvelope(
    const std::vector<std::uint8_t>& frame) {
  if (frame.empty()) return std::nullopt;
  return std::make_pair(frame.front(), std::vector<std::uint8_t>(frame.begin() + 1, frame.end()));
}

std::pair<NoiseSession, std::vector<std::uint8_t>> NoiseSession::AsInitiator(const KeyPair& static_keypair) {
  NoiseSession session;
  session.handshake_.emplace(true, static_keypair);
  auto result = session.handshake_->WriteMessage();
  // A single WriteMessage can never complete the 3-message XX pattern on its own.
  return {std::move(session), std::move(result.message)};
}

NoiseSession NoiseSession::AsResponder(const KeyPair& static_keypair) {
  NoiseSession session;
  session.handshake_.emplace(false, static_keypair);
  return session;
}

void NoiseSession::Finish(const HandshakeState::SplitResult& split) {
  send_ = split.send;
  recv_ = split.recv;
  peer_static_ = split.peer_static;
  status_ = SessionStatus::kReady;
  handshake_.reset();
}

NoiseSession::HandshakeResult NoiseSession::HandleHandshakeMessage(const std::vector<std::uint8_t>& data) {
  if (status_ != SessionStatus::kHandshaking || !handshake_.has_value()) {
    status_ = SessionStatus::kFailed;
    return {std::nullopt, true};
  }

  try {
    auto read_result = handshake_->ReadMessage(data);
    if (read_result.split.has_value()) {
      Finish(*read_result.split);
      return {std::nullopt, true};
    }

    auto write_result = handshake_->WriteMessage();
    if (write_result.split.has_value()) {
      auto reply = write_result.message;
      Finish(*write_result.split);
      return {std::move(reply), true};
    }
    return {std::move(write_result.message), false};
  } catch (const std::exception&) {
    status_ = SessionStatus::kFailed;
    return {std::nullopt, true};
  }
}

const Key& NoiseSession::peer_static_key() const {
  if (status_ != SessionStatus::kReady) {
    throw NotReadyError();
  }
  return peer_static_;
}

std::vector<std::uint8_t> NoiseSession::Encrypt(const std::vector<std::uint8_t>& plaintext) {
  if (status_ != SessionStatus::kReady) {
    throw NotReadyError();
  }
  return send_->EncryptWithAd({}, plaintext);
}

std::vector<std::uint8_t> NoiseSession::Decrypt(const std::vector<std::uint8_t>& ciphertext) {
  if (status_ != SessionStatus::kReady) {
    throw NotReadyError();
  }
  return recv_->DecryptWithAd({}, ciphertext);
}

}  // namespace relayly::noise
