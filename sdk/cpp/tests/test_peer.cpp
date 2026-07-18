// Tests for PeerConn — make-before-break (docs/PROTOCOL.md §6) and the
// unsolicited-msg1 rate limit, matching the other four SDKs' peer tests.

#include <catch2/catch_test_macros.hpp>

#include <string>

#include "noise/primitives.hpp"
#include "peer_conn.hpp"

using namespace relayly;
using namespace relayly::noise;

namespace {

std::vector<std::uint8_t> Str(const std::string& s) { return {s.begin(), s.end()}; }
std::string ToStr(const std::vector<std::uint8_t>& b) { return {b.begin(), b.end()}; }

/// Drives a full handshake between a fresh initiator NoiseSession and a PeerConn
/// playing the responder role, returning both once ready.
std::pair<NoiseSession, PeerConn> CompleteFirstHandshake(const KeyPair& a_key, const KeyPair& b_key) {
  auto [a_session, msg1] = NoiseSession::AsInitiator(a_key);
  PeerConn b_peer("");

  auto outcome1 = b_peer.HandleHandshakeEnvelope(b_key, msg1);
  REQUIRE_FALSE(outcome1.done);
  REQUIRE(outcome1.reply.has_value());

  auto a_result = a_session.HandleHandshakeMessage(*outcome1.reply);
  REQUIRE(a_result.done);

  auto outcome2 = b_peer.HandleHandshakeEnvelope(b_key, *a_result.reply);
  REQUIRE_FALSE(outcome2.reply.has_value());
  REQUIRE(outcome2.done);
  REQUIRE_FALSE(outcome2.was_pending);  // first-ever handshake, not a rekey
  REQUIRE_FALSE(outcome2.failed);
  // First-ever handshake: nothing to promote, it's already sitting in `active`.

  return {std::move(a_session), std::move(b_peer)};
}

}  // namespace

TEST_CASE("PeerConn completes first handshake and roundtrips both ways", "[peer]") {
  auto a_key = GenerateKeypair();
  auto b_key = GenerateKeypair();
  auto [a_session, b_peer] = CompleteFirstHandshake(a_key, b_key);

  auto ct_a_to_b = a_session.Encrypt(Str("hello from A"));
  REQUIRE(ToStr(b_peer.Recv(ct_a_to_b)) == "hello from A");

  auto ct_b_to_a = b_peer.Send(Str("hello from B"));
  REQUIRE(ToStr(a_session.Decrypt(ct_b_to_a)) == "hello from B");
}

TEST_CASE("PeerConn make-before-break: an in-flight rekey never disturbs the existing session", "[peer]") {
  auto a_key = GenerateKeypair();
  auto b_key = GenerateKeypair();
  auto [a_session, b_peer] = CompleteFirstHandshake(a_key, b_key);

  // Inject an unsolicited rekey attempt that never completes (stop after msg1/msg2).
  auto [rekey_session, rekey_msg1] = NoiseSession::AsInitiator(a_key);
  auto outcome = b_peer.HandleHandshakeEnvelope(b_key, rekey_msg1);
  REQUIRE(outcome.was_pending);
  REQUIRE_FALSE(outcome.done);  // still mid-handshake

  // The original session must still work, both directions, throughout.
  auto ct1 = a_session.Encrypt(Str("still using the old session"));
  REQUIRE(ToStr(b_peer.Recv(ct1)) == "still using the old session");

  auto ct2 = b_peer.Send(Str("original session still alive mid-rekey"));
  REQUIRE(ToStr(a_session.Decrypt(ct2)) == "original session still alive mid-rekey");
}

TEST_CASE("PeerConn rate-limits a second unsolicited msg1 after a failed first attempt", "[peer]") {
  auto a_key = GenerateKeypair();
  auto b_key = GenerateKeypair();
  auto [a_session, b_peer] = CompleteFirstHandshake(a_key, b_key);

  // First unsolicited attempt: accepted, then fails (garbage instead of a real msg2
  // reply) — settles as failed, and the client calls AbandonPending() on it.
  auto [rekey_session, rekey_msg1] = NoiseSession::AsInitiator(a_key);
  auto outcome = b_peer.HandleHandshakeEnvelope(b_key, rekey_msg1);
  REQUIRE(outcome.was_pending);
  std::vector<std::uint8_t> garbage{0xff, 0xff, 0xff, 0xff};
  auto failed_outcome = b_peer.HandleHandshakeEnvelope(b_key, garbage);
  REQUIRE(failed_outcome.done);
  REQUIRE(failed_outcome.failed);
  b_peer.AbandonPending();

  // A second unsolicited attempt arriving immediately after must be dropped.
  auto [rekey2_session, rekey2_msg1] = NoiseSession::AsInitiator(a_key);
  auto outcome2 = b_peer.HandleHandshakeEnvelope(b_key, rekey2_msg1);
  REQUIRE_FALSE(outcome2.reply.has_value());
  REQUIRE_FALSE(outcome2.done);
  REQUIRE_FALSE(outcome2.was_pending);

  // The original session must still be unaffected throughout.
  auto ct = a_session.Encrypt(Str("still alive"));
  REQUIRE(ToStr(b_peer.Recv(ct)) == "still alive");
}
