#include "noise/cipher_state.hpp"

namespace relayly::noise {

void CipherState::InitializeKey(const Key& key) {
  key_ = key;
  n_ = 0;
}

std::vector<std::uint8_t> CipherState::EncryptWithAd(const std::vector<std::uint8_t>& ad,
                                                     const std::vector<std::uint8_t>& plaintext) {
  if (!key_.has_value()) {
    return plaintext;
  }
  auto out = AeadEncrypt(*key_, n_, ad, plaintext);
  ++n_;
  return out;
}

std::vector<std::uint8_t> CipherState::DecryptWithAd(const std::vector<std::uint8_t>& ad,
                                                     const std::vector<std::uint8_t>& ciphertext) {
  if (!key_.has_value()) {
    return ciphertext;
  }
  auto out = AeadDecrypt(*key_, n_, ad, ciphertext);
  ++n_;
  return out;
}

}  // namespace relayly::noise
