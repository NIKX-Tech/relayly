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

# --- IXWebSocket (WebSocket client, runs its own I/O thread) --------------------
set(USE_TLS ON CACHE BOOL "" FORCE)
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
