// WireMessage (control-channel JSON, docs/PROTOCOL.md §5) and the binary envelope
// prefix (§4) — encode/decode round trips, forward-compat tolerance of unknown
// fields, and malformed-input handling.

#include <catch2/catch_test_macros.hpp>

#include "noise/session.hpp"
#include "wire.hpp"

using namespace relayly;
using namespace relayly::noise;

TEST_CASE("WireMessage round-trips a welcome message with peers", "[wire]") {
  auto text =
      R"({"type":"welcome","protocol_version":1,"device_id":"dev-a",)"
      R"("peers":[{"id":"dev-b","static_key":"abc123"}]})";

  auto msg = WireMessage::Decode(text);
  REQUIRE(msg.has_value());
  REQUIRE(msg->type == "welcome");
  REQUIRE(msg->protocol_version == 1u);
  REQUIRE(msg->device_id == "dev-a");
  REQUIRE(msg->peers.has_value());
  REQUIRE(msg->peers->size() == 1);
  REQUIRE((*msg->peers)[0].id == "dev-b");
  REQUIRE((*msg->peers)[0].static_key == "abc123");
}

TEST_CASE("WireMessage ignores unknown fields (forward compat)", "[wire]") {
  auto text = R"({"type":"pong","some_future_field":"whatever","nested":{"a":1}})";
  auto msg = WireMessage::Decode(text);
  REQUIRE(msg.has_value());
  REQUIRE(msg->type == "pong");
}

TEST_CASE("WireMessage::Decode returns nullopt on malformed JSON", "[wire]") {
  REQUIRE_FALSE(WireMessage::Decode("not json at all").has_value());
  REQUIRE_FALSE(WireMessage::Decode("{\"no_type\":true}").has_value());
  REQUIRE_FALSE(WireMessage::Decode("[1,2,3]").has_value());
}

TEST_CASE("WireMessage constructors encode only their relevant fields", "[wire]") {
  auto encoded = WireMessage::AnnounceKey("mykey==").Encode();
  auto decoded = WireMessage::Decode(encoded);
  REQUIRE(decoded.has_value());
  REQUIRE(decoded->type == "announce_key");
  REQUIRE(decoded->static_key == "mykey==");
  REQUIRE_FALSE(decoded->code.has_value());

  auto pair_accept = WireMessage::Decode(WireMessage::PairAccept("483921").Encode());
  REQUIRE(pair_accept->type == "pair_accept");
  REQUIRE(pair_accept->code == "483921");

  REQUIRE(WireMessage::Decode(WireMessage::PairRequest().Encode())->type == "pair_request");
  REQUIRE(WireMessage::Decode(WireMessage::Ping().Encode())->type == "ping");
}

TEST_CASE("WireMessage round-trips an error message (code doubles as machine code)", "[wire]") {
  auto text = R"({"type":"error","code":"invalid_code","message":"the code has expired or was never issued"})";
  auto msg = WireMessage::Decode(text);
  REQUIRE(msg.has_value());
  REQUIRE(msg->code == "invalid_code");
  REQUIRE(msg->message == "the code has expired or was never issued");
}

TEST_CASE("EncodeEnvelope/DecodeEnvelope round-trip both kinds", "[wire]") {
  std::vector<std::uint8_t> payload{1, 2, 3, 4, 5};

  auto handshake_frame = EncodeEnvelope(kEnvelopeHandshake, payload);
  auto decoded = DecodeEnvelope(handshake_frame);
  REQUIRE(decoded.has_value());
  REQUIRE(decoded->first == kEnvelopeHandshake);
  REQUIRE(decoded->second == payload);

  auto transport_frame = EncodeEnvelope(kEnvelopeTransport, payload);
  auto decoded2 = DecodeEnvelope(transport_frame);
  REQUIRE(decoded2->first == kEnvelopeTransport);
  REQUIRE(decoded2->second == payload);
}

TEST_CASE("DecodeEnvelope rejects an empty frame", "[wire]") {
  REQUIRE_FALSE(DecodeEnvelope({}).has_value());
}
