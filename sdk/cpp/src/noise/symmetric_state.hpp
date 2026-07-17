#pragma once

#include <string>
#include <utility>
#include <vector>

#include "noise/cipher_state.hpp"
#include "noise/primitives.hpp"

namespace relayly::noise {

/// Noise spec §5.2: chaining key + handshake hash + a CipherState, driving the
/// handshake's MixKey/MixHash/EncryptAndHash/DecryptAndHash/Split operations.
class SymmetricState {
 public:
  /// protocol_name is "Noise_XX_25519_ChaChaPoly_BLAKE2s" (33 bytes > HASHLEN=32,
  /// so per spec h = Hash(protocol_name) rather than being zero-padded).
  static SymmetricState Initialize(const std::string& protocol_name);

  void MixKey(const std::vector<std::uint8_t>& input_key_material);
  void MixHash(const std::vector<std::uint8_t>& data);

  std::vector<std::uint8_t> EncryptAndHash(const std::vector<std::uint8_t>& plaintext);
  std::vector<std::uint8_t> DecryptAndHash(const std::vector<std::uint8_t>& ciphertext);

  std::pair<CipherState, CipherState> Split();

  const Key& handshake_hash() const { return h_; }

 private:
  Key ck_{};
  Key h_{};
  CipherState cipher_state_;
};

}  // namespace relayly::noise
