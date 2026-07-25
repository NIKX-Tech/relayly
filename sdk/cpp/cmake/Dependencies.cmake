# Third-party dependencies, fetched via FetchContent so the whole SDK builds cleanly
# from source with nothing but a C/C++ toolchain and CMake — no system package
# manager required (though system libsodium/OpenSSL are also fine if already present;
# FetchContent only builds what isn't already satisfied).
#
# See sdk/cpp/README.md's "Why libsodium + hand-written XX, not noise-c?" section for
# the crypto library reasoning; this file just pins versions.

include(FetchContent)

# --- libsodium (X25519, ChaCha20-Poly1305) --------------------------------------
# Upstream libsodium is autotools-based; libsodium-cmake is a maintained wrapper
# giving it proper FetchContent/CMake integration.
FetchContent_Declare(
  libsodium-cmake
  GIT_REPOSITORY https://github.com/robinlinden/libsodium-cmake.git
  GIT_TAG 9b2848dfc1b917a9410f0de9d81059b26cbfaa8d
)
set(SODIUM_DISABLE_TESTS ON CACHE BOOL "" FORCE)
FetchContent_MakeAvailable(libsodium-cmake)

# --- mbedTLS (Windows-only TLS backend for IXWebSocket below) -------------------
# IXWebSocket defaults to Secure Transport on Apple and OpenSSL elsewhere, but to
# mbedTLS on Windows (its own CMakeLists.txt: "default to mbedtls on windows if
# nothing is configured") - unlike those two, mbedTLS isn't preinstalled on a stock
# Windows box or a GitHub Actions windows-latest runner, and IXWebSocket only ever
# looks for it via find_package(MbedTLS REQUIRED), never fetches it itself. Fetching
# it here and hand-setting the MBEDTLS_FOUND/MBEDTLS_INCLUDE_DIRS/MBEDTLS_LIBRARIES
# variables IXWebSocket's own find_package(MbedTLS) check looks for (guarded by
# `if (NOT MBEDTLS_FOUND)`) keeps the "nothing preinstalled but a compiler and CMake"
# promise this file makes on every other platform.
if(WIN32)
  FetchContent_Declare(
    mbedtls
    GIT_REPOSITORY https://github.com/Mbed-TLS/mbedtls.git
    GIT_TAG v3.6.2
  )
  set(ENABLE_TESTING OFF CACHE BOOL "" FORCE)
  set(ENABLE_PROGRAMS OFF CACHE BOOL "" FORCE)
  FetchContent_MakeAvailable(mbedtls)
  set(MBEDTLS_FOUND TRUE)
  set(MBEDTLS_INCLUDE_DIRS "${mbedtls_SOURCE_DIR}/include")
  set(MBEDTLS_LIBRARIES mbedtls mbedcrypto mbedx509)
endif()

# --- IXWebSocket (WebSocket client, runs its own I/O thread) --------------------
set(USE_TLS ON CACHE BOOL "" FORCE)
# IXWebSocket's own USE_ZLIB option defaults on unconditionally (permessage-deflate
# WebSocket compression) and does a hard find_package(ZLIB REQUIRED) when it is -
# satisfied on Linux/macOS by the OS/SDK's own libz, but not preinstalled on
# Windows. relayly's own wire protocol (a JSON control channel plus binary Noise
# envelopes, see docs/PROTOCOL.md) doesn't depend on WebSocket-layer compression,
# so disabling it on Windows only - rather than also fetching zlib - keeps this
# fix scoped to what's actually needed rather than growing the dependency tree
# for an optional feature this SDK doesn't use.
if(WIN32)
  set(USE_ZLIB OFF CACHE BOOL "" FORCE)
endif()
FetchContent_Declare(
  ixwebsocket
  GIT_REPOSITORY https://github.com/machinezone/IXWebSocket.git
  GIT_TAG v12.0.1
)
FetchContent_MakeAvailable(ixwebsocket)

# --- nlohmann/json (control channel) --------------------------------------------
FetchContent_Declare(
  nlohmann_json
  GIT_REPOSITORY https://github.com/nlohmann/json.git
  GIT_TAG v3.12.0
)
FetchContent_MakeAvailable(nlohmann_json)

# --- Catch2 (tests only) ---------------------------------------------------------
if(RELAYLY_BUILD_TESTS)
  FetchContent_Declare(
    Catch2
    GIT_REPOSITORY https://github.com/catchorg/Catch2.git
    GIT_TAG v3.15.2
  )
  FetchContent_MakeAvailable(Catch2)
  list(APPEND CMAKE_MODULE_PATH "${catch2_SOURCE_DIR}/extras")
endif()
