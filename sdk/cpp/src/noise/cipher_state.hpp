#pragma once

#include <cstdint>
#include <optional>
#include <vector>

#include "noise/primitives.hpp"

namespace relayly::noise {

/// One direction's transport cipher state (Noise spec §5.1): a key plus a strictly
/// increasing nonce counter. Encrypt/decrypt are no-ops (return the input unchanged)
/// before a key is set, matching the Noise spec's fallback for EncryptAndHash calls
/// during the handshake before MixKey has run.
class CipherState {
 public:
  void InitializeKey(const Key& key);
  bool HasKey() const { return key_.has_value(); }

  std::vector<std::uint8_t> EncryptWithAd(const std::vector<std::uint8_t>& ad,
                                          const std::vector<std::uint8_t>& plaintext);
  std::vector<std::uint8_t> DecryptWithAd(const std::vector<std::uint8_t>& ad,
                                          const std::vector<std::uint8_t>& ciphertext);

 private:
  std::optional<Key> key_;
  std::uint64_t n_ = 0;
};

}  // namespace relayly::noise
