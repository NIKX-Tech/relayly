//! Device-to-device Noise XX handshake and transport encryption
//! (Noise_XX_25519_ChaChaPoly_BLAKE2s, docs/PROTOCOL.md §6), driven through the
//! `snow` crate — actively maintained, backed by RustCrypto's audited
//! curve25519-dalek/chacha20poly1305/blake2 crates, and verified byte-for-byte
//! against the same flynn/noise reference vectors used in sdk/ts/sdk/py (see the
//! `tests` module below — `NoiseSession`/`HandshakeState` are crate-private, so this
//! lives as an in-module unit test rather than an integration test under `tests/`).

use snow::{Builder, HandshakeState, TransportState};

use crate::{crypto::PrivateKey, Error};

const NOISE_PARAMS: &str = "Noise_XX_25519_ChaChaPoly_BLAKE2s";

// E2E envelope types (docs/PROTOCOL.md §6): binary WebSocket frames are one byte of
// envelope type followed by the Noise message or transport ciphertext.
pub const ENVELOPE_HANDSHAKE: u8 = 0x01;
pub const ENVELOPE_TRANSPORT: u8 = 0x02;

pub fn encode_envelope(kind: u8, payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(1 + payload.len());
    out.push(kind);
    out.extend_from_slice(payload);
    out
}

pub fn decode_envelope(frame: &[u8]) -> Option<(u8, &[u8])> {
    frame.split_first().map(|(kind, payload)| (*kind, payload))
}

enum State {
    Handshaking(Box<HandshakeState>),
    Ready { transport: Box<TransportState>, peer_static: Vec<u8> },
    Failed,
}

/// Drives exactly one Noise XX handshake, as either role, and once it completes,
/// encrypts/decrypts transport messages for that session. Does not itself implement
/// the make-before-break replacement policy — see peer.rs for the wrapper that
/// decides when a new NoiseSession may replace an existing one.
pub struct NoiseSession {
    state: State,
}

impl NoiseSession {
    fn params() -> snow::params::NoiseParams {
        NOISE_PARAMS.parse().expect("valid noise params")
    }

    /// Starts a handshake as the Noise initiator and returns (session, msg1) — msg1 is
    /// the first message to send as an ENVELOPE_HANDSHAKE frame.
    pub fn as_initiator(private_key: &PrivateKey) -> Result<(Self, Vec<u8>), Error> {
        let key_bytes = private_key.to_bytes();
        let mut hs = Builder::new(Self::params())
            .local_private_key(&key_bytes)
            .build_initiator()
            .map_err(|e| Error::Crypto(e.to_string()))?;
        let mut buf = [0u8; 256];
        let len = hs.write_message(&[], &mut buf).map_err(|e| Error::Crypto(e.to_string()))?;
        Ok((Self { state: State::Handshaking(Box::new(hs)) }, buf[..len].to_vec()))
    }

    /// Starts a handshake as the Noise responder, ready to receive msg1.
    pub fn as_responder(private_key: &PrivateKey) -> Result<Self, Error> {
        let key_bytes = private_key.to_bytes();
        let hs = Builder::new(Self::params())
            .local_private_key(&key_bytes)
            .build_responder()
            .map_err(|e| Error::Crypto(e.to_string()))?;
        Ok(Self { state: State::Handshaking(Box::new(hs)) })
    }

    /// Feeds one received ENVELOPE_HANDSHAKE payload into the state machine. Returns
    /// (reply, done): reply is Some when a response message must be sent back; done
    /// is true once the handshake has completed (successfully or not) — check
    /// `failed()` in that case.
    pub fn handle_handshake_message(&mut self, data: &[u8]) -> (Option<Vec<u8>>, bool) {
        let State::Handshaking(hs) = &mut self.state else {
            self.state = State::Failed;
            return (None, true);
        };

        let mut rbuf = [0u8; 256];
        if hs.read_message(data, &mut rbuf).is_err() {
            self.state = State::Failed;
            return (None, true);
        }

        if hs.is_handshake_finished() {
            return self.finish(None);
        }

        let mut wbuf = [0u8; 256];
        match hs.write_message(&[], &mut wbuf) {
            Ok(len) => {
                let reply = wbuf[..len].to_vec();
                if hs.is_handshake_finished() {
                    self.finish(Some(reply))
                } else {
                    (Some(reply), false)
                }
            }
            Err(_) => {
                self.state = State::Failed;
                (None, true)
            }
        }
    }

    fn finish(&mut self, reply: Option<Vec<u8>>) -> (Option<Vec<u8>>, bool) {
        let State::Handshaking(hs) = std::mem::replace(&mut self.state, State::Failed) else {
            unreachable!()
        };
        let peer_static = match hs.get_remote_static() {
            Some(k) => k.to_vec(),
            None => return (None, true), // state stays Failed
        };
        match hs.into_transport_mode() {
            Ok(transport) => {
                self.state = State::Ready { transport: Box::new(transport), peer_static };
                (reply, true)
            }
            Err(_) => (None, true), // state stays Failed
        }
    }

    pub fn ready(&self) -> bool {
        matches!(self.state, State::Ready { .. })
    }

    pub fn failed(&self) -> bool {
        matches!(self.state, State::Failed)
    }

    /// The authenticated peer static key. Only valid once `ready()` is true.
    pub fn peer_static_key(&self) -> Option<&[u8]> {
        match &self.state {
            State::Ready { peer_static, .. } => Some(peer_static),
            _ => None,
        }
    }

    pub fn encrypt(&mut self, plaintext: &[u8]) -> Result<Vec<u8>, Error> {
        let State::Ready { transport, .. } = &mut self.state else {
            return Err(Error::NotReady);
        };
        let mut buf = vec![0u8; plaintext.len() + 16];
        let len = transport.write_message(plaintext, &mut buf).map_err(|e| Error::Crypto(e.to_string()))?;
        buf.truncate(len);
        Ok(buf)
    }

    pub fn decrypt(&mut self, ciphertext: &[u8]) -> Result<Vec<u8>, Error> {
        let State::Ready { transport, .. } = &mut self.state else {
            return Err(Error::NotReady);
        };
        let mut buf = vec![0u8; ciphertext.len()];
        let len = transport.read_message(ciphertext, &mut buf).map_err(|e| Error::Crypto(e.to_string()))?;
        buf.truncate(len);
        Ok(buf)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // flynn/noise cross-validation vectors — the same fixed keys/deterministic
    // ephemeral bytes already embedded in sdk/ts/src/noise/noise.test.ts and
    // sdk/py/tests/test_noise.py, generated with a standalone Go program using
    // flynn/noise (already proven server-side and in sdk/go). This is the actual
    // correctness gate for using `snow` here, not "the crate claims to support our
    // suite": Noise's AEAD auth means a subtle mismatch fails loudly (decrypt/MAC
    // failure), not silently.
    const A_STATIC_PRIVATE: &str = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20";
    const B_STATIC_PRIVATE: &str = "101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f";
    const A_STATIC_PUBLIC: &str = "07a37cbc142093c8b755dc1b10e86cb426374ad16aa853ed0bdfc0b2b86d1c7c";
    const B_STATIC_PUBLIC: &str = "d89e3bad79437dbed9f843418304f460ff05c7fe81fe4a9577a804cb9367ff66";
    const MSG1: &str = "358072d6365880d1aeea329adf9121383851ed21a28e3b75e965d0d2cd166254";
    const MSG2: &str = "34e42d4af5ef94a07a3a84201b889d4cd1a743cb27b11b6a10438a8feb8e5847ee0b2fa3bbca43904cbf6186d5e09fe67128c94cc3e3da6d35bf21f0358c487d5300c27a709ae1da5b4951c9eb1f0afd64e57891c7894b617293b07c9a455311";
    const MSG3: &str = "b8312f344cb91f060c34ae9a514f48981b3316af898179729fd217b843cf0f75b07d427b956b287b149ee47a4b0b71e3b822b0f15bc616ce52af8a3dbeab8bc8";
    const CT_A_TO_B_1: &str = "a21eb0be51f6230018b2a51f1b501eb2885cf12b23e6351f1a577c43";
    const CT_B_TO_A_1: &str = "362c3040c6440177f0d09b74b5457be4fc12cc30733563aa87dc83b9";

    fn hex(s: &str) -> Vec<u8> {
        (0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap()).collect()
    }
    fn to_hex(b: &[u8]) -> String {
        b.iter().map(|x| format!("{x:02x}")).collect()
    }
    fn det_bytes(seed: u8, n: usize) -> Vec<u8> {
        (0..n).map(|i| seed.wrapping_add(i as u8)).collect()
    }

    #[test]
    fn handshake_messages_match_flynn_noise_vectors() {
        let a_static = hex(A_STATIC_PRIVATE);
        let b_static = hex(B_STATIC_PRIVATE);
        let a_eph = det_bytes(0x20, 32);
        let b_eph = det_bytes(0x30, 32);

        let mut initiator = Builder::new(NoiseSession::params())
            .local_private_key(&a_static)
            .fixed_ephemeral_key_for_testing_only(&a_eph)
            .build_initiator()
            .unwrap();
        let mut responder = Builder::new(NoiseSession::params())
            .local_private_key(&b_static)
            .fixed_ephemeral_key_for_testing_only(&b_eph)
            .build_responder()
            .unwrap();

        let mut buf = [0u8; 256];
        let mut rbuf = [0u8; 256];

        let len = initiator.write_message(&[], &mut buf).unwrap();
        assert_eq!(to_hex(&buf[..len]), MSG1);
        responder.read_message(&buf[..len], &mut rbuf).unwrap();

        let len = responder.write_message(&[], &mut buf).unwrap();
        assert_eq!(to_hex(&buf[..len]), MSG2);
        initiator.read_message(&buf[..len], &mut rbuf).unwrap();

        let len = initiator.write_message(&[], &mut buf).unwrap();
        assert_eq!(to_hex(&buf[..len]), MSG3);
        responder.read_message(&buf[..len], &mut rbuf).unwrap();

        assert_eq!(initiator.get_remote_static().unwrap(), hex(B_STATIC_PUBLIC));
        assert_eq!(responder.get_remote_static().unwrap(), hex(A_STATIC_PUBLIC));

        let mut a_transport = initiator.into_transport_mode().unwrap();
        let mut b_transport = responder.into_transport_mode().unwrap();

        let mut ctbuf = [0u8; 256];
        let ctlen = a_transport.write_message(b"hello from A", &mut ctbuf).unwrap();
        assert_eq!(to_hex(&ctbuf[..ctlen]), CT_A_TO_B_1);
        let mut ptbuf = [0u8; 256];
        let ptlen = b_transport.read_message(&ctbuf[..ctlen], &mut ptbuf).unwrap();
        assert_eq!(&ptbuf[..ptlen], b"hello from A");

        let ctlen = b_transport.write_message(b"hello from B", &mut ctbuf).unwrap();
        assert_eq!(to_hex(&ctbuf[..ctlen]), CT_B_TO_A_1);
        let ptlen = a_transport.read_message(&ctbuf[..ctlen], &mut ptbuf).unwrap();
        assert_eq!(&ptbuf[..ptlen], b"hello from B");
    }

    fn keypair_from_raw(raw: &[u8]) -> PrivateKey {
        PrivateKey::from_base64(&base64_encode(raw)).unwrap()
    }
    fn base64_encode(raw: &[u8]) -> String {
        use base64::{engine::general_purpose::STANDARD, Engine};
        STANDARD.encode(raw)
    }

    #[test]
    fn noise_session_handshake_and_transport_roundtrip() {
        let a_priv = keypair_from_raw(&hex(A_STATIC_PRIVATE));
        let b_priv = keypair_from_raw(&hex(B_STATIC_PRIVATE));

        let (mut initiator, msg1) = NoiseSession::as_initiator(&a_priv).unwrap();
        let mut responder = NoiseSession::as_responder(&b_priv).unwrap();

        let (reply, done) = responder.handle_handshake_message(&msg1);
        assert!(!done);
        let reply = reply.unwrap();

        let (msg3, done) = initiator.handle_handshake_message(&reply);
        assert!(done);
        assert!(initiator.ready());
        let msg3 = msg3.unwrap();

        let (_, done) = responder.handle_handshake_message(&msg3);
        assert!(done);
        assert!(responder.ready());

        assert_eq!(initiator.peer_static_key().unwrap(), hex(B_STATIC_PUBLIC).as_slice());
        assert_eq!(responder.peer_static_key().unwrap(), hex(A_STATIC_PUBLIC).as_slice());

        let ct = initiator.encrypt(b"hello from A").unwrap();
        assert_eq!(responder.decrypt(&ct).unwrap(), b"hello from A");

        let ct2 = responder.encrypt(b"hello from B").unwrap();
        assert_eq!(initiator.decrypt(&ct2).unwrap(), b"hello from B");
    }

    #[test]
    fn noise_session_rejects_corrupted_ciphertext() {
        let a_priv = keypair_from_raw(&hex(A_STATIC_PRIVATE));
        let b_priv = keypair_from_raw(&hex(B_STATIC_PRIVATE));

        let (mut initiator, msg1) = NoiseSession::as_initiator(&a_priv).unwrap();
        let mut responder = NoiseSession::as_responder(&b_priv).unwrap();
        let (reply, _) = responder.handle_handshake_message(&msg1);
        let (msg3, _) = initiator.handle_handshake_message(&reply.unwrap());
        responder.handle_handshake_message(&msg3.unwrap());

        let mut ct = initiator.encrypt(b"hi").unwrap();
        ct[0] ^= 0xff;
        assert!(responder.decrypt(&ct).is_err());
    }

    #[test]
    fn noise_session_handshake_failure_is_reported_not_panicked() {
        let b_priv = keypair_from_raw(&hex(B_STATIC_PRIVATE));

        let mut responder = NoiseSession::as_responder(&b_priv).unwrap();
        let (reply, done) = responder.handle_handshake_message(&[0xffu8; 4]);
        assert!(done);
        assert!(reply.is_none());
        assert!(responder.failed());
        assert!(!responder.ready());
    }
}
