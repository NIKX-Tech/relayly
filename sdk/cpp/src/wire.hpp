#pragma once

#include <cstdint>
#include <optional>
#include <string>
#include <vector>

namespace relayly {

/// One entry of welcome's `peers` array (docs/PROTOCOL.md §5.1).
struct WirePeer {
  std::string id;
  std::string static_key;
};

/// The JSON frame exchanged on the control channel (text frames only,
/// docs/PROTOCOL.md §5). Fields are selectively populated depending on `type`;
/// unknown fields on the way in are ignored, and unset fields are omitted going out.
/// Mirrors sdk/rust's WireMessage field-for-field.
struct WireMessage {
  std::string type;

  // welcome
  std::optional<std::uint32_t> protocol_version;
  std::optional<std::string> device_id;
  std::optional<std::vector<WirePeer>> peers;

  // announce_key
  std::optional<std::string> static_key;

  // pair_code / pair_accept / pair_complete; also doubles as error's machine code,
  // matching the server's wire shape.
  std::optional<std::string> code;
  std::optional<std::uint64_t> expires_in;

  // pair_complete (peer_id is reused by peer_status)
  std::optional<std::string> peer_id;
  std::optional<std::string> peer_static_key;

  // peer_status
  std::optional<bool> online;

  // error
  std::optional<std::string> message;

  static WireMessage AnnounceKey(const std::string& key);
  static WireMessage PairRequest();
  static WireMessage PairAccept(const std::string& code);
  static WireMessage Ping();

  /// Serializes to a control-channel JSON text frame, omitting unset fields.
  std::string Encode() const;

  /// Parses a received control-channel text frame. Returns nullopt on malformed
  /// JSON or a missing/non-string `type` field; unknown fields are ignored.
  static std::optional<WireMessage> Decode(const std::string& text);
};

}  // namespace relayly
