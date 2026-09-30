//! Endpoint groups returned by [`Client`] (`client.orders()`, `client.contracts()`, ...).
//!
//! Every method checks the gateway's business result and returns [`Error::Api`] on
//! failure. Use [`Client::execute_raw`] for the unchecked response envelope.

use chrono::{DateTime, Utc};

use crate::client::{AuthMode, Client};
use crate::error::{Error, Result};
use crate::models::*;
use crate::operation::{GatewayResponse, Operation};

macro_rules! group {
    ($(#[$meta:meta])* $name:ident) => {
        $(#[$meta])*
        #[derive(Debug, Clone, Copy)]
        pub struct $name<'a> {
            client: &'a Client,
        }

        impl<'a> $name<'a> {
            pub(crate) fn new(client: &'a Client) -> Self {
                $name { client }
            }
        }
    };
}

group! {
    /// Authentication: `/api/Auth/*`.
    Auth
}

impl Auth<'_> {
    /// Logs in with a username and API key (`POST /api/Auth/loginKey`), stores the token and
    /// returns it.
    ///
    /// The credentials are not remembered; configure them on the builder to enable automatic
    /// re-login.
    pub async fn login_key(
        &self,
        user_name: impl Into<String>,
        api_key: impl Into<String>,
    ) -> Result<String> {
        let request = LoginApiKeyRequest::new(user_name, api_key);
        let _guard = self.client.inner.session.refresh.lock().await;
        let token = self.client.login_request(&request).await?;
        self.client.inner.session.store(token.clone());
        Ok(token)
    }

    /// Application login (`POST /api/Auth/loginApp`). Stores the token and returns it.
    ///
    /// **Not supported by the current authentication guide**, which documents API-key login
    /// as the only way to obtain a token. The operation exists in Swagger, but its
    /// availability is unverified. Prefer [`login_key`](Self::login_key).
    pub async fn login_app(&self, request: &LoginAppRequest) -> Result<String> {
        let _guard = self.client.inner.session.refresh.lock().await;
        let token = self.client.login_request(request).await?;
        self.client.inner.session.store(token.clone());
        Ok(token)
    }

    /// Renews the stored token (`POST /api/Auth/validate`), stores the new token and returns
    /// it. Renew before expiry; after expiry, log in again.
    pub async fn validate(&self) -> Result<String> {
        let _guard = self.client.inner.session.refresh.lock().await;
        let state = self
            .client
            .inner
            .session
            .current()
            .ok_or(Error::NotAuthenticated)?;
        let renewed = self.client.validate_locked(&state.token).await?;
        Ok(renewed.token.to_string())
    }

    /// Ends the session (`POST /api/Auth/logout`) and forgets the stored token, even when
    /// the call fails. The effect on other sessions is not documented.
    pub async fn logout(&self) -> Result<()> {
        let _guard = self.client.inner.session.refresh.lock().await;
        let state = self
            .client
            .inner
            .session
            .current()
            .ok_or(Error::NotAuthenticated)?;
        let result = self
            .client
            .request_json::<LogoutResponse>(
                Operation::AuthLogout,
                None,
                AuthMode::Explicit(&state.token),
            )
            .await;
        self.client.inner.session.clear();
        result?.check(Operation::AuthLogout)?;
        Ok(())
    }
}

group! {
    /// Accounts: `/api/Account/*`.
    Accounts
}

impl Accounts<'_> {
    /// Lists trading accounts (`POST /api/Account/search`).
    pub async fn search(&self, only_active_accounts: bool) -> Result<Vec<TradingAccount>> {
        Ok(self
            .client
            .execute(&SearchAccountRequest::new(only_active_accounts))
            .await?
            .accounts)
    }
}

group! {
    /// Contracts: `/api/Contract/*`.
    Contracts
}

impl Contracts<'_> {
    /// Searches contracts by name (`POST /api/Contract/search`); at most 20 results. `live`
    /// selects the market-data subscription, not the account type.
    pub async fn search(
        &self,
        search_text: impl Into<String>,
        live: bool,
    ) -> Result<Vec<Contract>> {
        Ok(self
            .client
            .execute(&SearchContractRequest::new(search_text, live))
            .await?
            .contracts)
    }

    /// Looks up one contract (`POST /api/Contract/searchById`).
    pub async fn search_by_id(&self, contract_id: impl Into<String>) -> Result<Contract> {
        self.client
            .execute(&SearchContractByIdRequest::new(contract_id))
            .await?
            .contract
            .ok_or(Error::MissingData {
                operation: Operation::ContractSearchById,
                field: "contract",
            })
    }

    /// Lists contracts available on the live or simulated data subscription
    /// (`POST /api/Contract/available`).
    pub async fn available(&self, live: bool) -> Result<Vec<Contract>> {
        Ok(self
            .client
            .execute(&ListAvailableContractRequest::new(live))
            .await?
            .contracts)
    }
}

group! {
    /// Historical data: `/api/History/*`.
    History
}

impl History<'_> {
    /// Retrieves bars (`POST /api/History/retrieveBars`). At most 20,000 bars per request;
    /// rate limited to 50 requests per 30 seconds.
    pub async fn retrieve_bars(&self, request: &RetrieveBarRequest) -> Result<Vec<AggregateBar>> {
        Ok(self.client.execute(request).await?.bars)
    }
}

/// One page of [`Orders::query`] results.
#[derive(Debug, Clone, PartialEq)]
#[non_exhaustive]
pub struct OrderPage {
    /// Orders on this page.
    pub orders: Vec<Order>,
    /// Total number of matches, when requested with `include_total_count`.
    pub total_count: Option<i32>,
}

group! {
    /// Orders: `/api/Order/*`.
    Orders
}

impl Orders<'_> {
    /// Searches orders by creation time (`POST /api/Order/search`).
    pub async fn search(&self, request: &SearchOrderRequest) -> Result<Vec<Order>> {
        Ok(self.client.execute(request).await?.orders)
    }

    /// Searches orders created at or after `start` (and before `end`, when given).
    pub async fn search_range(
        &self,
        account_id: i32,
        start: DateTime<Utc>,
        end: Option<DateTime<Utc>>,
    ) -> Result<Vec<Order>> {
        let request = SearchOrderRequest {
            account_id,
            start_timestamp: start,
            end_timestamp: end,
        };
        self.search(&request).await
    }

    /// Looks up one order (`POST /api/Order/searchById`, Swagger-only).
    pub async fn search_by_id(&self, account_id: i32, order_id: i64) -> Result<Order> {
        self.client
            .execute(&SearchOrderByIdRequest {
                account_id,
                order_id,
            })
            .await?
            .order
            .ok_or(Error::MissingData {
                operation: Operation::OrderSearchById,
                field: "order",
            })
    }

    /// Lists open orders (`POST /api/Order/searchOpen`).
    pub async fn search_open(&self, account_id: i32) -> Result<Vec<Order>> {
        Ok(self
            .client
            .execute(&SearchOpenOrderRequest { account_id })
            .await?
            .orders)
    }

    /// Filters and paginates orders (`POST /api/Order/v2/query`, Swagger-only).
    pub async fn query(&self, request: &SearchOrdersQueryRequest) -> Result<OrderPage> {
        let response = self.client.execute(request).await?;
        Ok(OrderPage {
            orders: response.orders,
            total_count: response.total_count,
        })
    }

    /// Places an order (`POST /api/Order/place`) and returns its id.
    ///
    /// On rejection the error's [`ApiError::order_id`](crate::ApiError::order_id) carries any
    /// id the gateway returned; that id does not mean the order was accepted.
    pub async fn place(&self, request: &PlaceOrderRequest) -> Result<i64> {
        self.client
            .execute(request)
            .await?
            .order_id
            .ok_or(Error::MissingData {
                operation: Operation::OrderPlace,
                field: "orderId",
            })
    }

    /// Cancels an order (`POST /api/Order/cancel`). Documented for simulated accounts that are
    /// not copy-trading followers.
    pub async fn cancel(&self, account_id: i32, order_id: i64) -> Result<()> {
        self.client
            .execute(&CancelOrderRequest {
                account_id,
                order_id,
            })
            .await?;
        Ok(())
    }

    /// Modifies an open order (`POST /api/Order/modify`).
    pub async fn modify(&self, request: &ModifyOrderRequest) -> Result<()> {
        self.client.execute(request).await?;
        Ok(())
    }
}

group! {
    /// Positions: `/api/Position/*`.
    Positions
}

impl Positions<'_> {
    /// Lists open positions (`POST /api/Position/searchOpen`).
    pub async fn search_open(&self, account_id: i32) -> Result<Vec<Position>> {
        Ok(self
            .client
            .execute(&SearchPositionRequest { account_id })
            .await?
            .positions)
    }

    /// Closes the whole position in a contract (`POST /api/Position/closeContract`).
    pub async fn close(&self, account_id: i32, contract_id: impl Into<String>) -> Result<()> {
        let request = CloseContractPositionRequest {
            account_id,
            contract_id: contract_id.into(),
        };
        self.client.execute(&request).await?;
        Ok(())
    }

    /// Closes `size` contracts of a position (`POST /api/Position/partialCloseContract`);
    /// `size` must be at least 1 and at most the open size.
    pub async fn partial_close(
        &self,
        account_id: i32,
        contract_id: impl Into<String>,
        size: i32,
    ) -> Result<()> {
        let request = PartialCloseContractPositionRequest {
            account_id,
            contract_id: contract_id.into(),
            size,
        };
        self.client.execute(&request).await?;
        Ok(())
    }
}

group! {
    /// Trades: `/api/Trade/*`.
    Trades
}

impl Trades<'_> {
    /// Searches executed trades (`POST /api/Trade/search`).
    pub async fn search(&self, request: &SearchTradeRequest) -> Result<Vec<HalfTrade>> {
        Ok(self.client.execute(request).await?.trades)
    }

    /// Searches trades executed at or after `start` (and before `end`, when given).
    pub async fn search_range(
        &self,
        account_id: i32,
        start: DateTime<Utc>,
        end: Option<DateTime<Utc>>,
    ) -> Result<Vec<HalfTrade>> {
        let request = SearchTradeRequest {
            account_id,
            start_timestamp: start,
            end_timestamp: end,
        };
        self.search(&request).await
    }
}

group! {
    /// Service status: `/api/Status/*`.
    Status
}

impl Status<'_> {
    /// Checks API responsiveness (`GET /api/Status/ping`, Swagger-only); returns the body,
    /// normally `pong`. Sends the session token when one is held or obtainable.
    pub async fn ping(&self) -> Result<String> {
        self.client.ping().await
    }
}
