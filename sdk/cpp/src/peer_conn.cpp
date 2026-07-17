#include "peer_conn.hpp"

namespace relayly {

std::vector<std::uint8_t> PeerConn::StartAsInitiator(const noise::KeyPair& private_key) {
  auto [session, msg1] = noise::NoiseSession::AsInitiator(private_key);
  active_ = std::move(session);
  return msg1;
}

std::vector<std::uint8_t> PeerConn::StartRekeyAsInitiator(const noise::KeyPair& private_key) {
  auto [session, msg1] = noise::NoiseSession::AsInitiator(private_key);
  pending_ = std::move(session);
  return msg1;
}

HandshakeOutcome PeerConn::HandleHandshakeEnvelope(const noise::KeyPair& private_key,
                                                    const std::vector<std::uint8_t>& data) {
  bool was_pending = false;
  noise::NoiseSession* session = nullptr;

  if (pending_.has_value()) {
    was_pending = true;
    session = &*pending_;
  } else if (active_.has_value() && !active_->ready()) {
    // Continuing the (first-ever) in-progress handshake.
    session = &*active_;
  } else if (!active_.has_value()) {
    // No session at all yet: this incoming msg1 starts the very first handshake
    // for this peer, with us as responder.
    active_ = noise::NoiseSession::AsResponder(private_key);
    session = &*active_;
  } else {
    // active exists and is ready: an unsolicited msg1 on a healthy connection.
    auto now = std::chrono::steady_clock::now();
    if (last_unsolicited_msg1_.has_value() && (now - *last_unsolicited_msg1_) < kUnsolicitedMsg1MinInterval) {
      return {};  // rate-limited, drop silently
    }
    last_unsolicited_msg1_ = now;
    was_pending = true;
    pending_ = noise::NoiseSession::AsResponder(private_key);
    session = &*pending_;
  }

  auto result = session->HandleHandshakeMessage(data);
  HandshakeOutcome outcome;
  outcome.reply = result.reply;
  outcome.done = result.done;
  outcome.was_pending = was_pending;
  if (result.done) {
    outcome.failed = session->failed();
    if (!outcome.failed) {
      outcome.peer_static_key = session->peer_static_key();
    }
  }
  return outcome;
}

void PeerConn::PromotePending() {
  if (pending_.has_value()) {
    active_ = std::move(*pending_);
    pending_.reset();
  }
}

void PeerConn::AbandonPending() { pending_.reset(); }

std::vector<std::uint8_t> PeerConn::Send(const std::vector<std::uint8_t>& payload) {
  if (!active_.has_value()) {
    throw noise::NotReadyError();
  }
  return active_->Encrypt(payload);
}

std::vector<std::uint8_t> PeerConn::Recv(const std::vector<std::uint8_t>& ciphertext) {
  if (!active_.has_value()) {
    throw noise::NotReadyError();
  }
  return active_->Decrypt(ciphertext);
}

}  // namespace relayly
