#include "wire.hpp"

#include <nlohmann/json.hpp>

namespace relayly {

using nlohmann::json;

WireMessage WireMessage::AnnounceKey(const std::string& key) {
  WireMessage m;
  m.type = "announce_key";
  m.static_key = key;
  return m;
}

WireMessage WireMessage::PairRequest() {
  WireMessage m;
  m.type = "pair_request";
  return m;
}

WireMessage WireMessage::PairAccept(const std::string& code) {
  WireMessage m;
  m.type = "pair_accept";
  m.code = code;
  return m;
}

WireMessage WireMessage::Ping() {
  WireMessage m;
  m.type = "ping";
  return m;
}

std::string WireMessage::Encode() const {
  json j;
  j["type"] = type;

  if (protocol_version) j["protocol_version"] = *protocol_version;
  if (device_id) j["device_id"] = *device_id;
  if (peers) {
    auto arr = json::array();
    for (const auto& p : *peers) {
      arr.push_back({{"id", p.id}, {"static_key", p.static_key}});
    }
    j["peers"] = std::move(arr);
  }
  if (static_key) j["static_key"] = *static_key;
  if (code) j["code"] = *code;
  if (expires_in) j["expires_in"] = *expires_in;
  if (peer_id) j["peer_id"] = *peer_id;
  if (peer_static_key) j["peer_static_key"] = *peer_static_key;
  if (online) j["online"] = *online;
  if (message) j["message"] = *message;

  return j.dump();
}

std::optional<WireMessage> WireMessage::Decode(const std::string& text) {
  json j = json::parse(text, nullptr, /*allow_exceptions=*/false);
  if (j.is_discarded() || !j.is_object()) return std::nullopt;

  auto type_it = j.find("type");
  if (type_it == j.end() || !type_it->is_string()) return std::nullopt;

  WireMessage m;
  m.type = type_it->get<std::string>();

  if (auto it = j.find("protocol_version"); it != j.end() && it->is_number_unsigned()) {
    m.protocol_version = it->get<std::uint32_t>();
  }
  if (auto it = j.find("device_id"); it != j.end() && it->is_string()) {
    m.device_id = it->get<std::string>();
  }
  if (auto it = j.find("peers"); it != j.end() && it->is_array()) {
    std::vector<WirePeer> peer_list;
    for (const auto& entry : *it) {
      if (!entry.is_object()) continue;
      WirePeer p;
      p.id = entry.value("id", "");
      p.static_key = entry.value("static_key", "");
      peer_list.push_back(std::move(p));
    }
    m.peers = std::move(peer_list);
  }
  if (auto it = j.find("static_key"); it != j.end() && it->is_string()) {
    m.static_key = it->get<std::string>();
  }
  if (auto it = j.find("code"); it != j.end() && it->is_string()) {
    m.code = it->get<std::string>();
  }
  if (auto it = j.find("expires_in"); it != j.end() && it->is_number_unsigned()) {
    m.expires_in = it->get<std::uint64_t>();
  }
  if (auto it = j.find("peer_id"); it != j.end() && it->is_string()) {
    m.peer_id = it->get<std::string>();
  }
  if (auto it = j.find("peer_static_key"); it != j.end() && it->is_string()) {
    m.peer_static_key = it->get<std::string>();
  }
  if (auto it = j.find("online"); it != j.end() && it->is_boolean()) {
    m.online = it->get<bool>();
  }
  if (auto it = j.find("message"); it != j.end() && it->is_string()) {
    m.message = it->get<std::string>();
  }

  return m;
}

}  // namespace relayly
