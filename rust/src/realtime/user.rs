//! The user hub: account, order, position and trade updates.

use std::fmt;

use chrono::{DateTime, Utc};
use rust_decimal::Decimal;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tokio::sync::broadcast;
use url::Url;

use super::connection::{Connection, RegistryUpdate};
use super::{
    unwrap_envelope, ConnectionEvent, ConnectionState, HubEvent, HubOptions, TokenProvider,
    UnknownEvent,
};
use crate::enums::{OrderSide, OrderStatus, OrderType, PositionType};
use crate::error::Result;

/// `GatewayUserAccount` payload.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct GatewayUserAccount {
    /// Account identifier.
    pub id: i32,
    /// Account name.
    #[serde(default)]
    pub name: Option<String>,
    /// Current balance.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub balance: Option<Decimal>,
    /// Whether the account may trade.
    #[serde(default)]
    pub can_trade: Option<bool>,
    /// Whether the account is visible.
    #[serde(default)]
    pub is_visible: Option<bool>,
    /// Whether the account is simulated.
    #[serde(default)]
    pub simulated: Option<bool>,
}

/// `GatewayUserOrder` payload.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct GatewayUserOrder {
    /// Order identifier.
    pub id: i64,
    /// Owning account.
    pub account_id: i32,
    /// Contract identifier.
    pub contract_id: String,
    /// Symbol identifier.
    #[serde(default)]
    pub symbol_id: Option<String>,
    /// Creation time.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub creation_timestamp: Option<DateTime<Utc>>,
    /// Last update time.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub update_timestamp: Option<DateTime<Utc>>,
    /// Status.
    #[serde(default)]
    pub status: Option<OrderStatus>,
    /// Order type (`type`).
    #[serde(rename = "type", default)]
    pub order_type: Option<OrderType>,
    /// Side.
    #[serde(default)]
    pub side: Option<OrderSide>,
    /// Quantity.
    #[serde(default)]
    pub size: Option<i32>,
    /// Limit price.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub limit_price: Option<Decimal>,
    /// Stop price.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub stop_price: Option<Decimal>,
    /// Filled quantity.
    #[serde(default)]
    pub fill_volume: Option<i32>,
    /// Average fill price.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub filled_price: Option<Decimal>,
    /// Custom tag.
    #[serde(default)]
    pub custom_tag: Option<String>,
}

/// `GatewayUserPosition` payload. A size of zero means the position was closed.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct GatewayUserPosition {
    /// Position identifier.
    pub id: i32,
    /// Owning account.
    pub account_id: i32,
    /// Contract identifier.
    pub contract_id: String,
    /// When the position was opened.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub creation_timestamp: Option<DateTime<Utc>>,
    /// Direction (`type`).
    #[serde(rename = "type", default)]
    pub position_type: Option<PositionType>,
    /// Quantity.
    #[serde(default)]
    pub size: Option<i32>,
    /// Average entry price.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub average_price: Option<Decimal>,
}

/// `GatewayUserTrade` payload.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
#[non_exhaustive]
pub struct GatewayUserTrade {
    /// Trade identifier.
    pub id: i64,
    /// Owning account.
    pub account_id: i32,
    /// Contract identifier.
    pub contract_id: String,
    /// Execution time.
    #[serde(default, with = "crate::serde_util::lenient_timestamp")]
    pub creation_timestamp: Option<DateTime<Utc>>,
    /// Execution price.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub price: Option<Decimal>,
    /// Realized profit and loss; `None` for a half-turn trade.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub profit_and_loss: Option<Decimal>,
    /// Fees.
    #[serde(default, with = "rust_decimal::serde::float_option")]
    pub fees: Option<Decimal>,
    /// Side.
    #[serde(default)]
    pub side: Option<OrderSide>,
    /// Quantity.
    #[serde(default)]
    pub size: Option<i32>,
    /// Whether the trade was voided.
    #[serde(default)]
    pub voided: Option<bool>,
    /// Order that produced the trade.
    #[serde(default)]
    pub order_id: Option<i64>,
}

/// An event from the user hub.
///
/// `action` is the `action` member of the `{"action": n, "data": {...}}` envelope some
/// payloads arrive in; its meaning is undocumented.
#[derive(Debug, Clone, PartialEq)]
#[non_exhaustive]
#[allow(clippy::large_enum_variant)]
pub enum UserEvent {
    /// `GatewayUserAccount`.
    Account {
        /// Payload.
        account: GatewayUserAccount,
        /// Envelope action, when present.
        action: Option<i32>,
    },
    /// `GatewayUserOrder`.
    Order {
        /// Payload.
        order: GatewayUserOrder,
        /// Envelope action, when present.
        action: Option<i32>,
    },
    /// `GatewayUserPosition`.
    Position {
        /// Payload.
        position: GatewayUserPosition,
        /// Envelope action, when present.
        action: Option<i32>,
    },
    /// `GatewayUserTrade`.
    Trade {
        /// Payload.
        trade: GatewayUserTrade,
        /// Envelope action, when present.
        action: Option<i32>,
    },
    /// Connection lifecycle.
    Connection(ConnectionEvent),
    /// Unrecognized target or undecodable payload.
    Unknown(UnknownEvent),
}

type Decoder = fn(Value, Option<i32>) -> serde_json::Result<UserEvent>;

impl HubEvent for UserEvent {
    fn from_invocation(target: String, arguments: Vec<Value>) -> Vec<Self> {
        let decode: Decoder = match target.as_str() {
            "GatewayUserAccount" => |data, action| {
                Ok(UserEvent::Account {
                    account: serde_json::from_value(data)?,
                    action,
                })
            },
            "GatewayUserOrder" => |data, action| {
                Ok(UserEvent::Order {
                    order: serde_json::from_value(data)?,
                    action,
                })
            },
            "GatewayUserPosition" => |data, action| {
                Ok(UserEvent::Position {
                    position: serde_json::from_value(data)?,
                    action,
                })
            },
            "GatewayUserTrade" => |data, action| {
                Ok(UserEvent::Trade {
                    trade: serde_json::from_value(data)?,
                    action,
                })
            },
            _ => {
                return vec![UserEvent::Unknown(UnknownEvent {
                    target,
                    arguments,
                    error: None,
                })]
            }
        };
        let items = match arguments.first() {
            Some(Value::Array(items)) => items.clone(),
            Some(item) => vec![item.clone()],
            None => {
                let error = Some("the event has no payload".to_owned());
                return vec![UserEvent::Unknown(UnknownEvent {
                    target,
                    arguments,
                    error,
                })];
            }
        };
        items
            .into_iter()
            .map(|item| {
                let (action, data) = unwrap_envelope(item);
                decode(data, action).unwrap_or_else(|err| {
                    UserEvent::Unknown(UnknownEvent {
                        target: target.clone(),
                        arguments: arguments.clone(),
                        error: Some(err.to_string()),
                    })
                })
            })
            .collect()
    }

    fn from_connection(event: ConnectionEvent) -> Self {
        UserEvent::Connection(event)
    }
}

/// Connection to the user hub.
///
/// Clones share the connection, which closes when the last clone is dropped or
/// [`close`](Self::close) is called.
#[derive(Clone)]
pub struct UserHub {
    connection: Connection<UserEvent>,
}

impl fmt::Debug for UserHub {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("UserHub")
            .field("url", &self.connection.url().as_str())
            .field("state", &self.connection.state())
            .finish()
    }
}

impl UserHub {
    /// Connects and completes the SignalR handshake. Usually called through
    /// [`Client::connect_user_hub`](crate::Client::connect_user_hub).
    pub async fn connect(url: Url, tokens: TokenProvider, options: HubOptions) -> Result<UserHub> {
        Ok(UserHub {
            connection: Connection::connect(url, tokens, options).await?,
        })
    }

    /// A new receiver for events published from now on. Call it before subscribing so no
    /// updates are missed. The stream ends after [`ConnectionEvent::Closed`].
    pub fn events(&self) -> broadcast::Receiver<UserEvent> {
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

    /// `SubscribeAccounts`: account updates (`GatewayUserAccount`).
    pub async fn subscribe_accounts(&self) -> Result<()> {
        Ok(self
            .connection
            .subscribe("SubscribeAccounts", Vec::new())
            .await?)
    }

    /// `UnsubscribeAccounts`.
    pub async fn unsubscribe_accounts(&self) -> Result<()> {
        Ok(self
            .connection
            .unsubscribe("SubscribeAccounts", "UnsubscribeAccounts", Vec::new())
            .await?)
    }

    /// `SubscribeOrders`: order updates for one account (`GatewayUserOrder`).
    pub async fn subscribe_orders(&self, account_id: i32) -> Result<()> {
        Ok(self
            .connection
            .subscribe("SubscribeOrders", vec![json!(account_id)])
            .await?)
    }

    /// `UnsubscribeOrders`.
    pub async fn unsubscribe_orders(&self, account_id: i32) -> Result<()> {
        Ok(self
            .connection
            .unsubscribe(
                "SubscribeOrders",
                "UnsubscribeOrders",
                vec![json!(account_id)],
            )
            .await?)
    }

    /// `SubscribePositions`: position updates for one account (`GatewayUserPosition`).
    pub async fn subscribe_positions(&self, account_id: i32) -> Result<()> {
        Ok(self
            .connection
            .subscribe("SubscribePositions", vec![json!(account_id)])
            .await?)
    }

    /// `UnsubscribePositions`.
    pub async fn unsubscribe_positions(&self, account_id: i32) -> Result<()> {
        Ok(self
            .connection
            .unsubscribe(
                "SubscribePositions",
                "UnsubscribePositions",
                vec![json!(account_id)],
            )
            .await?)
    }

    /// `SubscribeTrades`: trade updates for one account (`GatewayUserTrade`).
    pub async fn subscribe_trades(&self, account_id: i32) -> Result<()> {
        Ok(self
            .connection
            .subscribe("SubscribeTrades", vec![json!(account_id)])
            .await?)
    }

    /// `UnsubscribeTrades`.
    pub async fn unsubscribe_trades(&self, account_id: i32) -> Result<()> {
        Ok(self
            .connection
            .unsubscribe(
                "SubscribeTrades",
                "UnsubscribeTrades",
                vec![json!(account_id)],
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
    use rust_decimal::Decimal;
    use serde_json::json;

    fn order_json() -> Value {
        json!({
            "id": 26060, "accountId": 545, "contractId": "CON.F.US.EP.M25", "symbolId": "F.US.EP",
            "creationTimestamp": "2025-04-14T17:49:10.142532+00:00", "updateTimestamp": null,
            "status": 2, "type": 2, "side": 0, "size": 1, "limitPrice": null, "stopPrice": null,
            "fillVolume": 1, "filledPrice": 5321.25, "customTag": null
        })
    }

    #[test]
    fn bare_envelope_and_array_forms() {
        let bare = UserEvent::from_invocation("GatewayUserOrder".into(), vec![order_json()]);
        let envelope = UserEvent::from_invocation(
            "GatewayUserOrder".into(),
            vec![json!({"action": 1, "data": order_json()})],
        );
        let array = UserEvent::from_invocation(
            "GatewayUserOrder".into(),
            vec![json!([order_json(), {"action": 2, "data": order_json()}])],
        );
        let UserEvent::Order {
            order,
            action: None,
        } = &bare[0]
        else {
            panic!("{bare:?}")
        };
        assert_eq!(order.id, 26060);
        assert_eq!(order.status, Some(OrderStatus::Filled));
        assert_eq!(order.filled_price, Some(Decimal::new(532125, 2)));
        assert!(matches!(
            &envelope[0],
            UserEvent::Order {
                action: Some(1),
                ..
            }
        ));
        assert_eq!(array.len(), 2);
        assert!(matches!(
            &array[1],
            UserEvent::Order {
                action: Some(2),
                ..
            }
        ));
    }

    #[test]
    fn position_account_and_trade_payloads() {
        let events = UserEvent::from_invocation(
            "GatewayUserPosition".into(),
            vec![
                json!({"id": 1, "accountId": 2, "contractId": "C", "type": 1, "size": 0, "averagePrice": 21000.25}),
            ],
        );
        let UserEvent::Position { position, .. } = &events[0] else {
            panic!("{events:?}")
        };
        assert_eq!(position.size, Some(0));
        assert_eq!(position.position_type, Some(PositionType::Long));

        let events = UserEvent::from_invocation(
            "GatewayUserAccount".into(),
            vec![json!({"id": 7, "name": "A", "balance": 50000, "canTrade": true})],
        );
        assert!(
            matches!(&events[0], UserEvent::Account { account, .. } if account.balance == Some(Decimal::from(50000)))
        );

        let events = UserEvent::from_invocation(
            "GatewayUserTrade".into(),
            vec![
                json!({"id": 9, "accountId": 2, "contractId": "C", "price": 1.5, "profitAndLoss": null, "orderId": 3}),
            ],
        );
        assert!(
            matches!(&events[0], UserEvent::Trade { trade, .. } if trade.profit_and_loss.is_none())
        );
    }

    #[test]
    fn unknown_targets_and_bad_payloads_are_surfaced() {
        let events = UserEvent::from_invocation("SomethingNew".into(), vec![json!(1)]);
        assert!(matches!(
            &events[0],
            UserEvent::Unknown(UnknownEvent { error: None, .. })
        ));
        let events = UserEvent::from_invocation(
            "GatewayUserOrder".into(),
            vec![json!({"id": "not a number"})],
        );
        assert!(matches!(
            &events[0],
            UserEvent::Unknown(UnknownEvent { error: Some(_), .. })
        ));
        let events = UserEvent::from_invocation("GatewayUserOrder".into(), vec![]);
        assert!(matches!(
            &events[0],
            UserEvent::Unknown(UnknownEvent { error: Some(_), .. })
        ));
    }
}
