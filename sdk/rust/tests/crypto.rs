use relayly::{generate_key, load_key_from_file, load_or_generate_key, PrivateKey, PublicKey};
use std::path::Path;
use tempfile::TempDir;

#[test]
fn generate_key_returns_different_keys() {
    let k1 = generate_key();
    let k2 = generate_key();
    assert_ne!(k1.to_base64(), k2.to_base64());
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
}

#[test]
fn encrypt_decrypt_roundtrip() {
    let alice = generate_key();
    let bob = generate_key();

    let plaintext = b"hello from alice";
    let (ct, nonce) = alice.encrypt(plaintext, &bob.public_key()).unwrap();
    let recovered = bob.decrypt(&ct, &nonce, &alice.public_key()).unwrap();
    assert_eq!(recovered, plaintext);
}

#[test]
fn decrypt_wrong_key_fails() {
    let alice = generate_key();
    let bob = generate_key();
    let eve = generate_key();

    let (ct, nonce) = alice.encrypt(b"secret", &bob.public_key()).unwrap();
    assert!(eve.decrypt(&ct, &nonce, &alice.public_key()).is_err());
}

#[test]
fn encrypt_produces_different_nonces() {
    let alice = generate_key();
    let bob = generate_key();
    let (_, n1) = alice.encrypt(b"msg", &bob.public_key()).unwrap();
    let (_, n2) = alice.encrypt(b"msg", &bob.public_key()).unwrap();
    assert_ne!(n1, n2);
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
