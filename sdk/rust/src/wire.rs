use serde::{Deserialize, Serialize};

/// One entry of welcome's `peers` array.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WirePeer {
    pub id: String,
    pub static_key: String,
}

/// The JSON frame exchanged on the control channel (text frames only). Fields are
/// selectively populated depending on `msg_type`; unknown fields on the way in are
/// ignored by serde, and `None` fields are omitted going out.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct WireMessage {
    #[serde(rename = "type")]
    pub msg_type: String,

    // welcome
    #[serde(skip_serializing_if = "Option::is_none")]
    pub protocol_version: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub device_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub peers: Option<Vec<WirePeer>>,

    // announce_key
    #[serde(skip_serializing_if = "Option::is_none")]
    pub static_key: Option<String>,

    // pair_code / pair_accept / pair_complete (code doubles as the error machine code
    // on error messages, matching the server's wire shape)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub code: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expires_in: Option<u64>,

    // pair_complete (peer_id is reused by peer_status)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub peer_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub peer_static_key: Option<String>,

    // peer_status
    #[serde(skip_serializing_if = "Option::is_none")]
    pub online: Option<bool>,

    // error
    #[serde(skip_serializing_if = "Option::is_none")]
    pub message: Option<String>,
}

impl WireMessage {
    pub fn announce_key(static_key: &str) -> Self {
        Self { msg_type: "announce_key".into(), static_key: Some(static_key.into()), ..Default::default() }
    }
    pub fn pair_request() -> Self {
        Self { msg_type: "pair_request".into(), ..Default::default() }
    }
    pub fn pair_accept(code: &str) -> Self {
        Self { msg_type: "pair_accept".into(), code: Some(code.into()), ..Default::default() }
    }
    pub fn ping() -> Self {
        Self { msg_type: "ping".into(), ..Default::default() }
    }
}
