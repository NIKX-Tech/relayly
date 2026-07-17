#include "relayly/crypto.hpp"

#include <sodium.h>

#include <filesystem>
#include <fstream>
#include <sstream>

#include "relayly/errors.hpp"

#ifndef _WIN32
#include <sys/stat.h>
#endif

namespace relayly {

namespace {

namespace fs = std::filesystem;

std::string Base64Encode(const std::array<std::uint8_t, 32>& bytes) {
  // sodium_base64_ENCODED_LEN includes the null terminator.
  std::string out(sodium_base64_ENCODED_LEN(bytes.size(), sodium_base64_VARIANT_ORIGINAL), '\0');
  sodium_bin2base64(out.data(), out.size(), bytes.data(), bytes.size(), sodium_base64_VARIANT_ORIGINAL);
  out.resize(out.size() - 1);  // drop the trailing '\0' baked into the buffer size
  return out;
}

std::array<std::uint8_t, 32> Base64Decode(const std::string& b64, ErrorCode error_context) {
  std::array<std::uint8_t, 32> out{};
  std::size_t decoded_len = 0;
  if (sodium_base642bin(out.data(), out.size(), b64.data(), b64.size(), nullptr, &decoded_len, nullptr,
                         sodium_base64_VARIANT_ORIGINAL) != 0 ||
      decoded_len != out.size()) {
    throw Error(error_context, "key must be 32 bytes of base64");
  }
  return out;
}

std::string Trim(const std::string& s) {
  auto begin = s.find_first_not_of(" \t\r\n");
  if (begin == std::string::npos) return "";
  auto end = s.find_last_not_of(" \t\r\n");
  return s.substr(begin, end - begin + 1);
}

}  // namespace

PublicKey PublicKey::FromBase64(const std::string& b64) { return PublicKey(Base64Decode(b64, ErrorCode::kCrypto)); }

std::string PublicKey::ToBase64() const { return Base64Encode(bytes_); }

PrivateKey PrivateKey::Generate() {
  std::array<std::uint8_t, 32> priv{};
  std::array<std::uint8_t, 32> pub{};
  crypto_box_keypair(pub.data(), priv.data());
  return PrivateKey(priv);
}

PrivateKey PrivateKey::FromBase64(const std::string& b64) { return PrivateKey(Base64Decode(b64, ErrorCode::kCrypto)); }

PrivateKey PrivateKey::LoadFromFile(const std::string& path) {
  std::ifstream in(path);
  if (!in) {
    throw Error(ErrorCode::kIo, "failed to open key file: " + path);
  }
  std::ostringstream contents;
  contents << in.rdbuf();
  return FromBase64(Trim(contents.str()));
}

PrivateKey PrivateKey::LoadOrGenerate(const std::string& path) {
  if (fs::exists(path)) {
    return LoadFromFile(path);
  }
  auto key = Generate();
  key.SaveToFile(path);
  return key;
}

PublicKey PrivateKey::GetPublicKey() const {
  std::array<std::uint8_t, 32> pub{};
  crypto_scalarmult_base(pub.data(), bytes_.data());
  return PublicKey(pub);
}

std::string PrivateKey::ToBase64() const { return Base64Encode(bytes_); }

void PrivateKey::SaveToFile(const std::string& path) const {
  fs::path target(path);
  fs::path dir = target.parent_path();
  if (!dir.empty()) {
    fs::create_directories(dir);
#ifndef _WIN32
    ::chmod(dir.c_str(), 0700);
#endif
  }

  std::ofstream out(target, std::ios::trunc);
  if (!out) {
    throw Error(ErrorCode::kIo, "failed to write key file: " + path);
  }
  out << ToBase64() << "\n";
  out.close();
#ifndef _WIN32
  ::chmod(target.c_str(), 0600);
#endif
}

}  // namespace relayly
