#include "noise/primitives.hpp"

#include <sodium.h>

#include <cstring>

#include "blake2.h"

namespace relayly::noise {

namespace {

constexpr std::size_t kBlake2sBlockLen = 64;  // BLOCKLEN for BLAKE2s (Noise spec §4.3).

Key Blake2s32(const std::uint8_t* data, std::size_t len) {
  Key out{};
  blake2s_state state;
  blake2s_init(&state, kKeyLen);
  if (len > 0) {
    blake2s_update(&state, data, len);
  }
  blake2s_final(&state, out.data(), kKeyLen);
  return out;
}

/// Generic HMAC construction (RFC 2104) over BLAKE2s, needed because Noise's HKDF
/// (spec §4.3) is defined in terms of HMAC-HASH for whichever hash the cipher suite
/// names — libsodium only ships HMAC-SHA256/SHA512, not HMAC-with-arbitrary-hash.
Key HmacBlake2s(const std::vector<std::uint8_t>& key, const std::vector<std::uint8_t>& data) {
  std::array<std::uint8_t, kBlake2sBlockLen> key_block{};
  if (key.size() > kBlake2sBlockLen) {
    Key hashed = Blake2s32(key.data(), key.size());
    std::copy(hashed.begin(), hashed.end(), key_block.begin());
  } else {
    std::copy(key.begin(), key.end(), key_block.begin());
  }

  std::array<std::uint8_t, kBlake2sBlockLen> ipad{};
  std::array<std::uint8_t, kBlake2sBlockLen> opad{};
  for (std::size_t i = 0; i < kBlake2sBlockLen; ++i) {
    ipad[i] = key_block[i] ^ 0x36;
    opad[i] = key_block[i] ^ 0x5c;
  }

  std::vector<std::uint8_t> inner;
  inner.reserve(kBlake2sBlockLen + data.size());
  inner.insert(inner.end(), ipad.begin(), ipad.end());
  inner.insert(inner.end(), data.begin(), data.end());
  Key inner_hash = Blake2s32(inner.data(), inner.size());

  std::vector<std::uint8_t> outer;
  outer.reserve(kBlake2sBlockLen + kKeyLen);
  outer.insert(outer.end(), opad.begin(), opad.end());
  outer.insert(outer.end(), inner_hash.begin(), inner_hash.end());
  return Blake2s32(outer.data(), outer.size());
}

/// Nonce encoding per docs/PROTOCOL.md §6: 4 zero bytes + 8-byte little-endian
/// counter, matching every other SDK's convention.
std::array<std::uint8_t, crypto_aead_chacha20poly1305_IETF_NPUBBYTES> EncodeNonce(std::uint64_t n) {
  static_assert(crypto_aead_chacha20poly1305_IETF_NPUBBYTES == 12);
  std::array<std::uint8_t, 12> out{};
  for (int i = 0; i < 8; ++i) {
    out[4 + i] = static_cast<std::uint8_t>((n >> (8 * i)) & 0xff);
  }
  return out;
}

}  // namespace

KeyPair GenerateKeypair() {
  KeyPair kp{};
  crypto_box_keypair(kp.public_key.data(), kp.private_key.data());
  return kp;
}

Key PublicKeyFromPrivate(const Key& private_key) {
  Key pub{};
  if (crypto_scalarmult_base(pub.data(), private_key.data()) != 0) {
    throw std::runtime_error("relayly: crypto_scalarmult_base failed");
  }
  return pub;
}

Key Dh(const Key& private_key, const Key& public_key) {
  Key shared{};
  if (crypto_scalarmult(shared.data(), private_key.data(), public_key.data()) != 0) {
    throw std::runtime_error("relayly: crypto_scalarmult (DH) failed");
  }
  return shared;
}

Key Hash(const std::uint8_t* data, std::size_t len) { return Blake2s32(data, len); }

std::pair<Key, Key> Hkdf2(const Key& chaining_key, const std::vector<std::uint8_t>& input_key_material) {
  std::vector<std::uint8_t> ck_bytes(chaining_key.begin(), chaining_key.end());
  Key temp_key = HmacBlake2s(ck_bytes, input_key_material);

  std::vector<std::uint8_t> temp_key_bytes(temp_key.begin(), temp_key.end());
  Key output1 = HmacBlake2s(temp_key_bytes, {0x01});

  std::vector<std::uint8_t> output1_plus_02(output1.begin(), output1.end());
  output1_plus_02.push_back(0x02);
  Key output2 = HmacBlake2s(temp_key_bytes, output1_plus_02);

  return {output1, output2};
}

std::vector<std::uint8_t> AeadEncrypt(const Key& key, std::uint64_t nonce, const std::vector<std::uint8_t>& ad,
                                       const std::vector<std::uint8_t>& plaintext) {
  auto nonce_bytes = EncodeNonce(nonce);
  std::vector<std::uint8_t> out(plaintext.size() + kTagLen);
  unsigned long long out_len = 0;
  crypto_aead_chacha20poly1305_ietf_encrypt(out.data(), &out_len, plaintext.data(), plaintext.size(), ad.data(),
                                             ad.size(), nullptr, nonce_bytes.data(), key.data());
  out.resize(out_len);
  return out;
}

std::vector<std::uint8_t> AeadDecrypt(const Key& key, std::uint64_t nonce, const std::vector<std::uint8_t>& ad,
                                       const std::vector<std::uint8_t>& ciphertext) {
  auto nonce_bytes = EncodeNonce(nonce);
  std::vector<std::uint8_t> out(ciphertext.size() >= kTagLen ? ciphertext.size() - kTagLen : 0);
  unsigned long long out_len = 0;
  if (crypto_aead_chacha20poly1305_ietf_decrypt(out.data(), &out_len, nullptr, ciphertext.data(), ciphertext.size(),
                                                 ad.data(), ad.size(), nonce_bytes.data(), key.data()) != 0) {
    throw DecryptError();
  }
  out.resize(out_len);
  return out;
}

}  // namespace relayly::noise
