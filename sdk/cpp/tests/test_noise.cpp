// flynn/noise cross-validation vectors — the same fixed keys/deterministic ephemeral
// bytes already embedded in sdk/ts/src/noise/noise.test.ts, sdk/py/tests/test_noise.py,
// and sdk/rust/src/noise.rs's test module, generated with a standalone Go program
// using flynn/noise (already proven server-side and in sdk/go). This is the actual
// correctness gate for this hand-written implementation, not the spec recalled from
// memory: Noise's AEAD auth means a subtle bug here fails loudly (decrypt/MAC
// failure), not silently producing plausible-looking wrong output.

#include <catch2/catch_test_macros.hpp>

#include <algorithm>
#include <cstdio>
#include <string>

#include "noise/cipher_state.hpp"
#include "noise/handshake_state.hpp"
#include "noise/session.hpp"
#include "noise/symmetric_state.hpp"

using namespace relayly::noise;

namespace {

const std::string kAStaticPrivate = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20";
const std::string kBStaticPrivate = "101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f";
const std::string kAStaticPublic = "07a37cbc142093c8b755dc1b10e86cb426374ad16aa853ed0bdfc0b2b86d1c7c";
const std::string kBStaticPublic = "d89e3bad79437dbed9f843418304f460ff05c7fe81fe4a9577a804cb9367ff66";
const std::string kMsg1 = "358072d6365880d1aeea329adf9121383851ed21a28e3b75e965d0d2cd166254";
const std::string kMsg2 =
    "34e42d4af5ef94a07a3a84201b889d4cd1a743cb27b11b6a10438a8feb8e5847ee0b2fa3bbca43904cbf6186d5e09fe6712"
    "8c94cc3e3da6d35bf21f0358c487d5300c27a709ae1da5b4951c9eb1f0afd64e57891c7894b617293b07c9a455311";
const std::string kMsg3 =
    "b8312f344cb91f060c34ae9a514f48981b3316af898179729fd217b843cf0f75b07d427b956b287b149ee47a4b0b71e3b82"
    "2b0f15bc616ce52af8a3dbeab8bc8";
const std::string kCtAToB1 = "a21eb0be51f6230018b2a51f1b501eb2885cf12b23e6351f1a577c43";
const std::string kCtBToA1 = "362c3040c6440177f0d09b74b5457be4fc12cc30733563aa87dc83b9";

std::vector<std::uint8_t> Hex(const std::string& s) {
  std::vector<std::uint8_t> out(s.size() / 2);
  for (std::size_t i = 0; i < out.size(); ++i) {
    out[i] = static_cast<std::uint8_t>(std::strtol(s.substr(i * 2, 2).c_str(), nullptr, 16));
  }
  return out;
}

std::string ToHex(const std::vector<std::uint8_t>& bytes) {
  std::string out;
  char buf[3];
  for (auto b : bytes) {
    std::snprintf(buf, sizeof(buf), "%02x", b);
    out += buf;
  }
  return out;
}

Key ToKey(const std::vector<std::uint8_t>& bytes) {
  Key k{};
  std::copy(bytes.begin(), bytes.end(), k.begin());
  return k;
}

std::vector<std::uint8_t> Str(const std::string& s) { return {s.begin(), s.end()}; }

/// det_bytes(seed) yields seed, seed+1, seed+2, ... — mirrors the other SDKs'
/// deterministic ephemeral key generator exactly.
KeyPair DetEphemeral(std::uint8_t seed) {
  KeyPair kp{};
  for (int i = 0; i < 32; ++i) {
    kp.private_key[static_cast<std::size_t>(i)] = static_cast<std::uint8_t>(seed + i);
  }
  kp.public_key = PublicKeyFromPrivate(kp.private_key);
  return kp;
}

}  // namespace

TEST_CASE("HandshakeState produces byte-identical messages to flynn/noise", "[noise]") {
  KeyPair a_static{ToKey(Hex(kAStaticPrivate)), ToKey(Hex(kAStaticPublic))};
  KeyPair b_static{ToKey(Hex(kBStaticPrivate)), ToKey(Hex(kBStaticPublic))};
  KeyPair a_eph = DetEphemeral(0x20);
  KeyPair b_eph = DetEphemeral(0x30);

  HandshakeState initiator(true, a_static, a_eph);
  HandshakeState responder(false, b_static, b_eph);

  auto m1 = initiator.WriteMessage();
  REQUIRE(ToHex(m1.message) == kMsg1);
  responder.ReadMessage(m1.message);

  auto m2 = responder.WriteMessage();
  REQUIRE(ToHex(m2.message) == kMsg2);
  initiator.ReadMessage(m2.message);

  auto m3 = initiator.WriteMessage();
  REQUIRE(ToHex(m3.message) == kMsg3);
  REQUIRE(m3.split.has_value());
  auto r3 = responder.ReadMessage(m3.message);
  REQUIRE(r3.split.has_value());

  REQUIRE(m3.split->peer_static == ToKey(Hex(kBStaticPublic)));
  REQUIRE(r3.split->peer_static == ToKey(Hex(kAStaticPublic)));

  auto ct1 = m3.split->send.EncryptWithAd({}, Str("hello from A"));
  REQUIRE(ToHex(ct1) == kCtAToB1);
  auto pt1 = r3.split->recv.DecryptWithAd({}, ct1);
  REQUIRE(std::string(pt1.begin(), pt1.end()) == "hello from A");

  auto ct2 = r3.split->send.EncryptWithAd({}, Str("hello from B"));
  REQUIRE(ToHex(ct2) == kCtBToA1);
  auto pt2 = m3.split->recv.DecryptWithAd({}, ct2);
  REQUIRE(std::string(pt2.begin(), pt2.end()) == "hello from B");
}

TEST_CASE("NoiseSession completes a handshake and roundtrips transport messages", "[noise]") {
  KeyPair a_static{ToKey(Hex(kAStaticPrivate)), ToKey(Hex(kAStaticPublic))};
  KeyPair b_static{ToKey(Hex(kBStaticPrivate)), ToKey(Hex(kBStaticPublic))};

  auto [initiator, msg1] = NoiseSession::AsInitiator(a_static);
  auto responder = NoiseSession::AsResponder(b_static);

  auto r1 = responder.HandleHandshakeMessage(msg1);
  REQUIRE_FALSE(r1.done);
  REQUIRE(r1.reply.has_value());

  auto r2 = initiator.HandleHandshakeMessage(*r1.reply);
  REQUIRE(r2.done);
  REQUIRE(initiator.ready());
  REQUIRE(r2.reply.has_value());

  auto r3 = responder.HandleHandshakeMessage(*r2.reply);
  REQUIRE(r3.done);
  REQUIRE(responder.ready());

  REQUIRE(initiator.peer_static_key() == ToKey(Hex(kBStaticPublic)));
  REQUIRE(responder.peer_static_key() == ToKey(Hex(kAStaticPublic)));

  auto ct = initiator.Encrypt(Str("hello from A"));
  auto pt = responder.Decrypt(ct);
  REQUIRE(std::string(pt.begin(), pt.end()) == "hello from A");

  auto ct2 = responder.Encrypt(Str("hello from B"));
  auto pt2 = initiator.Decrypt(ct2);
  REQUIRE(std::string(pt2.begin(), pt2.end()) == "hello from B");
}

TEST_CASE("NoiseSession rejects a corrupted transport ciphertext", "[noise]") {
  KeyPair a_static{ToKey(Hex(kAStaticPrivate)), ToKey(Hex(kAStaticPublic))};
  KeyPair b_static{ToKey(Hex(kBStaticPrivate)), ToKey(Hex(kBStaticPublic))};

  auto [initiator, msg1] = NoiseSession::AsInitiator(a_static);
  auto responder = NoiseSession::AsResponder(b_static);
  auto r1 = responder.HandleHandshakeMessage(msg1);
  auto r2 = initiator.HandleHandshakeMessage(*r1.reply);
  responder.HandleHandshakeMessage(*r2.reply);

  auto ct = initiator.Encrypt(Str("hi"));
  ct[0] ^= 0xff;
  REQUIRE_THROWS(responder.Decrypt(ct));
}

TEST_CASE("NoiseSession reports failure instead of throwing on a malformed handshake message", "[noise]") {
  KeyPair b_static{ToKey(Hex(kBStaticPrivate)), ToKey(Hex(kBStaticPublic))};
  auto responder = NoiseSession::AsResponder(b_static);

  std::vector<std::uint8_t> garbage{0xff, 0xff, 0xff, 0xff};
  auto result = responder.HandleHandshakeMessage(garbage);
  REQUIRE(result.done);
  REQUIRE(responder.failed());
  REQUIRE_FALSE(responder.ready());
}
