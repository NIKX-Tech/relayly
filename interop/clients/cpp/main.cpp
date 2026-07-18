// A thin CLI wrapper around sdk/cpp's public API, driven by newline-delimited JSON
// over stdin/stdout. It exists only for the interop harness (interop/harness/) to
// drive real SDK instances as subprocesses — it uses no internal/test-only hooks,
// proving the public API alone is enough for interop testing. The command/event
// vocabulary is defined canonically by interop/clients/go/main.go's doc comment;
// this shim implements the exact same protocol.

#include <nlohmann/json.hpp>

#include <cstdio>
#include <iostream>
#include <mutex>
#include <string>
#include <thread>

#include "relayly/client.hpp"
#include "relayly/crypto.hpp"
#include "relayly/errors.hpp"

using nlohmann::json;
using namespace relayly;

namespace {

std::mutex g_emit_mu;

void Emit(const json& event) {
  std::lock_guard<std::mutex> lock(g_emit_mu);
  std::cout << event.dump() << std::endl;
}

const char kBase64Chars[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

std::string Base64Encode(const std::vector<std::uint8_t>& data) {
  std::string out;
  out.reserve(((data.size() + 2) / 3) * 4);
  std::size_t i = 0;
  while (i + 3 <= data.size()) {
    std::uint32_t n = (static_cast<std::uint32_t>(data[i]) << 16) | (static_cast<std::uint32_t>(data[i + 1]) << 8) |
                       data[i + 2];
    out += kBase64Chars[(n >> 18) & 0x3F];
    out += kBase64Chars[(n >> 12) & 0x3F];
    out += kBase64Chars[(n >> 6) & 0x3F];
    out += kBase64Chars[n & 0x3F];
    i += 3;
  }
  std::size_t remaining = data.size() - i;
  if (remaining == 1) {
    std::uint32_t n = static_cast<std::uint32_t>(data[i]) << 16;
    out += kBase64Chars[(n >> 18) & 0x3F];
    out += kBase64Chars[(n >> 12) & 0x3F];
    out += "==";
  } else if (remaining == 2) {
    std::uint32_t n = (static_cast<std::uint32_t>(data[i]) << 16) | (static_cast<std::uint32_t>(data[i + 1]) << 8);
    out += kBase64Chars[(n >> 18) & 0x3F];
    out += kBase64Chars[(n >> 12) & 0x3F];
    out += kBase64Chars[(n >> 6) & 0x3F];
    out += '=';
  }
  return out;
}

int Base64CharValue(char c) {
  if (c >= 'A' && c <= 'Z') return c - 'A';
  if (c >= 'a' && c <= 'z') return c - 'a' + 26;
  if (c >= '0' && c <= '9') return c - '0' + 52;
  if (c == '+') return 62;
  if (c == '/') return 63;
  return -1;
}

std::vector<std::uint8_t> Base64Decode(const std::string& text) {
  std::vector<std::uint8_t> out;
  int vals[4];
  int count = 0;
  for (char c : text) {
    if (c == '=' || c == '\n' || c == '\r') continue;
    int v = Base64CharValue(c);
    if (v < 0) continue;
    vals[count++] = v;
    if (count == 4) {
      out.push_back(static_cast<std::uint8_t>((vals[0] << 2) | (vals[1] >> 4)));
      out.push_back(static_cast<std::uint8_t>(((vals[1] & 0xF) << 4) | (vals[2] >> 2)));
      out.push_back(static_cast<std::uint8_t>(((vals[2] & 0x3) << 6) | vals[3]));
      count = 0;
    }
  }
  if (count == 2) {
    out.push_back(static_cast<std::uint8_t>((vals[0] << 2) | (vals[1] >> 4)));
  } else if (count == 3) {
    out.push_back(static_cast<std::uint8_t>((vals[0] << 2) | (vals[1] >> 4)));
    out.push_back(static_cast<std::uint8_t>(((vals[1] & 0xF) << 4) | (vals[2] >> 2)));
  }
  return out;
}

void HandleRequestPairCode(Client& client) {
  std::thread([&client]() {
    try {
      auto code = client.RequestPairCode();
      Emit({{"event", "pair_code"}, {"code", code.short_code()}, {"expires_in", code.expires_in()}});
      auto peer = code.wait().get();
      Emit({{"event", "paired"}, {"peer_id", peer.id}, {"peer_public_key_b64", peer.public_key.ToBase64()}});
    } catch (const std::exception& e) {
      Emit({{"event", "pair_error"}, {"message", e.what()}});
    }
  }).detach();
}

void HandleAcceptPair(Client& client, const std::string& code) {
  std::thread([&client, code]() {
    try {
      auto peer = client.AcceptPair(code).get();
      Emit({{"event", "paired"}, {"peer_id", peer.id}, {"peer_public_key_b64", peer.public_key.ToBase64()}});
    } catch (const std::exception& e) {
      Emit({{"event", "pair_error"}, {"message", e.what()}});
    }
  }).detach();
}

void HandleSend(Client& client, const std::string& peer_id, const std::string& payload_b64) {
  std::thread([&client, peer_id, payload_b64]() {
    try {
      auto bytes = Base64Decode(payload_b64);
      client.Send(peer_id, std::as_bytes(std::span(bytes)));
      Emit({{"event", "sent"}});
    } catch (const std::exception& e) {
      Emit({{"event", "send_error"}, {"message", e.what()}});
    }
  }).detach();
}

void HandleCommand(Client& client, const json& cmd) {
  std::string name = cmd.value("cmd", "");
  if (name == "request_pair_code") {
    HandleRequestPairCode(client);
  } else if (name == "accept_pair") {
    HandleAcceptPair(client, cmd.value("code", ""));
  } else if (name == "send") {
    HandleSend(client, cmd.value("peer_id", ""), cmd.value("payload_b64", ""));
  } else if (name == "close") {
    client.Close();
  } else {
    std::cerr << "unknown command: " << name << std::endl;
  }
}

std::string ArgValue(int argc, char** argv, const std::string& flag) {
  for (int i = 1; i < argc - 1; ++i) {
    if (flag == argv[i]) return argv[i + 1];
  }
  return "";
}

}  // namespace

int main(int argc, char** argv) {
  std::string server = ArgValue(argc, argv, "--server");
  std::string device_id = ArgValue(argc, argv, "--device-id");
  std::string device_token = ArgValue(argc, argv, "--device-token");
  std::string peer_store_path = ArgValue(argc, argv, "--peer-store-path");

  Options opts;
  opts.device_id = device_id;
  opts.device_token = device_token;
  opts.private_key = PrivateKey::Generate();
  if (!peer_store_path.empty()) opts.peer_store_path = peer_store_path;
  opts.on_ready = [](const std::string& peer_id) { Emit({{"event", "ready_signal"}, {"peer_id", peer_id}}); };
  opts.on_peer_status = [](const std::string& peer_id, bool online) {
    Emit({{"event", "peer_status"}, {"peer_id", peer_id}, {"online", online}});
  };
  opts.on_message = [](const Message& msg) {
    Emit({{"event", "message"}, {"from", msg.from}, {"payload_b64", Base64Encode(msg.payload)}});
  };

  std::unique_ptr<Client> client;
  try {
    client = Client::Connect(server, opts);
  } catch (const std::exception& e) {
    Emit({{"event", "connect_error"}, {"message", e.what()}});
    return 1;
  }

  Emit({{"event", "ready"}});

  std::string line;
  while (std::getline(std::cin, line)) {
    if (line.empty()) continue;
    json cmd = json::parse(line, nullptr, /*allow_exceptions=*/false);
    if (cmd.is_discarded()) continue;  // skip malformed lines

    HandleCommand(*client, cmd);
    if (cmd.value("cmd", "") == "close") break;
  }

  client->Close();
  return 0;
}
