#pragma once

#include <optional>
#include <utility>
#include <vector>

#include "noise/cipher_state.hpp"
#include "noise/primitives.hpp"
#include "noise/symmetric_state.hpp"

namespace relayly::noise {

/// Drives exactly the Noise XX pattern (-> e / <- e, ee, s, es / -> s, se) —
/// hardcoded, not a generic multi-pattern engine, since XX is all Protocol v1 uses
/// and a narrower state machine is less surface for a mistake. Mirrors sdk/ts's
/// handshakeState.ts, verified byte-for-byte against the same flynn/noise vectors
/// (see tests/test_noise.cpp).
class HandshakeState {
 public:
  struct SplitResult {
    CipherState send;
    CipherState recv;
    Key peer_static{};
  };

  struct MessageResult {
    std::vector<std::uint8_t> message;    // only set by WriteMessage
    std::optional<SplitResult> split;     // set once the 3-message pattern completes
  };

  HandshakeState(bool initiator, const KeyPair& static_keypair);

  /// Test-only constructor: uses a fixed ephemeral keypair instead of a random one,
  /// so handshake transcripts are reproducible against known-good vectors.
  HandshakeState(bool initiator, const KeyPair& static_keypair, const KeyPair& fixed_ephemeral);

  MessageResult WriteMessage();
  MessageResult ReadMessage(const std::vector<std::uint8_t>& message);

  const Key& handshake_hash() const { return symmetric_.handshake_hash(); }

 private:
  KeyPair GenerateOrFixedEphemeral();

  bool initiator_;
  int message_index_ = 0;  // 0, 1, 2 across the pattern's three messages
  SymmetricState symmetric_;

  KeyPair s_;
  std::optional<KeyPair> e_;
  std::optional<Key> re_;
  std::optional<Key> rs_;

  std::optional<KeyPair> fixed_ephemeral_;
};

}  // namespace relayly::noise
