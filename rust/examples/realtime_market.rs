//! Streams quotes, trades and depth for one contract until Ctrl-C.
//!
//! ```sh
//! TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... cargo run --example realtime_market -- ES
//! ```

use tokio::sync::broadcast::error::RecvError;
use topstepx::realtime::{GatewayQuote, MarketEvent};
use topstepx::Client;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let search = std::env::args().nth(1).unwrap_or_else(|| "ES".to_owned());
    let client = Client::from_env()?;
    let contracts = client.contracts().search(&search, false).await?;
    let contract = contracts
        .iter()
        .find(|c| c.active_contract)
        .or(contracts.first())
        .ok_or("no matching contract")?;

    let hub = client.connect_market_hub().await?;
    let mut events = hub.events();
    hub.subscribe_contract_quotes(&contract.id).await?;
    hub.subscribe_contract_trades(&contract.id).await?;
    hub.subscribe_contract_market_depth(&contract.id).await?;
    println!(
        "streaming {} ({}); press Ctrl-C to stop",
        contract.name, contract.id
    );

    // Quote updates can be partial, so fold them into a running quote.
    let mut quote = GatewayQuote::default();
    loop {
        tokio::select! {
            _ = tokio::signal::ctrl_c() => break,
            event = events.recv() => match event {
                Ok(MarketEvent::Quote { quote: update, .. }) => {
                    quote.merge(&update);
                    println!("quote bid {:?} ask {:?} last {:?}", quote.best_bid, quote.best_ask, quote.last_price);
                }
                Ok(MarketEvent::Trades { trades, .. }) => {
                    for trade in trades {
                        println!("trade {:?} x{:?} ({:?})", trade.price, trade.volume, trade.trade_type);
                    }
                }
                Ok(MarketEvent::Depth { entries, .. }) => println!("depth: {} level change(s)", entries.len()),
                Ok(MarketEvent::Connection(state)) => println!("connection: {state:?}"),
                Ok(other) => println!("other: {other:?}"),
                Err(RecvError::Lagged(skipped)) => eprintln!("fell behind; skipped {skipped} events"),
                Err(RecvError::Closed) => break,
            },
        }
    }
    hub.close().await;
    Ok(())
}
