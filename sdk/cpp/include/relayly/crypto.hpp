#pragma once

#include <array>
#include <cstdint>
#include <string>

namespace relayly {

/// A device's X25519 public key, opaque outside of base64/byte access. Comparable
/// so callers can check an announced key against one they already hold.
class PublicKey {
 public:
  explicit PublicKey(const std::array<std::uint8_t, 32>& bytes) : bytes_(bytes) {}

  /// Throws relayly::Error(kCrypto) if raw isn't valid base64 or isn't 32 bytes.
  static PublicKey FromBase64(const std::string& b64);

  const std::array<std::uint8_t, 32>& bytes() const { return bytes_; }
  std::string ToBase64() const;

  bool operator==(const PublicKey& other) const { return bytes_ == other.bytes_; }
  bool operator!=(const PublicKey& other) const { return !(*this == other); }

 private:
  std::array<std::uint8_t, 32> bytes_;
};

/// A device's long-lived X25519 static private key (docs/PROTOCOL.md §7). This is
/// the identity Noise XX authenticates; losing it means losing every peer pin.
class PrivateKey {
 public:
  /// All-zero placeholder key, not meant for actual Noise operations — exists only
  /// so Options (which embeds a PrivateKey) stays default-constructible for
  /// ergonomic field-by-field initialization. Always overwrite it via Generate()/
  /// FromBase64()/LoadFromFile()/LoadOrGenerate() before connecting.
  PrivateKey() : bytes_{} {}

  static PrivateKey Generate();

  /// Throws relayly::Error(kCrypto) if b64 isn't valid base64 or isn't 32 bytes.
  static PrivateKey FromBase64(const std::string& b64);

  /// Reads path (trimmed base64, one line), throwing relayly::Error(kIo)/(kCrypto)
  /// on read failure or malformed contents.
  static PrivateKey LoadFromFile(const std::string& path);

  /// Loads path if it exists, otherwise generates a fresh key and saves it there
  /// first (creating parent directories, permissions 0600) — the common
  /// first-run-vs-later-runs pattern every SDK's Options resolves to internally.
  static PrivateKey LoadOrGenerate(const std::string& path);

  PublicKey GetPublicKey() const;
  const std::array<std::uint8_t, 32>& bytes() const { return bytes_; }
  std::string ToBase64() const;

  /// Writes path as trimmed base64 + newline, permissions 0600, creating parent
  /// directories (permissions 0700) if needed. Throws relayly::Error(kIo) on
  /// failure.
  void SaveToFile(const std::string& path) const;

 private:
  explicit PrivateKey(const std::array<std::uint8_t, 32>& bytes) : bytes_(bytes) {}

  std::array<std::uint8_t, 32> bytes_;
};

inline PrivateKey GenerateKey() { return PrivateKey::Generate(); }
inline PrivateKey LoadKeyFromFile(const std::string& path) { return PrivateKey::LoadFromFile(path); }
inline PrivateKey LoadOrGenerateKey(const std::string& path) { return PrivateKey::LoadOrGenerate(path); }

}  // namespace relayly
