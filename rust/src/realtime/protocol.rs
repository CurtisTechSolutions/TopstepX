//! ASP.NET Core SignalR JSON hub protocol, version 1: message framing and parsing.

use serde_json::Value;

/// Terminates every message.
pub(crate) const RECORD_SEPARATOR: char = '\u{1e}';
/// First message sent on a new connection.
pub(crate) const HANDSHAKE_REQUEST: &str = "{\"protocol\":\"json\",\"version\":1}\u{1e}";
/// Keep-alive message (type 6).
pub(crate) const PING: &str = "{\"type\":6}\u{1e}";
/// Graceful close message (type 7).
pub(crate) const CLOSE: &str = "{\"type\":7}\u{1e}";

/// A decoded hub message.
#[derive(Debug, Clone, PartialEq)]
pub(crate) enum HubMessage {
    /// Type 1 from the server: a client method call, delivered as an event.
    Invocation {
        target: String,
        arguments: Vec<Value>,
    },
    /// Type 3: the result of a client-initiated invocation.
    Completion {
        invocation_id: String,
        result: Value,
        error: Option<String>,
    },
    /// Type 6.
    Ping,
    /// Type 7.
    Close {
        error: Option<String>,
        allow_reconnect: bool,
    },
    /// Stream messages (types 2, 4, 5) and anything newer; ignored.
    Other,
}

/// Parses one record (without its separator).
pub(crate) fn parse_message(record: &str) -> Result<HubMessage, String> {
    let value: Value =
        serde_json::from_str(record).map_err(|err| format!("invalid JSON: {err}"))?;
    let Value::Object(mut map) = value else {
        return Err("message is not a JSON object".into());
    };
    let kind = map
        .get("type")
        .and_then(Value::as_u64)
        .ok_or("message has no numeric `type`")?;
    Ok(match kind {
        1 => {
            let target = match map.remove("target") {
                Some(Value::String(target)) => target,
                _ => return Err("invocation without a target".into()),
            };
            let arguments = match map.remove("arguments") {
                Some(Value::Array(arguments)) => arguments,
                None | Some(Value::Null) => Vec::new(),
                Some(other) => vec![other],
            };
            HubMessage::Invocation { target, arguments }
        }
        3 => {
            let invocation_id = match map.remove("invocationId") {
                Some(Value::String(id)) => id,
                Some(Value::Number(id)) => id.to_string(),
                _ => return Err("completion without an invocationId".into()),
            };
            HubMessage::Completion {
                invocation_id,
                result: map.remove("result").unwrap_or(Value::Null),
                error: error_text(map.remove("error")),
            }
        }
        6 => HubMessage::Ping,
        7 => HubMessage::Close {
            error: error_text(map.remove("error")),
            allow_reconnect: map
                .get("allowReconnect")
                .and_then(Value::as_bool)
                .unwrap_or(false),
        },
        _ => HubMessage::Other,
    })
}

fn error_text(value: Option<Value>) -> Option<String> {
    match value {
        None | Some(Value::Null) => None,
        Some(Value::String(text)) => Some(text),
        Some(other) => Some(other.to_string()),
    }
}

/// Parses the handshake response: `{}` on success, `{"error": "..."}` on failure.
pub(crate) fn parse_handshake(record: &str) -> Result<(), String> {
    let value: Value =
        serde_json::from_str(record).map_err(|err| format!("invalid handshake response: {err}"))?;
    match error_text(value.get("error").cloned()) {
        Some(error) => Err(error),
        None => Ok(()),
    }
}

/// Encodes a client invocation that expects a completion.
pub(crate) fn encode_invocation(invocation_id: &str, target: &str, arguments: &[Value]) -> String {
    let mut text = serde_json::json!({
        "type": 1,
        "invocationId": invocation_id,
        "target": target,
        "arguments": arguments,
    })
    .to_string();
    text.push(RECORD_SEPARATOR);
    text
}

/// Splits incoming text into records, keeping an incomplete trailing record for the next
/// frame.
#[derive(Debug, Default)]
pub(crate) struct FrameBuffer {
    pending: String,
}

impl FrameBuffer {
    /// Appends `chunk` and returns every complete, non-empty record.
    pub(crate) fn push(&mut self, chunk: &str) -> Vec<String> {
        self.pending.push_str(chunk);
        let mut records = Vec::new();
        let mut start = 0;
        while let Some(offset) = self.pending[start..].find(RECORD_SEPARATOR) {
            let end = start + offset;
            let record = &self.pending[start..end];
            if !record.trim().is_empty() {
                records.push(record.to_owned());
            }
            start = end + RECORD_SEPARATOR.len_utf8();
        }
        self.pending.drain(..start);
        records
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn frames_split_across_and_within_chunks() {
        let mut buffer = FrameBuffer::default();
        assert!(buffer.push("{\"type\":").is_empty());
        let records = buffer.push("6}\u{1e}{}\u{1e}{\"type\":1,\"tar");
        assert_eq!(records, vec!["{\"type\":6}".to_owned(), "{}".to_owned()]);
        let records = buffer.push("get\":\"X\",\"arguments\":[1]}\u{1e}");
        assert_eq!(
            parse_message(&records[0]).unwrap(),
            HubMessage::Invocation {
                target: "X".into(),
                arguments: vec![json!(1)]
            }
        );
        assert!(
            buffer.push("\u{1e}\u{1e}").is_empty(),
            "empty records are skipped"
        );
    }

    #[test]
    fn parses_every_message_type() {
        assert_eq!(parse_message(r#"{"type":6}"#).unwrap(), HubMessage::Ping);
        assert_eq!(
            parse_message(r#"{"type":3,"invocationId":"4","result":null}"#).unwrap(),
            HubMessage::Completion {
                invocation_id: "4".into(),
                result: Value::Null,
                error: None
            }
        );
        assert_eq!(
            parse_message(r#"{"type":3,"invocationId":"5","error":"boom"}"#).unwrap(),
            HubMessage::Completion {
                invocation_id: "5".into(),
                result: Value::Null,
                error: Some("boom".into())
            }
        );
        assert_eq!(
            parse_message(r#"{"type":7,"error":"bye","allowReconnect":true}"#).unwrap(),
            HubMessage::Close {
                error: Some("bye".into()),
                allow_reconnect: true
            }
        );
        assert_eq!(
            parse_message(r#"{"type":7}"#).unwrap(),
            HubMessage::Close {
                error: None,
                allow_reconnect: false
            }
        );
        assert_eq!(
            parse_message(r#"{"type":2,"invocationId":"1","item":1}"#).unwrap(),
            HubMessage::Other
        );
        assert!(parse_message("[]").is_err());
        assert!(parse_message(r#"{"type":1}"#).is_err());
    }

    #[test]
    fn handshake_and_invocation_encoding() {
        assert!(parse_handshake("{}").is_ok());
        assert_eq!(parse_handshake(r#"{"error":"nope"}"#), Err("nope".into()));
        let text = encode_invocation("1", "SubscribeOrders", &[json!(123)]);
        assert!(text.ends_with(RECORD_SEPARATOR));
        let value: Value = serde_json::from_str(text.trim_end_matches(RECORD_SEPARATOR)).unwrap();
        assert_eq!(
            value,
            json!({"type":1,"invocationId":"1","target":"SubscribeOrders","arguments":[123]})
        );
    }
}
