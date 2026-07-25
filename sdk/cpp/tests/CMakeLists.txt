add_executable(relayly-tests
  test_noise.cpp
  test_peer.cpp
  test_peerstore.cpp
  test_wire.cpp
  test_crypto.cpp
)

target_include_directories(relayly-tests PRIVATE ${CMAKE_SOURCE_DIR}/src)
target_link_libraries(relayly-tests PRIVATE relayly::relayly Catch2::Catch2WithMain)
target_compile_features(relayly-tests PRIVATE cxx_std_20)

include(Catch)
catch_discover_tests(relayly-tests)

# The self-pair integration test builds and runs the real cmd/relayly server binary,
# which needs the Go toolchain and is slower than the unit tests above — kept as its
# own executable/target so `ctest -E self_pair` can skip it when Go isn't available.
#
# Windows-excluded, not just skipped: self_pair_test.cpp itself is written directly
# against POSIX process/socket APIs (posix_spawn, sys/socket.h, sys/wait.h, ...),
# none of which exist on MSVC. That's test-harness code, not relayly's own library
# (which this same fix makes Windows-buildable) - porting the harness to something
# cross-platform (Boost.Process, or a Winsock/CreateProcess branch) is real,
# separate follow-up work, not done here.
if(NOT WIN32)
  add_executable(relayly-self-pair-test self_pair_test.cpp)
  target_include_directories(relayly-self-pair-test PRIVATE ${CMAKE_SOURCE_DIR}/src)
  target_link_libraries(relayly-self-pair-test PRIVATE relayly::relayly Catch2::Catch2WithMain nlohmann_json::nlohmann_json)
  target_compile_features(relayly-self-pair-test PRIVATE cxx_std_20)
  catch_discover_tests(relayly-self-pair-test)
endif()
