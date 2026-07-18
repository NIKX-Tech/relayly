use std::path::Path;

use base64::{engine::general_purpose::STANDARD, Engine};
use x25519_dalek::{PublicKey as XPublicKey, StaticSecret};

use crate::Error;

#[derive(Clone, Debug)]
pub struct PublicKey(pub(crate) XPublicKey);

#[derive(Clone)]
pub struct PrivateKey(StaticSecret);

impl PrivateKey {
    pub fn generate() -> Self {
        Self(StaticSecret::random())
    }

    pub fn public_key(&self) -> PublicKey {
        PublicKey(XPublicKey::from(&self.0))
    }

    pub fn to_bytes(&self) -> [u8; 32] {
        self.0.to_bytes()
    }

    pub fn to_base64(&self) -> String {
        STANDARD.encode(self.0.to_bytes())
    }

    pub fn from_base64(s: &str) -> Result<Self, Error> {
        let raw = STANDARD.decode(s).map_err(|e| Error::Crypto(e.to_string()))?;
        let arr: [u8; 32] = raw
            .try_into()
            .map_err(|_| Error::Crypto("private key must be 32 bytes".into()))?;
        Ok(Self(StaticSecret::from(arr)))
    }

    pub fn save_to_file(&self, path: &Path) -> Result<(), Error> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        std::fs::write(path, format!("{}\n", self.to_base64()))?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))?;
        }
        Ok(())
    }

    pub fn load_from_file(path: &Path) -> Result<Self, Error> {
        let s = std::fs::read_to_string(path)?;
        Self::from_base64(s.trim())
    }

    pub fn load_or_generate(path: &Path) -> Result<Self, Error> {
        if path.exists() {
            return Self::load_from_file(path);
        }
        let key = Self::generate();
        key.save_to_file(path)?;
        Ok(key)
    }
}

impl PublicKey {
    pub fn to_bytes(&self) -> [u8; 32] {
        self.0.to_bytes()
    }

    pub fn to_base64(&self) -> String {
        STANDARD.encode(self.0.as_bytes())
    }

    pub fn from_base64(s: &str) -> Result<Self, Error> {
        let raw = STANDARD.decode(s).map_err(|e| Error::Crypto(e.to_string()))?;
        Self::from_bytes(&raw)
    }

    pub fn from_bytes(raw: &[u8]) -> Result<Self, Error> {
        let arr: [u8; 32] = raw
            .try_into()
            .map_err(|_| Error::Crypto("public key must be 32 bytes".into()))?;
        Ok(Self(XPublicKey::from(arr)))
    }
}

impl PartialEq for PublicKey {
    fn eq(&self, other: &Self) -> bool {
        self.0.as_bytes() == other.0.as_bytes()
    }
}

pub fn generate_key() -> PrivateKey {
    PrivateKey::generate()
}

pub fn load_key_from_file(path: &Path) -> Result<PrivateKey, Error> {
    PrivateKey::load_from_file(path)
}

pub fn load_or_generate_key(path: &Path) -> Result<PrivateKey, Error> {
    PrivateKey::load_or_generate(path)
}
