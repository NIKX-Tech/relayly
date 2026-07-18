// PeerStore: pin-on-first-sight, matching re-announce, and mismatch rejection
// (docs/PROTOCOL.md §7.1) — mirrors the other four SDKs' peerstore tests.

#include <catch2/catch_test_macros.hpp>

#include <filesystem>
#include <fstream>
#include <random>
#include <sstream>

#include "relayly/peer_store.hpp"

using namespace relayly;

namespace {

std::string TempStorePath() {
  std::random_device rd;
  std::ostringstream name;
  name << (std::filesystem::temp_directory_path() / "relayly-peerstore-test-").string() << rd() << ".json";
  return name.str();
}

}  // namespace

TEST_CASE("PeerStore pins a peer's key on first sight", "[peerstore]") {
  auto path = TempStorePath();
  auto store = PeerStore::Load(path);

  REQUIRE_FALSE(store.Get("peer-a").has_value());
  store.PinOrVerify("peer-a", "aGVsbG8=");
  REQUIRE(store.Get("peer-a") == "aGVsbG8=");

  std::filesystem::remove(path);
}

TEST_CASE("PeerStore accepts a matching re-announce and persists across reloads", "[peerstore]") {
  auto path = TempStorePath();
  {
    auto store = PeerStore::Load(path);
    store.PinOrVerify("peer-a", "aGVsbG8=");
    store.PinOrVerify("peer-a", "aGVsbG8=");  // re-announcing the same key is a no-op
  }

  auto reloaded = PeerStore::Load(path);
  REQUIRE(reloaded.Get("peer-a") == "aGVsbG8=");

  std::filesystem::remove(path);
}

TEST_CASE("PeerStore rejects a mismatched key and keeps the original pin", "[peerstore]") {
  auto path = TempStorePath();
  auto store = PeerStore::Load(path);
  store.PinOrVerify("peer-a", "aGVsbG8=");

  REQUIRE_THROWS_AS(store.PinOrVerify("peer-a", "d29ybGQ="), PeerKeyMismatchError);
  REQUIRE(store.Get("peer-a") == "aGVsbG8=");  // original pin untouched

  std::filesystem::remove(path);
}
