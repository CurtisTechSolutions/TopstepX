//! Places a limit buy 20 ticks below the last close, then cancels it.
//!
//! **This sends real orders.** It refuses to run unless `TOPSTEPX_ALLOW_TRADING=1` is set.
//! Use a simulated account.
//!
//! ```sh
//! TOPSTEPX_ALLOW_TRADING=1 TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... \
//!   cargo run --example place_order -- ACCOUNT_ID ES
//! ```

use chrono::{Duration, Utc};
use topstepx::enums::{AggregateBarUnit, OrderSide};
use topstepx::models::{PlaceOrderRequest, RetrieveBarRequest};
use topstepx::{Client, Decimal, Error};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    if std::env::var("TOPSTEPX_ALLOW_TRADING").as_deref() != Ok("1") {
        eprintln!("refusing to place orders: set TOPSTEPX_ALLOW_TRADING=1 to run this example");
        std::process::exit(2);
    }
    let mut args = std::env::args().skip(1);
    let account_id: i32 = args
        .next()
        .ok_or("usage: place_order ACCOUNT_ID [SEARCH]")?
        .parse()?;
    let search = args.next().unwrap_or_else(|| "ES".to_owned());

    let client = Client::from_env()?;
    let contracts = client.contracts().search(&search, false).await?;
    let contract = contracts
        .iter()
        .find(|c| c.active_contract)
        .or(contracts.first())
        .ok_or("no matching contract")?;

    let end = Utc::now();
    let request = RetrieveBarRequest::new(
        &contract.id,
        AggregateBarUnit::Minute,
        1,
        end - Duration::days(3),
        end,
        1,
    );
    let last_close = client
        .history()
        .retrieve_bars(&request)
        .await?
        .last()
        .map(|bar| bar.close)
        .ok_or("no recent bars")?;
    let price = contract.round_to_tick(last_close - contract.tick_size * Decimal::from(20));

    let order = PlaceOrderRequest::limit(account_id, &contract.id, OrderSide::Bid, 1, price)
        .with_custom_tag(format!("topstepx-rust-example-{}", end.timestamp_millis()));
    match client.orders().place(&order).await {
        Ok(order_id) => {
            println!("placed order {order_id}: buy 1 {} @ {price}", contract.name);
            client.orders().cancel(account_id, order_id).await?;
            println!("cancelled order {order_id}");
        }
        Err(Error::Api(err)) => {
            // A rejected order can still carry an id; it does not mean the order is working.
            println!("placement rejected: {err}");
        }
        Err(other) => return Err(other.into()),
    }
    Ok(())
}
