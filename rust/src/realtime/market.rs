//! The market hub: quotes, trades and depth per contract.

use std::fmt;

use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tokio::sync::broadcast;
use url::Url;

use super::connection::{Connection, RegistryUpdate};
use super::{ConnectionEvent, ConnectionState, HubEvent, HubOptions, TokenProvider, UnknownEvent};
use crate::enums::{DomType, TradeLogType};
use crate::error::Result;

/// `GatewayQuote` payload. Updates can be partial; fold them into a running quote with
/// [`merge`](Self::merge).
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct GatewayQuote {
    /// Symbol identifier.
    #[serde(default)]
    pub symbol: Option<String>,
    /// Friendly symbol name.
    #[serde(default)]
    pub symbol_name: Option<String>,
    /// Last traded price.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub last_price: Option<Decimal>,
    /// Best bid.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub best_bid: Option<Decimal>,
    /// Best ask.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub best_ask: Option<Decimal>,
    /// Change since the previous close.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub change: Option<Decimal>,
    /// Percent change since the previous close.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub change_percent: Option<Decimal>,
    /// Session open.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub open: Option<Decimal>,
    /// Session high.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub high: Option<Decimal>,
    /// Session low.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub low: Option<Decimal>,
    /// Session volume.
    #[serde(default, with = "crate::serde_util::lenient_i64")]
    pub volume: Option<i64>,
    /// Last update time.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub last_updated: Option<DateTime<Utc>>,
    /// Quote time.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub timestamp: Option<DateTime<Utc>>,
}

impl GatewayQuote {
    /// Overwrites every field that `update` carries, keeping the others.
    pub fn merge(&mut self, update: &GatewayQuote) {
        macro_rules! take {
            ($($field:ident),+) => {
                $(if update.$field.is_some() { self.$field = update.$field.clone(); })+
            };
        }
        take!(
            symbol,
            symbol_name,
            last_price,
            best_bid,
            best_ask,
            change,
            change_percent,
            open,
            high,
            low,
            volume,
            last_updated,
            timestamp
        );
    }
}

/// One `GatewayTrade` entry: a market trade.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct GatewayTrade {
    /// Symbol identifier.
    #[serde(default)]
    pub symbol_id: Option<String>,
    /// Trade price.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub price: Option<Decimal>,
    /// Trade time.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub timestamp: Option<DateTime<Utc>>,
    /// Aggressor side (`type`).
    #[serde(rename = "type", default)]
    pub trade_type: Option<TradeLogType>,
    /// Traded quantity.
    #[serde(default, with = "crate::serde_util::lenient_i64")]
    pub volume: Option<i64>,
}

/// One `GatewayDepth` entry: a change at one price level.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct GatewayDepth {
    /// Update time.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub timestamp: Option<DateTime<Utc>>,
    /// Entry type (`type`); [`DomType::Reset`] means the book should be cleared.
    #[serde(rename = "type", default)]
    pub depth_type: Option<DomType>,
    /// Price level.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub price: Option<Decimal>,
    /// Total volume at the level.
    #[serde(default, with = "crate::serde_util::lenient_i64")]
    pub volume: Option<i64>,
    /// Current volume at the level.
    #[serde(default, with = "crate::serde_util::lenient_i64")]
    pub current_volume: Option<i64>,
}

/// An event from the market hub. Market-hub callbacks receive `(contractId, data)`.
#[derive(Debug, Clone, PartialEq)]
#[non_exhaustive]
pub enum MarketEvent {
    /// `GatewayQuote`: a quote update, possibly partial.
    Quote {
        /// Contract the update belongs to.
        contract_id: String,
        /// Payload.
        quote: GatewayQuote,
    },
    /// `GatewayTrade`: one or more market trades, as delivered in one message.
    Trades {
        /// Contract the trades belong to.
        contract_id: String,
        /// Trades in delivery order.
        trades: Vec<GatewayTrade>,
    },
    /// `GatewayDepth`: one or more depth changes, as delivered in one message.
    Depth {
        /// Contract the changes belong to.
        contract_id: String,
        /// Changes in delivery order.
        entries: Vec<GatewayDepth>,
    },
    /// Connection lifecycle.
    Connection(ConnectionEvent),
    /// Unrecognized target or undecodable payload.
    Unknown(UnknownEvent),
}

fn as_list(data: Value) -> Vec<Value> {
    match data {
        Value::Array(items) => items,
        other => vec![other],
    }
}

#[derive(Clone, Copy)]
enum MarketTarget {
    Quote,
    Trade,
    Depth,
}

impl MarketTarget {
    fn parse(target: &str) -> Option<Self> {
        match target {
            "GatewayQuote" => Some(MarketTarget::Quote),
            "GatewayTrade" => Some(MarketTarget::Trade),
            "GatewayDepth" => Some(MarketTarget::Depth),
            _ => None,
        }
    }

    fn decode(self, contract_id: String, data: Value) -> serde_json::Result<Vec<MarketEvent>> {
        let items = as_list(data).into_iter();
        Ok(match self {
            MarketTarget::Quote => items
                .map(|item| {
                    Ok(MarketEvent::Quote {
                        contract_id: contract_id.clone(),
                        quote: serde_json::from_value(item)?,
                    })
                })
                .collect::<serde_json::Result<_>>()?,
            MarketTarget::Trade => vec![MarketEvent::Trades {
                contract_id,
                trades: items
                    .map(serde_json::from_value)
                    .collect::<serde_json::Result<_>>()?,
            }],
            MarketTarget::Depth => vec![MarketEvent::Depth {
                contract_id,
                entries: items
                    .map(serde_json::from_value)
                    .collect::<serde_json::Result<_>>()?,
            }],
        })
    }
}

impl HubEvent for MarketEvent {
    fn from_invocation(target: String, arguments: Vec<Value>) -> Vec<Self> {
        let Some(kind) = MarketTarget::parse(&target) else {
            return vec![MarketEvent::Unknown(UnknownEvent {
                target,
                arguments,
                error: None,
            })];
        };
        let unknown = |target: String, arguments: Vec<Value>, error: String| {
            vec![MarketEvent::Unknown(UnknownEvent {
                target,
                arguments,
                error: Some(error),
            })]
        };
        let (contract_id, data) = match (arguments.first(), arguments.get(1)) {
            (Some(Value::String(contract_id)), Some(data)) => (contract_id.clone(), data.clone()),
            _ => {
                return unknown(
                    target,
                    arguments,
                    "expected (contractId, data) arguments".into(),
                )
            }
        };
        match kind.decode(contract_id, data) {
            Ok(events) => events,
            Err(err) => unknown(target, arguments, err.to_string()),
        }
    }

    fn from_connection(event: ConnectionEvent) -> Self {
        MarketEvent::Connection(event)
    }
}

/// Connection to the market hub.
///
/// Clones share the connection, which closes when the last clone is dropped or
/// [`close`](Self::close) is called.
#[derive(Clone)]
pub struct MarketHub {
    connection: Connection<MarketEvent>,
}

impl fmt::Debug for MarketHub {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("MarketHub")
            .field("url", &self.connection.url().as_str())
            .field("state", &self.connection.state())
            .finish()
    }
}

impl MarketHub {
    /// Connects and completes the SignalR handshake. Usually called through
    /// [`Client::connect_market_hub`](crate::Client::connect_market_hub).
    pub async fn connect(
        url: Url,
        tokens: TokenProvider,
        options: HubOptions,
    ) -> Result<MarketHub> {
        Ok(MarketHub {
            connection: Connection::connect(url, tokens, options).await?,
        })
    }

    /// A new receiver for events published from now on. Call it before subscribing so no
    /// updates are missed. The stream ends after [`ConnectionEvent::Closed`].
    pub fn events(&self) -> broadcast::Receiver<MarketEvent> {
        self.connection.events()
    }

    /// Current connection state.
    pub fn state(&self) -> ConnectionState {
        self.connection.state()
    }

    /// Waits until the connection is in `state` or closed, and returns the state reached.
    pub async fn wait_for_state(&self, state: ConnectionState) -> ConnectionState {
        self.connection.wait_for_state(state).await
    }

    /// `SubscribeContractQuotes`: quote updates (`GatewayQuote`).
    pub async fn subscribe_contract_quotes(&self, contract_id: &str) -> Result<()> {
        Ok(self
            .connection
            .subscribe("SubscribeContractQuotes", vec![json!(contract_id)])
            .await?)
    }

    /// `UnsubscribeContractQuotes`.
    pub async fn unsubscribe_contract_quotes(&self, contract_id: &str) -> Result<()> {
        Ok(self
            .connection
            .unsubscribe(
                "SubscribeContractQuotes",
                "UnsubscribeContractQuotes",
                vec![json!(contract_id)],
            )
            .await?)
    }

    /// `SubscribeContractTrades`: market trades (`GatewayTrade`).
    pub async fn subscribe_contract_trades(&self, contract_id: &str) -> Result<()> {
        Ok(self
            .connection
            .subscribe("SubscribeContractTrades", vec![json!(contract_id)])
            .await?)
    }

    /// `UnsubscribeContractTrades`.
    pub async fn unsubscribe_contract_trades(&self, contract_id: &str) -> Result<()> {
        Ok(self
            .connection
            .unsubscribe(
                "SubscribeContractTrades",
                "UnsubscribeContractTrades",
                vec![json!(contract_id)],
            )
            .await?)
    }

    /// `SubscribeContractMarketDepth`: depth updates (`GatewayDepth`).
    pub async fn subscribe_contract_market_depth(&self, contract_id: &str) -> Result<()> {
        Ok(self
            .connection
            .subscribe("SubscribeContractMarketDepth", vec![json!(contract_id)])
            .await?)
    }

    /// `UnsubscribeContractMarketDepth`.
    pub async fn unsubscribe_contract_market_depth(&self, contract_id: &str) -> Result<()> {
        Ok(self
            .connection
            .unsubscribe(
                "SubscribeContractMarketDepth",
                "UnsubscribeContractMarketDepth",
                vec![json!(contract_id)],
            )
            .await?)
    }

    /// Invokes any hub method and returns its result. Such calls are not replayed after a
    /// reconnect.
    pub async fn invoke(&self, method: &str, arguments: Vec<Value>) -> Result<Value> {
        Ok(self
            .connection
            .invoke(method, arguments, RegistryUpdate::None)
            .await?)
    }

    /// Closes the connection and stops reconnecting.
    pub async fn close(&self) {
        self.connection.close().await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn quotes_trades_and_depth() {
        let events = MarketEvent::from_invocation(
            "GatewayQuote".into(),
            vec![
                json!("CON.F.US.EP.U25"),
                json!({"symbol": "F.US.EP", "bestBid": 6000.25, "volume": 12.0}),
            ],
        );
        let MarketEvent::Quote { contract_id, quote } = &events[0] else {
            panic!("{events:?}")
        };
        assert_eq!(contract_id, "CON.F.US.EP.U25");
        assert_eq!(quote.best_bid, Some(Decimal::new(600025, 2)));
        assert_eq!(quote.volume, Some(12));

        let trades = MarketEvent::from_invocation(
            "GatewayTrade".into(),
            vec![
                json!("C"),
                json!([{"price": 1.25, "type": 0, "volume": 2}, {"price": 1.5, "type": 1, "volume": 1}]),
            ],
        );
        let MarketEvent::Trades { trades, .. } = &trades[0] else {
            panic!()
        };
        assert_eq!(trades.len(), 2);
        assert_eq!(trades[1].trade_type, Some(TradeLogType::Sell));

        let single = MarketEvent::from_invocation(
            "GatewayDepth".into(),
            vec![json!("C"), json!({"type": 6, "price": 0, "volume": 0})],
        );
        let MarketEvent::Depth { entries, .. } = &single[0] else {
            panic!()
        };
        assert_eq!(entries[0].depth_type, Some(DomType::Reset));
    }

    #[test]
    fn malformed_market_events_become_unknown() {
        let events =
            MarketEvent::from_invocation("GatewayQuote".into(), vec![json!({"bestBid": 1})]);
        assert!(matches!(
            &events[0],
            MarketEvent::Unknown(UnknownEvent { error: Some(_), .. })
        ));
        let events = MarketEvent::from_invocation(
            "GatewayTrade".into(),
            vec![json!("C"), json!([{"price": "x"}])],
        );
        assert!(matches!(
            &events[0],
            MarketEvent::Unknown(UnknownEvent { error: Some(_), .. })
        ));
        let events = MarketEvent::from_invocation("NewThing".into(), vec![]);
        assert!(matches!(
            &events[0],
            MarketEvent::Unknown(UnknownEvent { error: None, .. })
        ));
    }

    #[test]
    fn quote_merge_keeps_unchanged_fields() {
        let mut state: GatewayQuote =
            serde_json::from_value(json!({"bestBid": 1.0, "bestAsk": 1.25, "lastPrice": 1.0}))
                .unwrap();
        let update: GatewayQuote = serde_json::from_value(json!({"bestBid": 1.25})).unwrap();
        state.merge(&update);
        assert_eq!(state.best_bid, Some(Decimal::new(125, 2)));
        assert_eq!(state.best_ask, Some(Decimal::new(125, 2)));
        assert_eq!(state.last_price, Some(Decimal::ONE));
    }
}
