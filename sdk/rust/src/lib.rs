mod client;
mod crypto;
mod wire;

pub use client::{connect, Client, Message, Messages, Options, PairCode, Peer};
pub use crypto::{generate_key, load_key_from_file, load_or_generate_key, PrivateKey, PublicKey};

use thiserror::Error;

#[derive(Debug, Error)]
pub enum Error {
    #[error("connection error: {0}")]
    Connection(String),
    #[error("auth failed: {0}")]
    Auth(String),
    #[error("no paired peer: {0}")]
    PeerNotFound(String),
    #[error("crypto error: {0}")]
    Crypto(String),
    #[error("client closed")]
    Closed,
    #[error("operation timed out")]
    Timeout,
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}
