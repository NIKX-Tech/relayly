#include "relayly/peer_store.hpp"

#include <nlohmann/json.hpp>

#include <chrono>
#include <cstdlib>
#include <ctime>
#include <filesystem>
#include <fstream>

#ifndef _WIN32
#include <sys/stat.h>
#endif

namespace relayly {

namespace {

namespace fs = std::filesystem;
using nlohmann::json;

std::string ExpandHome(const std::string& path) {
  if (path.empty() || path[0] != '~') return path;
  const char* home = std::getenv("HOME");
  if (home == nullptr) return path;
  return std::string(home) + path.substr(1);
}

// RFC 3339 UTC, matching the timestamp format the other four SDKs already write
// into the shared on-disk peers.json schema.
std::string NowRfc3339Utc() {
  auto now = std::chrono::system_clock::now();
  std::time_t t = std::chrono::system_clock::to_time_t(now);
  std::tm tm{};
  gmtime_r(&t, &tm);
  char buf[32];
  std::strftime(buf, sizeof(buf), "%Y-%m-%dT%H:%M:%SZ", &tm);
  return std::string(buf);
}

}  // namespace

PeerStore PeerStore::Load(const std::string& path) {
  PeerStore store(ExpandHome(path));

  std::ifstream in(store.path_);
  if (!in) {
    return store;  // not created yet: it's written lazily on the first pin
  }

  json doc;
  in >> doc;
  for (const auto& [peer_id, value] : doc.items()) {
    Entry entry;
    entry.static_key = value.value("static_key", "");
    entry.pinned_at = value.value("pinned_at", "");
    store.peers_.emplace(peer_id, std::move(entry));
  }
  return store;
}

void PeerStore::PinOrVerify(const std::string& peer_id, const std::string& static_key_b64) {
  auto it = peers_.find(peer_id);
  if (it != peers_.end()) {
    if (it->second.static_key != static_key_b64) {
      throw PeerKeyMismatchError(peer_id);
    }
    return;
  }

  Entry entry;
  entry.static_key = static_key_b64;
  entry.pinned_at = NowRfc3339Utc();
  peers_.emplace(peer_id, std::move(entry));
  Save();
}

std::optional<std::string> PeerStore::Get(const std::string& peer_id) const {
  auto it = peers_.find(peer_id);
  if (it == peers_.end()) return std::nullopt;
  return it->second.static_key;
}

void PeerStore::Save() const {
  fs::path target(path_);
  fs::path dir = target.parent_path();
  if (!dir.empty()) {
    fs::create_directories(dir);
#ifndef _WIN32
    ::chmod(dir.c_str(), 0700);
#endif
  }

  json doc = json::object();
  for (const auto& [peer_id, entry] : peers_) {
    doc[peer_id] = {{"static_key", entry.static_key}, {"pinned_at", entry.pinned_at}};
  }

  // Atomic write: write to a sibling temp file, then rename over the target, so a
  // crash mid-save never leaves a truncated peers.json behind.
  fs::path tmp = target;
  tmp += ".tmp";
  {
    std::ofstream out(tmp, std::ios::trunc);
    out << doc.dump(2);
  }
#ifndef _WIN32
  ::chmod(tmp.c_str(), 0600);
#endif
  fs::rename(tmp, target);
}

}  // namespace relayly
