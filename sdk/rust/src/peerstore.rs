//! Persists pinned peer static keys (docs/PROTOCOL.md §7.1): the client-side pin is
//! the real security boundary. A peer's key is pinned the first time its Noise
//! handshake completes; any later handshake presenting a different key for the same
//! peer ID hard-fails with `Error::PeerKeyMismatch`. Unpinning is never automatic.
//!
//! Schema is shared byte-for-byte across every official SDK (docs/tasks/
//! 02-sdks-and-interop.md), so the same file can in principle be read/written by
//! clients written in different languages on the same machine.

use std::{
    collections::HashMap,
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};

use serde::{Deserialize, Serialize};

use crate::Error;

pub const DEFAULT_PEER_STORE_PATH: &str = "~/.relayly/peers.json";

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PinnedPeer {
    static_key: String,
    pinned_at: String,
}

pub struct PeerStore {
    path: PathBuf,
    peers: HashMap<String, PinnedPeer>,
}

impl PeerStore {
    /// Loads the peer store at path, creating an empty one in memory if the file
    /// doesn't exist yet (it is created on first successful pin).
    pub fn load(path: &Path) -> Result<Self, Error> {
        let path = expand_home(path);
        match std::fs::read_to_string(&path) {
            Ok(data) if data.trim().is_empty() => Ok(Self { path, peers: HashMap::new() }),
            Ok(data) => {
                let peers: HashMap<String, PinnedPeer> =
                    serde_json::from_str(&data).map_err(|e| Error::Crypto(format!("invalid peer store: {e}")))?;
                Ok(Self { path, peers })
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Self { path, peers: HashMap::new() }),
            Err(e) => Err(Error::Io(e)),
        }
    }

    /// Implements §7.1: if peer_id has no recorded pin yet, static_key_b64 is pinned
    /// and persisted. If a pin already exists and matches, this is a no-op. If a pin
    /// already exists and differs, returns `Error::PeerKeyMismatch` and leaves the
    /// original pin in place.
    pub fn pin_or_verify(&mut self, peer_id: &str, static_key_b64: &str) -> Result<(), Error> {
        if let Some(existing) = self.peers.get(peer_id) {
            if existing.static_key != static_key_b64 {
                return Err(Error::PeerKeyMismatch(peer_id.to_string()));
            }
            return Ok(());
        }

        self.peers.insert(
            peer_id.to_string(),
            PinnedPeer { static_key: static_key_b64.to_string(), pinned_at: now_rfc3339() },
        );
        self.save()
    }

    /// Returns the pinned static key (base64) for peer_id, if any.
    pub fn get(&self, peer_id: &str) -> Option<&str> {
        self.peers.get(peer_id).map(|p| p.static_key.as_str())
    }

    fn save(&self) -> Result<(), Error> {
        if let Some(parent) = self.path.parent() {
            std::fs::create_dir_all(parent)?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                std::fs::set_permissions(parent, std::fs::Permissions::from_mode(0o700))?;
            }
        }

        let data = serde_json::to_string_pretty(&self.peers)
            .map_err(|e| Error::Crypto(format!("encoding peer store: {e}")))?;
        let tmp = self.path.with_extension("json.tmp");
        std::fs::write(&tmp, data)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&tmp, std::fs::Permissions::from_mode(0o600))?;
        }
        std::fs::rename(&tmp, &self.path)?;
        Ok(())
    }
}

fn expand_home(path: &Path) -> PathBuf {
    let Some(s) = path.to_str() else { return path.to_path_buf() };
    if let Some(rest) = s.strip_prefix("~/") {
        if let Ok(home) = std::env::var("HOME") {
            return PathBuf::from(home).join(rest);
        }
    }
    path.to_path_buf()
}

pub(crate) fn now_rfc3339() -> String {
    let secs = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_secs();
    // Manual UTC calendar conversion avoids pulling in a dedicated date/time crate for
    // a single timestamp field; matches the plain "YYYY-MM-DDTHH:MM:SSZ" shape the
    // other SDKs already write (no fractional seconds, no offset besides Z).
    let days = secs / 86400;
    let time_of_day = secs % 86400;
    let (hour, minute, second) = (time_of_day / 3600, (time_of_day % 3600) / 60, time_of_day % 60);

    let (year, month, day) = civil_from_days(days as i64);
    format!("{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:{second:02}Z")
}

/// Converts a day count since the Unix epoch (1970-01-01) to a (year, month, day)
/// civil calendar date, using Howard Hinnant's well-known constant-time algorithm
/// (proleptic Gregorian calendar, valid for the entire range we'll ever see here).
fn civil_from_days(z: i64) -> (i64, u32, u32) {
    let z = z + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    (if m <= 2 { y + 1 } else { y }, m, d)
}
