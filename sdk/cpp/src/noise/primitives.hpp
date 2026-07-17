#pragma once

// Thin wrappers around libsodium (X25519, ChaCha20-Poly1305) and the vendored
// BLAKE2s reference implementation (third_party/blake2/), used to build the
// Noise_XX_25519_ChaChaPoly_BLAKE2s state machine in cipher_state.hpp/
// symmetric_state.hpp/handshake_state.hpp. See sdk/cpp/README.md's "Why libsodium +
// hand-written XX, not noise-c?" section for why these primitives are combined by
// hand rather than driving an existing Noise library.

#include <array>
#include <cstdint>
#include <stdexcept>
#include <utility>
#include <vector>

namespace relayly::noise {

constexpr std::size_t kKeyLen = 32;   // DHLEN and HASHLEN are both 32 for this suite.
constexpr std::size_t kTagLen = 16;   // ChaChaPoly AEAD tag length.

using Key = std::array<std::uint8_t, kKeyLen>;

struct KeyPair {
  Key private_key;
  Key public_key;
};

/// Thrown when AEAD decryption fails (bad tag / tampered ciphertext) — the only
/// exception type this layer throws; callers translate it into a failed handshake
/// or dropped message rather than letting it propagate.
class DecryptError : public std::runtime_error {
 public:
  DecryptError() : std::runtime_error("relayly: AEAD decryption failed") {}
};

KeyPair GenerateKeypair();
Key PublicKeyFromPrivate(const Key& private_key);
Key Dh(const Key& private_key, const Key& public_key);

Key Hash(const std::uint8_t* data, std::size_t len);
inline Key Hash(const std::vector<std::uint8_t>& data) { return Hash(data.data(), data.size()); }

/// Noise's HKDF (spec §4.3): exactly two output blocks, matching every MixKey/Split
/// call in the XX pattern. Built on a hand-written HMAC-BLAKE2s (block size 64
/// bytes) since libsodium has no generic HMAC-with-arbitrary-hash API.
std::pair<Key, Key> Hkdf2(const Key& chaining_key, const std::vector<std::uint8_t>& input_key_material);

/// Nonce encoding: 4 zero bytes + 8-byte little-endian counter (docs/PROTOCOL.md §6).
std::vector<std::uint8_t> AeadEncrypt(const Key& key, std::uint64_t nonce,
                                       const std::vector<std::uint8_t>& ad,
                                       const std::vector<std::uint8_t>& plaintext);
/// Throws DecryptError on authentication failure.
std::vector<std::uint8_t> AeadDecrypt(const Key& key, std::uint64_t nonce,
                                       const std::vector<std::uint8_t>& ad,
                                       const std::vector<std::uint8_t>& ciphertext);

}  // namespace relayly::noise
