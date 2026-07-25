# relayly

C++20 SDK for [Relayly](https://github.com/NIKX-Tech/relayly) — a self-hosted,
end-to-end encrypted WebSocket relay for local-first apps.

Encryption is device-to-device Noise XX (`Noise_XX_25519_ChaChaPoly_BLAKE2s`); the
relay itself holds no key material. See
[`docs/PROTOCOL.md`](https://github.com/NIKX-Tech/relayly/blob/main/docs/PROTOCOL.md)
for the full wire spec.

## Install

CMake, via `FetchContent`:

```cmake
include(FetchContent)
FetchContent_Declare(relayly
  GIT_REPOSITORY https://github.com/NIKX-Tech/relayly.git
  SOURCE_SUBDIR sdk/cpp
  GIT_TAG main
)
FetchContent_MakeAvailable(relayly)

target_link_libraries(your_target PRIVATE relayly::relayly)
```

Or as a git submodule / vendored copy via `add_subdirectory(path/to/relayly/sdk/cpp)`.
`relayly` is built as a shared library — see "Why shared?" below.

Dependencies ([IXWebSocket](https://github.com/machinezone/IXWebSocket),
[libsodium](https://github.com/jedisct1/libsodium) via
[libsodium-cmake](https://github.com/robinlinden/libsodium-cmake),
[nlohmann/json](https://github.com/nlohmann/json)) are fetched automatically; nothing
else needs to be preinstalled beyond a C++20 compiler and OpenSSL (Linux) / Secure
Transport (macOS, built in).

## Quick start

```cpp
#include <relayly/client.hpp>
#include <relayly/crypto.hpp>

using namespace relayly;

int main() {
  auto key = PrivateKey::LoadOrGenerate("~/.relayly/device.key");

  Options opts;
  opts.device_id = "my-laptop";
  opts.device_token = device_token;  // from POST /api/v1/devices
  opts.private_key = key;
  opts.on_message = [](const Message& msg) {
    std::cout << "[" << msg.from << "] "
              << std::string(msg.payload.begin(), msg.payload.end()) << "\n";
  };

  auto client = Client::Connect("wss://relay.example.com/ws", opts);

  // Pair with another device
  auto code = client->RequestPairCode();
  std::cout << "Share this code: " << code.short_code() << "\n";
  auto peer = code.wait().get();  // blocks until the Noise handshake completes

  // Send an encrypted message
  std::string hello = "hello!";
  client->Send(peer.id, std::as_bytes(std::span(hello)));
}
```

## Threading model

All `Options` callbacks (`on_message`, `on_ready`, `on_peer_status`, `on_reconnect`,
`on_disconnect`) fire on IXWebSocket's internal I/O thread — never the thread that
called `Client::Connect`. Keep them short and thread-safe; hand off to your own queue
if you need to do real work. `Client::Send`, `RequestPairCode`, and `AcceptPair` are
safe to call from any thread — IXWebSocket serializes concurrent writes internally.
`RequestPairCode` blocks the calling thread until the server responds with a code (a
prompt round trip); `PairCode::wait()` and `AcceptPair` return a `std::future<Peer>`
instead of blocking, so you choose when (or whether) to wait.

## Pairing

**v1 links exactly one peer per device.** Pairing again replaces whatever was linked
before, it doesn't add a second one alongside it. Multi-peer support is a roadmap
item (`docs/ROADMAP.md`, v0.7). Don't build for N simultaneous peers against this
version.

Devices pair using a short 6-digit code shared out-of-band (or via QR). Both
`AcceptPair()` and `PairCode::wait()` resolve only once the Noise handshake actually
completes (not just the code exchange), so the peer they yield is immediately safe to
`Send()` to.

```cpp
// Device A - request a code
auto code = client->RequestPairCode();
std::cout << "Code: " << code.short_code() << "\n";
std::cout << "QR URL: " << code.QrCodeUrl("wss://relay.example.com") << "\n";

auto peer = code.wait().get();  // blocks until the other device pairs

// Device B - accept the code
auto peer = client->AcceptPair("483921").get();
```

## Peer key pinning

Each peer's authenticated static key is pinned on first pairing and checked on every
handshake after — this pin, not the relay, is the real security boundary
(`docs/PROTOCOL.md` §7). By default it's stored at `~/.relayly/peers.json`, the same
schema every other official SDK reads/writes, so a shared machine can keep one pin
store across languages:

```cpp
opts.peer_store_path = "~/.relayly/peers.json";  // default
```

A peer presenting a different key than its pin throws `relayly::Error` with
`code() == ErrorCode::kPeerKeyMismatch` — this is never auto-retried; unpinning is an
explicit action (remove the entry from the store, or use `relayly::PeerStore`
directly).

## Sending messages

```cpp
std::string hello = "hello!";
client->Send(peer.id, std::as_bytes(std::span(hello)));
```

Throws `relayly::Error` with `code() == ErrorCode::kNotReady` if the peer's session
isn't up yet — in normal use this only happens briefly after a reconnect forces a
re-handshake; use `on_ready` to know when it recovers.

## Reconnection

The client reconnects automatically with exponential backoff (via IXWebSocket's
built-in retry), and re-runs the Noise handshake per `docs/PROTOCOL.md` §6 (the device
with the lexicographically smaller ID re-initiates; the existing session keeps
working until the replacement completes):

```cpp
opts.reconnect_delay = std::chrono::seconds(2);
opts.max_reconnect_delay = std::chrono::seconds(30);
opts.on_disconnect = [](const std::string& reason) { std::cerr << "disconnected: " << reason << "\n"; };
opts.on_reconnect = []() { std::cout << "reconnected\n"; };
opts.on_ready = [](const std::string& peer_id) { std::cout << "session ready with " << peer_id << "\n"; };
opts.on_peer_status = [](const std::string& peer_id, bool online) {
  std::cout << peer_id << " online: " << online << "\n";
};
```

Set `reconnect_delay` to a negative duration to disable automatic reconnection.

## Key management

```cpp
#include <relayly/crypto.hpp>
using namespace relayly;

// Generate a fresh key
auto key = PrivateKey::Generate();

// Save and load
key.SaveToFile("~/.relayly/device.key");
auto loaded = PrivateKey::LoadFromFile("~/.relayly/device.key");

// Load or generate in one call (recommended)
auto key2 = PrivateKey::LoadOrGenerate("~/.relayly/device.key");
```

Key files are plain base64 of the 32 raw private bytes plus a trailing newline, the
same format every other official SDK reads and writes — a key generated by `sdk/go`
loads correctly here and vice versa.

## Options

| Field | Type | Default | Description |
|---|---|---|---|
| `device_id` | `std::string` | - | Unique ID for this device. Required. |
| `device_token` | `std::string` | - | From `POST /api/v1/devices`. Required. |
| `private_key` | `PrivateKey` | - | X25519 private key. Required. |
| `peer_store_path` | `std::string` | `~/.relayly/peers.json` | Pinned peer key storage path. |
| `ping_interval` | `std::chrono::seconds` | `30s` | Keepalive ping interval. |
| `reconnect_delay` | `std::chrono::seconds` | `1s` | Initial reconnect delay. Negative disables. |
| `max_reconnect_delay` | `std::chrono::seconds` | `60s` | Backoff ceiling. |
| `on_disconnect` | `std::function<void(const std::string&)>` | - | Called with reason when connection drops. |
| `on_reconnect` | `std::function<void()>` | - | Called after a successful reconnect. |
| `on_ready` | `std::function<void(const std::string&)>` | - | Called whenever a peer's session becomes usable for `Send()`. |
| `on_peer_status` | `std::function<void(const std::string&, bool)>` | - | Called on the paired peer's online/offline transitions. |
| `on_message` | `std::function<void(const Message&)>` | - | Called for every incoming decrypted message. |

## Why libsodium + a hand-written XX state machine, not noise-c?

`docs/PROTOCOL.md` requires `Noise_XX_25519_ChaChaPoly_BLAKE2s`. `noise-c` supports
this exact suite by name, but its ChaCha20-Poly1305 and BLAKE2 code is a single
maintainer's own from-scratch implementation (documented in-repo as "specific to this
distribution"), not vendored from an audited canonical source — and its last commit
was in 2023. Since libsodium ships ChaCha20-Poly1305 and X25519 but not BLAKE2s, no
path through libsodium alone covers the full suite either way, so this SDK uses
libsodium directly for X25519 and ChaCha20-Poly1305, vendors the *official* BLAKE2
reference implementation (`BLAKE2/BLAKE2`, authored by the algorithm's own designers)
for the hash, and hand-writes the small, spec-defined XX state machine
(`CipherState`/`SymmetricState`/`HandshakeState`) over those two audited primitive
sources — verified byte-for-byte against the same `flynn/noise` reference vectors
already used by `sdk/go`, `sdk/ts`, `sdk/py`, and `sdk/rust`.

## Why shared?

`relayly` is built as a shared library rather than the CMake default of static.
`libsodium-cmake`'s exported `sodium` target isn't itself export-clean (its
`INTERFACE_INCLUDE_DIRECTORIES` leaks a build-tree path), which breaks
`install(EXPORT ...)` for a static consumer that would otherwise need `sodium`
re-exported too. A shared `relayly` absorbs its private dependencies (`sodium`,
`nlohmann_json`) into the built library itself, so installed consumers only ever need
`relayly` plus the public `ixwebsocket` dependency.

## Requirements

- C++20 compiler (GCC 12+, Clang 15+, MSVC 19.3+/Visual Studio 2022, or Apple Clang
  from a recent Xcode)
- CMake 3.20+
- TLS backend: OpenSSL (Linux), Secure Transport (macOS, built in), mbedTLS
  (Windows). OpenSSL must already be present on Linux; mbedTLS is fetched
  automatically on Windows (see `cmake/Dependencies.cmake`) - nothing extra to
  install there either.

Windows note: the self-pair integration test (`relayly-self-pair-test`, exercised
via `ctest`) is excluded from Windows builds - it's written directly against POSIX
process/socket APIs. The library itself and its unit test suite
(`relayly-tests`) build and run on Windows.

## License

MIT
