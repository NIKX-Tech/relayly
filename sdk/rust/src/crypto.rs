use std::path::Path;

use base64::{engine::general_purpose::STANDARD, Engine};
use crypto_box::{
    aead::{Aead, AeadCore, OsRng},
    PublicKey as CBoxPublicKey, SalsaBox, SecretKey,
};

use crate::Error;

#[derive(Clone, Debug)]
pub struct PublicKey(pub(crate) CBoxPublicKey);

#[derive(Clone)]
pub struct PrivateKey(SecretKey);

impl PrivateKey {
    pub fn generate() -> Self {
        Self(SecretKey::generate(&mut OsRng))
    }

    pub fn public_key(&self) -> PublicKey {
        PublicKey(self.0.public_key())
    }

    pub fn to_base64(&self) -> String {
        STANDARD.encode(self.0.to_bytes())
    }

    pub fn from_base64(s: &str) -> Result<Self, Error> {
        let raw = STANDARD.decode(s).map_err(|e| Error::Crypto(e.to_string()))?;
        let arr: [u8; 32] = raw
            .try_into()
            .map_err(|_| Error::Crypto("private key must be 32 bytes".into()))?;
        Ok(Self(SecretKey::from(arr)))
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

    pub fn encrypt(&self, plaintext: &[u8], recipient: &PublicKey) -> Result<(Vec<u8>, [u8; 24]), Error> {
        let nonce = SalsaBox::generate_nonce(&mut OsRng);
        let b = SalsaBox::new(&recipient.0, &self.0);
        let ct = b.encrypt(&nonce, plaintext).map_err(|e| Error::Crypto(e.to_string()))?;
        let mut nonce_arr = [0u8; 24];
        nonce_arr.copy_from_slice(nonce.as_ref());
        Ok((ct, nonce_arr))
    }

    pub fn decrypt(&self, ciphertext: &[u8], nonce_bytes: &[u8], sender: &PublicKey) -> Result<Vec<u8>, Error> {
        let nonce_arr: [u8; 24] = nonce_bytes
            .try_into()
            .map_err(|_| Error::Crypto("nonce must be 24 bytes".into()))?;
        let b = SalsaBox::new(&sender.0, &self.0);
        b.decrypt(&nonce_arr.into(), ciphertext)
            .map_err(|e| Error::Crypto(e.to_string()))
    }
}

impl PublicKey {
    pub fn to_base64(&self) -> String {
        STANDARD.encode(self.0.as_bytes())
    }

    pub fn from_base64(s: &str) -> Result<Self, Error> {
        let raw = STANDARD.decode(s).map_err(|e| Error::Crypto(e.to_string()))?;
        let arr: [u8; 32] = raw
            .try_into()
            .map_err(|_| Error::Crypto("public key must be 32 bytes".into()))?;
        Ok(Self(CBoxPublicKey::from(arr)))
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
