#pragma once

#include <optional>
#include <stdexcept>
#include <string>
#include <unordered_map>

namespace relayly {

/// Default location for pinned peer static keys, shared byte-for-byte with every
/// other official SDK (docs/tasks/02-sdks-and-interop.md), so the same file can be
/// read/written by clients written in different languages on the same machine.
inline constexpr const char* kDefaultPeerStorePath = "~/.relayly/peers.json";

/// The client-side pin check (§7.1, the real security boundary) rejecting a peer
/// whose authenticated static key differs from the one already pinned for that peer
/// ID. Unpinning is an explicit user action only; this error is never auto-retried.
class PeerKeyMismatchError : public std::runtime_error {
 public:
  explicit PeerKeyMismatchError(const std::string& peer_id)
      : std::runtime_error("relayly: peer's authenticated key does not match the pinned key (peer " + peer_id +
                            ")") {}
};

/// Persists pinned peer static keys (docs/PROTOCOL.md §7.1). A peer's key is pinned
/// the first time its Noise handshake completes; any later handshake presenting a
/// different key for the same peer ID hard-fails with PeerKeyMismatchError.
class PeerStore {
 public:
  /// Loads the peer store at path, creating an empty one in memory if the file
  /// doesn't exist yet (it is created on first successful pin). "~" is expanded to
  /// $HOME.
  static PeerStore Load(const std::string& path = kDefaultPeerStorePath);

  /// Implements §7.1: if peer_id has no recorded pin yet, static_key_b64 is pinned
  /// and persisted. If a pin already exists and matches, this is a no-op. If a pin
  /// already exists and differs, throws PeerKeyMismatchError and leaves the
  /// original pin in place.
  void PinOrVerify(const std::string& peer_id, const std::string& static_key_b64);

  /// Returns the pinned static key (base64) for peer_id, if any.
  std::optional<std::string> Get(const std::string& peer_id) const;

 private:
  struct Entry {
    std::string static_key;
    std::string pinned_at;
  };

  explicit PeerStore(std::string path) : path_(std::move(path)) {}
  void Save() const;

  std::string path_;
  std::unordered_map<std::string, Entry> peers_;
};

}  // namespace relayly
