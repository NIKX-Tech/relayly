cmake_minimum_required(VERSION 3.20)
project(relayly VERSION 0.1.0 LANGUAGES C CXX)

# std::span/std::byte in the public API (Client::send) are C++20-only; the known
# real-world consumer already builds with C++20. See README.md's "Why C++20?".
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

option(RELAYLY_BUILD_TESTS "Build relayly's unit/integration tests" ON)

# relayly links statically-built dependencies (sodium, nlohmann_json) into a shared
# library (see "Why shared?" below). libsodium-cmake doesn't build its `sodium`
# target with -fPIC itself, which fails at link time on Linux ("recompile with
# -fPIC") — this must be set before FetchContent pulls it in, since it's read at
# the point each target is created.
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

# Unlike Linux/macOS (which locate a shared library via an rpath baked into the
# consuming executable), Windows has no such mechanism - the loader only searches
# the executable's own directory plus PATH. relayly is built SHARED (see "Why
# shared?" below), so without this, relayly-tests.exe/relayly-self-pair-test.exe
# can't find relayly.dll at runtime (fails with STATUS_DLL_NOT_FOUND, 0xc0000135) -
# hit by ctest's own test-discovery step trying to run the test binary to list its
# cases, not just an end-user running it manually. Consolidating every DLL and EXE
# into one output directory is the standard fix; harmless on non-Windows, where
# rpath already makes this a non-issue.
if(WIN32)
  set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin")
endif()

list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/cmake")
include("${CMAKE_CURRENT_SOURCE_DIR}/cmake/Dependencies.cmake")

# SHARED, not the implicit STATIC default: libsodium-cmake's "sodium" target leaks a
# build-tree path into its INTERFACE_INCLUDE_DIRECTORIES, which install(EXPORT)
# rejects outright for a static consumer (it would need sodium re-exported too, and
# sodium isn't export-clean). A shared relayly absorbs its private dependencies
# (sodium, nlohmann_json) into the built library itself, so installed consumers only
# ever need relayly + the public ixwebsocket dependency.
add_library(relayly SHARED
  src/client.cpp
  src/crypto.cpp
  src/peer_store.cpp
  src/peer_conn.cpp
  src/wire.cpp
  src/noise/primitives.cpp
  src/noise/cipher_state.cpp
  src/noise/symmetric_state.cpp
  src/noise/handshake_state.cpp
  src/noise/session.cpp
  third_party/blake2/blake2s-ref.c
)
add_library(relayly::relayly ALIAS relayly)

# A Windows DLL exports nothing by default unless symbols are explicitly annotated
# with __declspec(dllexport) - unlike a Unix .so/.dylib's default-visible symbols,
# which relayly's headers were written assuming. Without this, MSVC's linker
# produces the .dll but no relayly.lib import library, and anything linking against
# the relayly::relayly target (relayly-tests, relayly-self-pair-test, and any real
# consumer) fails with LNK1104. WINDOWS_EXPORT_ALL_SYMBOLS auto-exports every symbol,
# matching Unix behavior instead of auditing every public header for annotations -
# the same workaround IXWebSocket's own CMakeLists.txt already applies to itself for
# exactly this reason. A no-op on non-Windows.
set_target_properties(relayly PROPERTIES WINDOWS_EXPORT_ALL_SYMBOLS ON)

target_include_directories(relayly
  PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:include>
  PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}/src
    ${CMAKE_CURRENT_SOURCE_DIR}/third_party/blake2
)

target_link_libraries(relayly
  PUBLIC
    ixwebsocket::ixwebsocket
  PRIVATE
    sodium
    nlohmann_json::nlohmann_json
)

target_compile_features(relayly PUBLIC cxx_std_20)

if(MSVC)
  target_compile_options(relayly PRIVATE /W4)
  # Never set anywhere in this project before Windows support existed - GCC/Clang
  # always have full exception support enabled, so this had no equivalent need
  # there. Without it, cl.exe compiles try/throw/catch (warning C4530: "unwind
  # semantics are not enabled") but doesn't emit full unwind tables for every
  # frame, which is undefined behavior for any exception that unwinds through
  # such a frame - not hypothetical: this is what a real hang traced back to,
  # in a test whose only distinguishing feature versus its (passing) neighbors
  # is exercising an exception thrown across the relayly.dll/consumer boundary.
  # PUBLIC, not PRIVATE: every consumer (relayly-tests, and any real downstream
  # user like karshipta's gateway) must build with the same exception-handling
  # model relayly.dll itself does, or the same class of problem recurs for them.
  target_compile_options(relayly PUBLIC /EHsc)
else()
  target_compile_options(relayly PRIVATE -Wall -Wextra -Wpedantic)
endif()

# --- Install rules: find_package(relayly CONFIG) --------------------------------
# Windows-excluded, not ported: install(EXPORT ...) requires every target in an
# exported target's link interface to itself be part of an export set. On Windows,
# ixwebsocket links the mbedtls/mbedcrypto/mbedx509 targets fetched in
# Dependencies.cmake (its own TLS backend there), and IXWebSocket's own internal
# install(EXPORT "ixwebsocket" ...) rule hits the exact same completeness check for
# the exact same reason - that one lives in IXWebSocket's own fetched CMakeLists.txt,
# not something patchable here. The real consumer (karshipta's gateway, and this
# SDK's own README's primary documented install method) uses FetchContent /
# add_subdirectory, which never invokes install() at all - only a system-installed
# + find_package(relayly CONFIG) workflow needs this, and that's not exercised by
# anything today. Folding mbedTLS's targets into the same export set (or patching
# IXWebSocket's) is real, separate follow-up work if that workflow is ever needed
# on Windows.
if(NOT WIN32)
  include(GNUInstallDirs)
  include(CMakePackageConfigHelpers)

  install(TARGETS relayly ixwebsocket
    EXPORT relayly-targets
    LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
    ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
    RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
  )
  install(DIRECTORY include/relayly DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
  install(EXPORT relayly-targets
    FILE relayly-targets.cmake
    NAMESPACE relayly::
    DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/relayly
  )

  configure_package_config_file(
    "${CMAKE_CURRENT_SOURCE_DIR}/cmake/relayly-config.cmake.in"
    "${CMAKE_CURRENT_BINARY_DIR}/relayly-config.cmake"
    INSTALL_DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/relayly
  )
  write_basic_package_version_file(
    "${CMAKE_CURRENT_BINARY_DIR}/relayly-config-version.cmake"
    VERSION ${PROJECT_VERSION}
    COMPATIBILITY SameMajorVersion
  )
  install(FILES
    "${CMAKE_CURRENT_BINARY_DIR}/relayly-config.cmake"
    "${CMAKE_CURRENT_BINARY_DIR}/relayly-config-version.cmake"
    DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/relayly
  )
endif()

if(RELAYLY_BUILD_TESTS)
  enable_testing()
  add_subdirectory(tests)
endif()
