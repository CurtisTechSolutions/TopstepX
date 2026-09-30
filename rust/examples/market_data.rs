//! Searches contracts and prints the last day of 5-minute bars for the first match.
//!
//! ```sh
//! TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... cargo run --example market_data -- NQ
//! ```

use chrono::{Duration, Utc};
use topstepx::enums::AggregateBarUnit;
use topstepx::models::RetrieveBarRequest;
use topstepx::Client;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let search = std::env::args().nth(1).unwrap_or_else(|| "ES".to_owned());
    let client = Client::from_env()?;

    // `live` selects the market-data subscription (false = simulated data).
    let contracts = client.contracts().search(&search, false).await?;
    for contract in &contracts {
        println!(
            "{} {} tick={} value={} active={}",
            contract.id,
            contract.description,
            contract.tick_size,
            contract.tick_value,
            contract.active_contract
        );
    }
    let Some(contract) = contracts
        .iter()
        .find(|c| c.active_contract)
        .or(contracts.first())
    else {
        println!("no contract matches {search:?}");
        return Ok(());
    };

    let end = Utc::now();
    let request = RetrieveBarRequest::new(
        &contract.id,
        AggregateBarUnit::Minute,
        5,
        end - Duration::days(1),
        end,
        500,
    );
    let bars = client.history().retrieve_bars(&request).await?;
    println!("{} bars for {}", bars.len(), contract.name);
    for bar in bars.iter().take(10) {
        println!(
            "{} o={} h={} l={} c={} v={}",
            bar.timestamp, bar.open, bar.high, bar.low, bar.close, bar.volume
        );
    }
    Ok(())
}
