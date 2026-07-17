// Self-pair integration test: builds and runs the real cmd/relayly server binary
// (not an in-process test double) and drives two sdk/cpp Clients through it end to
// end — register, connect, pair, a real Noise XX handshake, and bidirectional
// encrypted delivery. This is the "each SDK against itself" leg of the interop
// matrix (docs/tasks/02-sdks-and-interop.md), matching sdk/go's client_test.go,
// sdk/ts's client.test.ts, sdk/py's test_client.py, and sdk/rust's tests/client.rs.
// Writing this test is what caught real wiring bugs in 3 of the 4 previous SDK PRs
// (missing auth query params hanging connect(), an event-loop busy loop on close,
// both pairing sides racing to initiate the handshake) — it exercises exactly the
// paths those bugs lived in. Kept as its own executable (see tests/CMakeLists.txt)
// so `ctest -E self_pair` can skip it when the Go toolchain isn't available.

#include <arpa/inet.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <signal.h>
#include <spawn.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

#include <catch2/catch_test_macros.hpp>
#include <nlohmann/json.hpp>

#include <chrono>
#include <cstdlib>
#include <filesystem>
#include <future>
#include <sstream>
#include <string>
#include <thread>

#include "relayly/client.hpp"
#include "relayly/crypto.hpp"

extern char** environ;

using namespace relayly;
using namespace std::chrono_literals;

namespace {

namespace fs = std::filesystem;

fs::path RepoRoot() {
  // sdk/cpp/tests/self_pair_test.cpp is three levels under the repo root.
  return fs::path(__FILE__).parent_path().parent_path().parent_path().parent_path();
}

int FreePort() {
  int sock = ::socket(AF_INET, SOCK_STREAM, 0);
  REQUIRE(sock >= 0);
  sockaddr_in addr{};
  addr.sin_family = AF_INET;
  addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  addr.sin_port = 0;
  REQUIRE(::bind(sock, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) == 0);
  socklen_t len = sizeof(addr);
  REQUIRE(::getsockname(sock, reinterpret_cast<sockaddr*>(&addr), &len) == 0);
  int port = ntohs(addr.sin_port);
  ::close(sock);
  return port;
}

void BuildServer(const std::string& bin_path) {
  std::string cmd = "cd " + RepoRoot().string() + " && go build -o " + bin_path + " ./cmd/relayly";
  int status = std::system(cmd.c_str());
  REQUIRE(status == 0);
}

/// Minimal raw HTTP/1.1 client — deliberately hand-rolled instead of adding an HTTP
/// client dependency just for these two calls against a local test server (mirrors
/// sdk/rust's tests/client.rs http_request).
struct HttpResponse {
  int status = 0;
  std::string body;
};

HttpResponse HttpRequest(int port, const std::string& method, const std::string& path,
                          const std::string& body = "") {
  int sock = ::socket(AF_INET, SOCK_STREAM, 0);
  sockaddr_in addr{};
  addr.sin_family = AF_INET;
  addr.sin_port = htons(static_cast<std::uint16_t>(port));
  ::inet_pton(AF_INET, "127.0.0.1", &addr.sin_addr);
  if (::connect(sock, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) != 0) {
    ::close(sock);
    return {};
  }

  std::ostringstream req;
  req << method << " " << path << " HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n";
  if (!body.empty()) {
    req << "Content-Type: application/json\r\n";
    req << "Content-Length: " << body.size() << "\r\n";
  }
  req << "\r\n" << body;

  auto req_str = req.str();
  ::send(sock, req_str.data(), req_str.size(), 0);

  std::string response;
  char buf[4096];
  ssize_t n;
  while ((n = ::recv(sock, buf, sizeof(buf), 0)) > 0) {
    response.append(buf, static_cast<std::size_t>(n));
  }
  ::close(sock);

  HttpResponse result;
  auto line_end = response.find("\r\n");
  auto first_space = response.find(' ');
  if (first_space != std::string::npos && line_end != std::string::npos) {
    result.status = std::atoi(response.substr(first_space + 1, 3).c_str());
  }
  auto body_start = response.find("\r\n\r\n");
  if (body_start != std::string::npos) {
    result.body = response.substr(body_start + 4);
  }
  return result;
}

void WaitForHealth(int port, std::chrono::steady_clock::time_point deadline) {
  while (std::chrono::steady_clock::now() < deadline) {
    auto resp = HttpRequest(port, "GET", "/health");
    if (resp.status == 200) return;
    std::this_thread::sleep_for(50ms);
  }
  FAIL("relayly: server did not become healthy in time");
}

struct DeviceCreds {
  std::string device_id;
  std::string device_token;
};

DeviceCreds RegisterDevice(int port, const std::string& name) {
  auto resp = HttpRequest(port, "POST", "/api/v1/devices", R"({"name":")" + name + R"("})");
  REQUIRE(resp.status == 200);
  auto j = nlohmann::json::parse(resp.body);
  return {j.at("device_id").get<std::string>(), j.at("device_token").get<std::string>()};
}

class RunningServer {
 public:
  RunningServer(const std::string& bin_path, int port, const std::string& db_path) : port_(port) {
    std::vector<std::string> arg_storage = {bin_path,     "start", "--host", "127.0.0.1",
                                             "--port",     std::to_string(port),
                                             "--db.path",  db_path};
    std::vector<char*> argv;
    for (auto& a : arg_storage) argv.push_back(a.data());
    argv.push_back(nullptr);

    posix_spawn_file_actions_t actions;
    posix_spawn_file_actions_init(&actions);
    posix_spawn_file_actions_addopen(&actions, STDOUT_FILENO, "/dev/null", O_WRONLY, 0);
    posix_spawn_file_actions_addopen(&actions, STDERR_FILENO, "/dev/null", O_WRONLY, 0);

    int rc = posix_spawn(&pid_, bin_path.c_str(), &actions, nullptr, argv.data(), environ);
    posix_spawn_file_actions_destroy(&actions);
    REQUIRE(rc == 0);

    WaitForHealth(port_, std::chrono::steady_clock::now() + 10s);
  }

  ~RunningServer() {
    if (pid_ > 0) {
      ::kill(pid_, SIGTERM);
      int status = 0;
      ::waitpid(pid_, &status, 0);
    }
  }

  RunningServer(const RunningServer&) = delete;
  RunningServer& operator=(const RunningServer&) = delete;

  std::string WsUrl() const { return "ws://127.0.0.1:" + std::to_string(port_) + "/ws"; }

 private:
  pid_t pid_ = -1;
  int port_;
};

std::vector<std::uint8_t> Str(const std::string& s) { return {s.begin(), s.end()}; }
std::string ToStr(const std::vector<std::uint8_t>& b) { return {b.begin(), b.end()}; }

}  // namespace

TEST_CASE("self_pair: two sdk/cpp clients register, connect, pair, and exchange messages both ways",
          "[self_pair]") {
  auto tmp_dir = fs::temp_directory_path() / ("relayly-cpp-selfpair-" + std::to_string(::getpid()));
  fs::create_directories(tmp_dir);
  auto bin_path = (tmp_dir / "relayly-server").string();
  BuildServer(bin_path);

  int port = FreePort();
  RunningServer server(bin_path, port, (tmp_dir / "relayly.db").string());

  auto dev_a = RegisterDevice(port, "device-a");
  auto dev_b = RegisterDevice(port, "device-b");

  Options opts_a;
  opts_a.device_id = dev_a.device_id;
  opts_a.device_token = dev_a.device_token;
  opts_a.private_key = PrivateKey::Generate();
  opts_a.peer_store_path = (tmp_dir / "peers-a.json").string();

  Options opts_b;
  opts_b.device_id = dev_b.device_id;
  opts_b.device_token = dev_b.device_token;
  opts_b.private_key = PrivateKey::Generate();
  opts_b.peer_store_path = (tmp_dir / "peers-b.json").string();

  std::promise<Message> msg_at_b_promise;
  auto msg_at_b_future = msg_at_b_promise.get_future();
  opts_b.on_message = [&msg_at_b_promise](const Message& m) { msg_at_b_promise.set_value(m); };

  std::promise<Message> msg_at_a_promise;
  auto msg_at_a_future = msg_at_a_promise.get_future();
  opts_a.on_message = [&msg_at_a_promise](const Message& m) { msg_at_a_promise.set_value(m); };

  auto client_a = Client::Connect(server.WsUrl(), opts_a);
  auto client_b = Client::Connect(server.WsUrl(), opts_b);

  auto code = client_a->RequestPairCode();
  REQUIRE(code.short_code().size() == 6);

  auto peer_b_future = client_b->AcceptPair(code.short_code());
  auto peer_a_future = code.wait();

  REQUIRE(peer_b_future.wait_for(10s) == std::future_status::ready);
  REQUIRE(peer_a_future.wait_for(10s) == std::future_status::ready);
  Peer peer_b = peer_b_future.get();
  Peer peer_a = peer_a_future.get();

  REQUIRE(peer_a.id == dev_b.device_id);
  REQUIRE(peer_b.id == dev_a.device_id);

  auto payload_a = Str("hello from A");
  client_a->Send(peer_a.id, std::as_bytes(std::span(payload_a)));
  REQUIRE(msg_at_b_future.wait_for(5s) == std::future_status::ready);
  auto msg_at_b = msg_at_b_future.get();
  REQUIRE(ToStr(msg_at_b.payload) == "hello from A");
  REQUIRE(msg_at_b.from == dev_a.device_id);

  auto payload_b = Str("hello from B");
  client_b->Send(peer_b.id, std::as_bytes(std::span(payload_b)));
  REQUIRE(msg_at_a_future.wait_for(5s) == std::future_status::ready);
  auto msg_at_a = msg_at_a_future.get();
  REQUIRE(ToStr(msg_at_a.payload) == "hello from B");
  REQUIRE(msg_at_a.from == dev_b.device_id);

  client_a->Close();
  client_b->Close();

  std::error_code ec;
  fs::remove_all(tmp_dir, ec);
}
