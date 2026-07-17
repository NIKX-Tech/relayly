#include "noise/symmetric_state.hpp"

#include <algorithm>

namespace relayly::noise {

SymmetricState SymmetricState::Initialize(const std::string& protocol_name) {
  SymmetricState s;
  std::vector<std::uint8_t> name_bytes(protocol_name.begin(), protocol_name.end());
  if (name_bytes.size() <= kKeyLen) {
    std::fill(s.h_.begin(), s.h_.end(), std::uint8_t{0});
    std::copy(name_bytes.begin(), name_bytes.end(), s.h_.begin());
  } else {
    s.h_ = Hash(name_bytes);
  }
  s.ck_ = s.h_;
  return s;
}

void SymmetricState::MixKey(const std::vector<std::uint8_t>& input_key_material) {
  auto [new_ck, temp_k] = Hkdf2(ck_, input_key_material);
  ck_ = new_ck;
  cipher_state_.InitializeKey(temp_k);
}

void SymmetricState::MixHash(const std::vector<std::uint8_t>& data) {
  std::vector<std::uint8_t> combined(h_.begin(), h_.end());
  combined.insert(combined.end(), data.begin(), data.end());
  h_ = Hash(combined);
}

std::vector<std::uint8_t> SymmetricState::EncryptAndHash(const std::vector<std::uint8_t>& plaintext) {
  std::vector<std::uint8_t> ad(h_.begin(), h_.end());
  auto ciphertext = cipher_state_.EncryptWithAd(ad, plaintext);
  MixHash(ciphertext);
  return ciphertext;
}

std::vector<std::uint8_t> SymmetricState::DecryptAndHash(const std::vector<std::uint8_t>& ciphertext) {
  std::vector<std::uint8_t> ad(h_.begin(), h_.end());
  auto plaintext = cipher_state_.DecryptWithAd(ad, ciphertext);
  MixHash(ciphertext);
  return plaintext;
}

std::pair<CipherState, CipherState> SymmetricState::Split() {
  auto [temp_k1, temp_k2] = Hkdf2(ck_, {});
  CipherState c1;
  CipherState c2;
  c1.InitializeKey(temp_k1);
  c2.InitializeKey(temp_k2);
  return {c1, c2};
}

}  // namespace relayly::noise
