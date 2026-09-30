//! Request and response models for every REST operation.
//!
//! Type names follow the specification's schema names; object models drop the `Model`
//! suffix (`OrderModel` is [`Order`], `HalfTradeModel` is [`HalfTrade`]). JSON field names are
//! exactly those of the specification.
//!
//! Request types implement [`Endpoint`](crate::Endpoint) and can be sent with
//! [`Client::execute`](crate::Client::execute). Optional request fields that are `None` are
//! omitted from the JSON body.

mod account;
mod auth;
mod common;
mod contract;
mod history;
mod order;
mod position;
mod trade;

pub use account::*;
pub use auth::*;
pub use common::*;
pub use contract::*;
pub use history::*;
pub use order::*;
pub use position::*;
pub use trade::*;
