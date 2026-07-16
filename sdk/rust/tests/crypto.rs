use relayly::{generate_key, load_key_from_file, load_or_generate_key, PrivateKey, PublicKey};
use tempfile::TempDir;

#[test]
fn generate_key_returns_different_keys() {
    let k1 = generate_key();
    let k2 = generate_key();
    assert_ne!(k1.to_base64(), k2.to_base64());
}

#[test]
fn generated_key_is_32_bytes() {
    let key = generate_key();
    assert_eq!(key.to_bytes().len(), 32);
}

#[test]
fn public_key_derived_from_private() {
    let key = generate_key();
    let pub_key = key.public_key();
    // Base64 of a 32-byte key is 44 chars
    assert_eq!(pub_key.to_base64().len(), 44);
}

#[test]
fn public_key_base64_roundtrip() {
    let key = generate_key();
    let pub1 = key.public_key();
    let pub2 = PublicKey::from_base64(&pub1.to_base64()).unwrap();
    assert_eq!(pub1.to_base64(), pub2.to_base64());
}

#[test]
fn private_key_base64_roundtrip() {
    let key = generate_key();
    let b64 = key.to_base64();
    let restored = PrivateKey::from_base64(&b64).unwrap();
    assert_eq!(key.to_base64(), restored.to_base64());
    assert_eq!(key.to_bytes(), restored.to_bytes());
}

#[test]
fn private_key_from_bytes_derives_same_public_key() {
    let key = generate_key();
    let restored = PrivateKey::from_base64(&key.to_base64()).unwrap();
    assert_eq!(key.public_key().to_base64(), restored.public_key().to_base64());
}

#[test]
fn save_and_load_key_file() {
    let dir = TempDir::new().unwrap();
    let path = dir.path().join("device.key");

    let original = generate_key();
    original.save_to_file(&path).unwrap();

    let loaded = load_key_from_file(&path).unwrap();
    assert_eq!(original.to_base64(), loaded.to_base64());
}

#[cfg(unix)]
#[test]
fn save_to_file_sets_restrictive_permissions() {
    use std::os::unix::fs::PermissionsExt;

    let dir = TempDir::new().unwrap();
    let path = dir.path().join("device.key");
    generate_key().save_to_file(&path).unwrap();

    let mode = std::fs::metadata(&path).unwrap().permissions().mode() & 0o777;
    assert_eq!(mode, 0o600);
}

#[test]
fn load_or_generate_creates_and_persists() {
    let dir = TempDir::new().unwrap();
    let path = dir.path().join("device.key");
    assert!(!path.exists());

    let k1 = load_or_generate_key(&path).unwrap();
    assert!(path.exists());

    let k2 = load_or_generate_key(&path).unwrap();
    assert_eq!(k1.to_base64(), k2.to_base64());
}

#[test]
fn invalid_public_key_length_fails() {
    assert!(PublicKey::from_base64("dG9vc2hvcnQ=").is_err()); // "tooshort" in base64
}
