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
else()
  target_compile_options(relayly PRIVATE -Wall -Wextra -Wpedantic)
endif()

# --- Install rules: find_package(relayly CONFIG) --------------------------------
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

if(RELAYLY_BUILD_TESTS)
  enable_testing()
  add_subdirectory(tests)
endif()
