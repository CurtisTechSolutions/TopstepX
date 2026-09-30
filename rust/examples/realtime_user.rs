//! Streams account, order, position and trade updates until Ctrl-C.
//!
//! ```sh
//! TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... cargo run --example realtime_user [ACCOUNT_ID]
//! ```

use tokio::sync::broadcast::error::RecvError;
use topstepx::realtime::UserEvent;
use topstepx::Client;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = Client::from_env()?;
    let account_id = match std::env::args().nth(1) {
        Some(id) => id.parse()?,
        None => {
            client
                .accounts()
                .search(true)
                .await?
                .first()
                .ok_or("no active account")?
                .id
        }
    };

    let hub = client.connect_user_hub().await?;
    let mut events = hub.events();
    hub.subscribe_accounts().await?;
    hub.subscribe_orders(account_id).await?;
    hub.subscribe_positions(account_id).await?;
    hub.subscribe_trades(account_id).await?;
    println!("streaming updates for account {account_id}; press Ctrl-C to stop");

    loop {
        tokio::select! {
            _ = tokio::signal::ctrl_c() => break,
            event = events.recv() => match event {
                Ok(UserEvent::Account { account, .. }) => println!("account {} balance {:?}", account.id, account.balance),
                Ok(UserEvent::Order { order, .. }) => println!(
                    "order {} {:?} {:?} {:?} filled {:?}@{:?}",
                    order.id, order.status, order.side, order.order_type, order.fill_volume, order.filled_price
                ),
                Ok(UserEvent::Position { position, .. }) => println!(
                    "position {} {:?} size {:?} avg {:?}",
                    position.contract_id, position.position_type, position.size, position.average_price
                ),
                Ok(UserEvent::Trade { trade, .. }) => println!(
                    "trade {} {:?} {:?}@{:?} pnl {:?}",
                    trade.id, trade.side, trade.size, trade.price, trade.profit_and_loss
                ),
                Ok(UserEvent::Connection(state)) => println!("connection: {state:?}"),
                Ok(other) => println!("other: {other:?}"),
                Err(RecvError::Lagged(skipped)) => eprintln!("fell behind; skipped {skipped} events"),
                Err(RecvError::Closed) => break,
            },
        }
    }
    hub.close().await;
    Ok(())
}
