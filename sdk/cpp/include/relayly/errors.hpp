#pragma once

#include <stdexcept>
#include <string>
#include <unordered_map>

namespace relayly {

enum class ErrorCode {
  kConnection,
  kAuth,
  kPeerNotFound,
  kCrypto,
  kClosed,
  kTimeout,
  kIo,

  // Relay control-channel error codes (docs/PROTOCOL.md §5.1).
  kInvalidCode,
  kCodeExpired,
  kAlreadyPaired,
  kPeerOffline,
  kRateLimited,
  kMalformed,
  kInternal,
  /// The server-announced-key lock (§7.2) rejecting a different key than the one
  /// already recorded for this device.
  kKeyMismatch,

  // SDK-local errors, not server error codes.
  /// The peer's Noise session isn't up yet. request_pair_code/accept_pair/
  /// PairCode::wait never return until the very first handshake completes, so this
  /// should not occur on a freshly paired peer.
  kNotReady,
  /// The client-side pin check (§7.1, the real security boundary) rejecting a peer
  /// whose authenticated static key differs from the one already pinned for that
  /// peer ID. Unpinning is an explicit user action only; this error is never
  /// auto-retried.
  kPeerKeyMismatch,
};

/// The single error type thrown across relayly's public API, carrying both a typed
/// code (for programmatic handling) and a human-readable message. Mirrors the other
/// four SDKs' one-error-type-per-language shape (sdk/rust's Error enum, sdk/go's
/// typed *Error, sdk/py's exception hierarchy, sdk/ts's RelaylyError).
class Error : public std::runtime_error {
 public:
  Error(ErrorCode code, const std::string& message) : std::runtime_error(message), code_(code) {}

  ErrorCode code() const { return code_; }

 private:
  ErrorCode code_;
};

/// Maps a control-channel error's machine code (docs/PROTOCOL.md §5.1) to a typed
/// Error, wrapped with the human-readable message the server sent.
inline Error ErrorForCode(const std::string& code, const std::string& message) {
  static const std::unordered_map<std::string, ErrorCode> kCodes = {
      {"invalid_code", ErrorCode::kInvalidCode},   {"code_expired", ErrorCode::kCodeExpired},
      {"already_paired", ErrorCode::kAlreadyPaired}, {"peer_offline", ErrorCode::kPeerOffline},
      {"rate_limited", ErrorCode::kRateLimited},   {"malformed", ErrorCode::kMalformed},
      {"key_mismatch", ErrorCode::kKeyMismatch},
  };
  auto it = kCodes.find(code);
  return Error(it != kCodes.end() ? it->second : ErrorCode::kInternal, message);
}

}  // namespace relayly
