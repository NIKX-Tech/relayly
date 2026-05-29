use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct WireMessage {
    #[serde(rename = "type")]
    pub msg_type: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub device_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub public_key: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub code: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expires_in: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub peer_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub peer_public_key: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub to: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub from: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub payload: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub nonce: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timestamp: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub message: Option<String>,
}

impl WireMessage {
    pub fn auth(device_id: &str, public_key: &str) -> Self {
        Self {
            msg_type: "auth".into(),
            device_id: Some(device_id.into()),
            public_key: Some(public_key.into()),
            ..Default::default()
        }
    }
    pub fn pair_request() -> Self {
        Self { msg_type: "pair_request".into(), ..Default::default() }
    }
    pub fn pair_accept(code: &str) -> Self {
        Self { msg_type: "pair_accept".into(), code: Some(code.into()), ..Default::default() }
    }
    pub fn send_msg(to: &str, payload: &str, nonce: &str) -> Self {
        Self {
            msg_type: "send".into(),
            to: Some(to.into()),
            payload: Some(payload.into()),
            nonce: Some(nonce.into()),
            ..Default::default()
        }
    }
    pub fn ping() -> Self {
        Self { msg_type: "ping".into(), ..Default::default() }
    }
}
