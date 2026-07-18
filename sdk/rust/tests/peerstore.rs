//! Tests for PeerStore — pinned peer static keys (docs/PROTOCOL.md §7.1), matching
//! sdk/go's peerstore_test.go / sdk/ts's peerStore.test.ts / sdk/py's
//! test_peerstore.py scenarios.

use relayly::PeerStore;
use tempfile::TempDir;

#[test]
fn load_missing_file_starts_empty() {
    let dir = TempDir::new().unwrap();
    let path = dir.path().join("peers.json");
    let store = PeerStore::load(&path).unwrap();
    assert_eq!(store.get("peer-a"), None);
}

#[test]
fn pin_first_seen_key_and_persist() {
    let dir = TempDir::new().unwrap();
    let path = dir.path().join("peers.json");
    let mut store = PeerStore::load(&path).unwrap();

    store.pin_or_verify("peer-a", "aGVsbG8=").unwrap();
    assert_eq!(store.get("peer-a"), Some("aGVsbG8="));
    assert!(path.exists());

    let on_disk: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
    assert_eq!(on_disk["peer-a"]["static_key"], "aGVsbG8=");
    assert!(on_disk["peer-a"]["pinned_at"].is_string());
}

#[test]
fn matching_reannounce_is_a_noop() {
    let dir = TempDir::new().unwrap();
    let path = dir.path().join("peers.json");
    let mut store = PeerStore::load(&path).unwrap();
    store.pin_or_verify("peer-a", "aGVsbG8=").unwrap();
    store.pin_or_verify("peer-a", "aGVsbG8=").unwrap(); // same key again, no error
    assert_eq!(store.get("peer-a"), Some("aGVsbG8="));
}

#[test]
fn mismatch_rejected_original_key_kept() {
    let dir = TempDir::new().unwrap();
    let path = dir.path().join("peers.json");
    let mut store = PeerStore::load(&path).unwrap();
    store.pin_or_verify("peer-a", "aGVsbG8=").unwrap();

    assert!(store.pin_or_verify("peer-a", "d29ybGQ=").is_err());
    assert_eq!(store.get("peer-a"), Some("aGVsbG8=")); // original pin untouched
}

#[test]
fn persists_across_reload() {
    let dir = TempDir::new().unwrap();
    let path = dir.path().join("peers.json");
    let mut store1 = PeerStore::load(&path).unwrap();
    store1.pin_or_verify("peer-a", "aGVsbG8=").unwrap();

    let store2 = PeerStore::load(&path).unwrap();
    assert_eq!(store2.get("peer-a"), Some("aGVsbG8="));
}

#[cfg(unix)]
#[test]
fn save_sets_restrictive_permissions() {
    use std::os::unix::fs::PermissionsExt;

    let dir = TempDir::new().unwrap();
    let path = dir.path().join("peers.json");
    let mut store = PeerStore::load(&path).unwrap();
    store.pin_or_verify("peer-a", "aGVsbG8=").unwrap();

    let mode = std::fs::metadata(&path).unwrap().permissions().mode() & 0o777;
    assert_eq!(mode, 0o600);
}
