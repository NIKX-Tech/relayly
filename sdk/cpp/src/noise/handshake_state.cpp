#include "noise/handshake_state.hpp"

#include <stdexcept>

namespace relayly::noise {

namespace {
constexpr const char* kProtocolName = "Noise_XX_25519_ChaChaPoly_BLAKE2s";
}

HandshakeState::HandshakeState(bool initiator, const KeyPair& static_keypair)
    : initiator_(initiator), symmetric_(SymmetricState::Initialize(kProtocolName)), s_(static_keypair) {
  // MixHash(prologue) with an empty prologue — required by Initialize() even when
  // the prologue is zero-length (docs/PROTOCOL.md doesn't use one, but the step
  // itself is not optional; skipping it was a real bug caught in sdk/ts's first
  // implementation attempt).
  symmetric_.MixHash({});
}

HandshakeState::HandshakeState(bool initiator, const KeyPair& static_keypair, const KeyPair& fixed_ephemeral)
    : HandshakeState(initiator, static_keypair) {
  fixed_ephemeral_ = fixed_ephemeral;
}

KeyPair HandshakeState::GenerateOrFixedEphemeral() {
  if (fixed_ephemeral_.has_value()) {
    return *fixed_ephemeral_;
  }
  return GenerateKeypair();
}

HandshakeState::MessageResult HandshakeState::WriteMessage() {
  MessageResult result;
  std::vector<std::uint8_t> buffer;

  if (message_index_ == 0) {
    // -> e
    e_ = GenerateOrFixedEphemeral();
    buffer.insert(buffer.end(), e_->public_key.begin(), e_->public_key.end());
    symmetric_.MixHash(std::vector<std::uint8_t>(e_->public_key.begin(), e_->public_key.end()));
  } else if (message_index_ == 1) {
    // <- e, ee, s, es
    e_ = GenerateOrFixedEphemeral();
    buffer.insert(buffer.end(), e_->public_key.begin(), e_->public_key.end());
    symmetric_.MixHash(std::vector<std::uint8_t>(e_->public_key.begin(), e_->public_key.end()));

    auto ee = Dh(e_->private_key, *re_);
    symmetric_.MixKey(std::vector<std::uint8_t>(ee.begin(), ee.end()));

    auto s_ct = symmetric_.EncryptAndHash(std::vector<std::uint8_t>(s_.public_key.begin(), s_.public_key.end()));
    buffer.insert(buffer.end(), s_ct.begin(), s_ct.end());

    // es (responder writing): DH(local s, remote e)
    auto es = Dh(s_.private_key, *re_);
    symmetric_.MixKey(std::vector<std::uint8_t>(es.begin(), es.end()));
  } else if (message_index_ == 2) {
    // -> s, se
    auto s_ct = symmetric_.EncryptAndHash(std::vector<std::uint8_t>(s_.public_key.begin(), s_.public_key.end()));
    buffer.insert(buffer.end(), s_ct.begin(), s_ct.end());

    // se (initiator writing): DH(local s, remote e)
    auto se = Dh(s_.private_key, *re_);
    symmetric_.MixKey(std::vector<std::uint8_t>(se.begin(), se.end()));
  } else {
    throw std::logic_error("relayly: WriteMessage called after XX pattern completed");
  }

  auto payload_ct = symmetric_.EncryptAndHash({});
  buffer.insert(buffer.end(), payload_ct.begin(), payload_ct.end());

  ++message_index_;
  result.message = std::move(buffer);
  if (message_index_ == 3) {
    auto [send, recv] = symmetric_.Split();
    result.split = SplitResult{std::move(send), std::move(recv), *rs_};
  }
  return result;
}

HandshakeState::MessageResult HandshakeState::ReadMessage(const std::vector<std::uint8_t>& message) {
  MessageResult result;
  std::size_t offset = 0;
  auto take = [&](std::size_t n) {
    if (offset + n > message.size()) {
      throw std::runtime_error("relayly: handshake message too short");
    }
    std::vector<std::uint8_t> out(message.begin() + static_cast<long>(offset),
                                   message.begin() + static_cast<long>(offset + n));
    offset += n;
    return out;
  };

  if (message_index_ == 0) {
    // -> e
    auto e_bytes = take(kKeyLen);
    Key re{};
    std::copy(e_bytes.begin(), e_bytes.end(), re.begin());
    re_ = re;
    symmetric_.MixHash(e_bytes);
  } else if (message_index_ == 1) {
    // <- e, ee, s, es
    auto e_bytes = take(kKeyLen);
    Key re{};
    std::copy(e_bytes.begin(), e_bytes.end(), re.begin());
    re_ = re;
    symmetric_.MixHash(e_bytes);

    auto ee = Dh(e_->private_key, *re_);
    symmetric_.MixKey(std::vector<std::uint8_t>(ee.begin(), ee.end()));

    bool has_key_before_s = true;  // MixKey(ee) always ran just above, so cipher has a key here.
    std::size_t s_field_len = has_key_before_s ? (kKeyLen + kTagLen) : kKeyLen;
    auto s_ct = take(s_field_len);
    auto s_pt = symmetric_.DecryptAndHash(s_ct);
    Key rs{};
    std::copy(s_pt.begin(), s_pt.end(), rs.begin());
    rs_ = rs;

    // es (initiator reading): DH(local e, remote s)
    auto es = Dh(e_->private_key, *rs_);
    symmetric_.MixKey(std::vector<std::uint8_t>(es.begin(), es.end()));
  } else if (message_index_ == 2) {
    // -> s, se
    auto s_ct = take(kKeyLen + kTagLen);
    auto s_pt = symmetric_.DecryptAndHash(s_ct);
    Key rs{};
    std::copy(s_pt.begin(), s_pt.end(), rs.begin());
    rs_ = rs;

    // se (responder reading): DH(local e, remote s)
    auto se = Dh(e_->private_key, *rs_);
    symmetric_.MixKey(std::vector<std::uint8_t>(se.begin(), se.end()));
  } else {
    throw std::logic_error("relayly: ReadMessage called after XX pattern completed");
  }

  std::vector<std::uint8_t> remaining(message.begin() + static_cast<long>(offset), message.end());
  symmetric_.DecryptAndHash(remaining);  // empty payload; discard, just advances hash/nonce

  ++message_index_;
  if (message_index_ == 3) {
    auto [send, recv] = symmetric_.Split();
    // Responder's send/recv are swapped relative to initiator (Split() returns
    // (c1, c2) = (initiator->responder, responder->initiator) by Noise convention).
    if (initiator_) {
      result.split = SplitResult{std::move(send), std::move(recv), *rs_};
    } else {
      result.split = SplitResult{std::move(recv), std::move(send), *rs_};
    }
  }
  return result;
}

}  // namespace relayly::noise
