//! Serde helpers for timestamps and lenient numbers.

use chrono::{DateTime, NaiveDateTime, SecondsFormat, Utc};
use serde::de::{self, Deserializer, Unexpected, Visitor};
use serde::Serializer;
use std::fmt;

/// Parses an RFC 3339 / ISO 8601 timestamp.
///
/// Accepts `Z` or a numeric offset, any number of fractional digits (.NET emits up to seven),
/// and a missing offset, which is read as UTC.
pub(crate) fn parse_timestamp(value: &str) -> Option<DateTime<Utc>> {
    let value = value.trim();
    if let Ok(parsed) = DateTime::parse_from_rfc3339(value) {
        return Some(parsed.with_timezone(&Utc));
    }
    // RFC 3339 requires seconds; ISO 8601 as emitted by some serializers may omit the offset.
    for format in [
        "%Y-%m-%dT%H:%M:%S%.f",
        "%Y-%m-%dT%H:%M:%S",
        "%Y-%m-%d %H:%M:%S%.f",
    ] {
        if let Ok(naive) = NaiveDateTime::parse_from_str(value, format) {
            return Some(naive.and_utc());
        }
    }
    None
}

/// Formats a timestamp as RFC 3339 in UTC with a `Z` suffix.
pub(crate) fn format_timestamp(value: &DateTime<Utc>) -> String {
    value.to_rfc3339_opts(SecondsFormat::AutoSi, true)
}

struct TimestampVisitor;

impl Visitor<'_> for TimestampVisitor {
    type Value = DateTime<Utc>;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("an RFC 3339 timestamp")
    }

    fn visit_str<E: de::Error>(self, value: &str) -> Result<Self::Value, E> {
        parse_timestamp(value).ok_or_else(|| E::invalid_value(Unexpected::Str(value), &self))
    }
}

/// `#[serde(with = "...")]` module for required `DateTime<Utc>` fields.
pub(crate) mod timestamp {
    use super::*;

    pub(crate) fn serialize<S: Serializer>(value: &DateTime<Utc>, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&format_timestamp(value))
    }

    pub(crate) fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<DateTime<Utc>, D::Error> {
        d.deserialize_str(TimestampVisitor)
    }
}

/// `#[serde(with = "...")]` module for optional `DateTime<Utc>` fields; `null` decodes to
/// `None`. Pair with `#[serde(default)]` so a missing field is also `None`.
pub(crate) mod timestamp_option {
    use super::*;

    pub(crate) fn serialize<S: Serializer>(
        value: &Option<DateTime<Utc>>,
        s: S,
    ) -> Result<S::Ok, S::Error> {
        match value {
            Some(value) => s.serialize_str(&format_timestamp(value)),
            None => s.serialize_none(),
        }
    }

    pub(crate) fn deserialize<'de, D: Deserializer<'de>>(
        d: D,
    ) -> Result<Option<DateTime<Utc>>, D::Error> {
        struct OptionVisitor;

        impl<'de> Visitor<'de> for OptionVisitor {
            type Value = Option<DateTime<Utc>>;

            fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str("an RFC 3339 timestamp or null")
            }

            fn visit_none<E: de::Error>(self) -> Result<Self::Value, E> {
                Ok(None)
            }

            fn visit_unit<E: de::Error>(self) -> Result<Self::Value, E> {
                Ok(None)
            }

            fn visit_some<D: Deserializer<'de>>(self, d: D) -> Result<Self::Value, D::Error> {
                d.deserialize_str(TimestampVisitor).map(Some)
            }
        }

        d.deserialize_option(OptionVisitor)
    }
}

/// Like [`timestamp_option`], but an unparseable string decodes to `None` instead of failing.
/// Used for realtime payloads, where losing a whole event over a timestamp format is worse
/// than losing the timestamp.
#[cfg(any(feature = "realtime", test))]
pub(crate) mod lenient_timestamp {
    use super::*;

    pub(crate) fn serialize<S: Serializer>(
        value: &Option<DateTime<Utc>>,
        s: S,
    ) -> Result<S::Ok, S::Error> {
        timestamp_option::serialize(value, s)
    }

    pub(crate) fn deserialize<'de, D: Deserializer<'de>>(
        d: D,
    ) -> Result<Option<DateTime<Utc>>, D::Error> {
        let value: Option<serde_json::Value> = serde::Deserialize::deserialize(d)?;
        Ok(match value {
            Some(serde_json::Value::String(text)) => parse_timestamp(&text),
            _ => None,
        })
    }
}

/// Lenient optional integer: accepts integers, integral floats (`3.0`) and numeric strings.
/// Non-integral floats are rounded to the nearest integer.
#[cfg(any(feature = "realtime", test))]
pub(crate) mod lenient_i64 {
    use super::*;

    pub(crate) fn serialize<S: Serializer>(value: &Option<i64>, s: S) -> Result<S::Ok, S::Error> {
        match value {
            Some(value) => s.serialize_i64(*value),
            None => s.serialize_none(),
        }
    }

    pub(crate) fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<Option<i64>, D::Error> {
        let value: Option<serde_json::Value> = serde::Deserialize::deserialize(d)?;
        match value {
            None | Some(serde_json::Value::Null) => Ok(None),
            Some(serde_json::Value::Number(number)) => {
                if let Some(v) = number.as_i64() {
                    Ok(Some(v))
                } else if let Some(v) = number.as_u64() {
                    i64::try_from(v)
                        .map(Some)
                        .map_err(|_| de::Error::custom("integer out of range"))
                } else {
                    number
                        .as_f64()
                        .map(|v| Some(v.round() as i64))
                        .ok_or_else(|| de::Error::custom("invalid number"))
                }
            }
            Some(serde_json::Value::String(text)) => {
                let text = text.trim();
                text.parse::<i64>()
                    .ok()
                    .or_else(|| text.parse::<f64>().ok().map(|v| v.round() as i64))
                    .map(Some)
                    .ok_or_else(|| de::Error::invalid_value(Unexpected::Str(text), &"a number"))
            }
            Some(other) => Err(de::Error::custom(format!(
                "expected a number, found {other}"
            ))),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::{TimeZone, Timelike};
    use serde::{Deserialize, Serialize};

    #[test]
    fn parses_common_timestamp_shapes() {
        let expected = Utc.with_ymd_and_hms(2025, 4, 14, 17, 49, 10).unwrap();
        for text in [
            "2025-04-14T17:49:10Z",
            "2025-04-14T17:49:10+00:00",
            "2025-04-14T19:49:10+02:00",
            "2025-04-14T17:49:10",
        ] {
            assert_eq!(parse_timestamp(text), Some(expected), "{text}");
        }
        let net = parse_timestamp("2025-04-14T17:49:10.1425326+00:00").unwrap();
        assert_eq!(net.nanosecond(), 142_532_600);
        let nine = parse_timestamp("2025-04-14T17:49:10.123456789Z").unwrap();
        assert_eq!(nine.nanosecond(), 123_456_789);
        assert_eq!(parse_timestamp("not a time"), None);
    }

    #[test]
    fn formats_with_z_suffix() {
        let t = Utc.with_ymd_and_hms(2025, 7, 1, 0, 0, 0).unwrap();
        assert_eq!(format_timestamp(&t), "2025-07-01T00:00:00Z");
    }

    #[derive(Serialize, Deserialize, PartialEq, Debug)]
    struct Holder {
        #[serde(default, with = "timestamp_option")]
        at: Option<DateTime<Utc>>,
        #[serde(default, with = "lenient_i64")]
        volume: Option<i64>,
        #[serde(default, with = "lenient_timestamp")]
        loose: Option<DateTime<Utc>>,
    }

    #[test]
    fn optional_helpers() {
        let h: Holder =
            serde_json::from_str(r#"{"at":null,"volume":"3","loose":"garbage"}"#).unwrap();
        assert_eq!(
            h,
            Holder {
                at: None,
                volume: Some(3),
                loose: None
            }
        );
        let h: Holder = serde_json::from_str(r#"{"volume":3.0}"#).unwrap();
        assert_eq!(h.volume, Some(3));
        let h: Holder = serde_json::from_str(r#"{"at":"2025-07-01T00:00:00Z"}"#).unwrap();
        assert!(h.at.is_some());
        assert!(serde_json::from_str::<Holder>(r#"{"at":"garbage"}"#).is_err());
    }
}
