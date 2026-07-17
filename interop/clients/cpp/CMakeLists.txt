cmake_minimum_required(VERSION 3.20)
project(relayly-interop-shim-cpp LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Skip sdk/cpp's own unit/self-pair test suite (and the Catch2 fetch it needs) —
# this build only wants the relayly library itself.
set(RELAYLY_BUILD_TESTS OFF CACHE BOOL "" FORCE)
add_subdirectory(${CMAKE_CURRENT_SOURCE_DIR}/../../../sdk/cpp relayly-sdk)

add_executable(relayly-interop-shim-cpp main.cpp)
target_link_libraries(relayly-interop-shim-cpp PRIVATE relayly::relayly nlohmann_json::nlohmann_json)
target_compile_features(relayly-interop-shim-cpp PRIVATE cxx_std_20)
