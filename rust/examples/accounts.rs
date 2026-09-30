//! Lists accounts with their open positions and orders.
//!
//! ```sh
//! TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... cargo run --example accounts
//! ```

use topstepx::Client;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = Client::from_env()?;

    let accounts = client.accounts().search(true).await?;
    if accounts.is_empty() {
        println!("no active accounts");
    }
    for account in accounts {
        println!(
            "account {} {:?}: balance {} (can trade: {}, simulated: {})",
            account.id, account.name, account.balance, account.can_trade, account.simulated
        );
        for position in client.positions().search_open(account.id).await? {
            println!(
                "  position {} {:?} x{} @ {}",
                position.contract_id, position.position_type, position.size, position.average_price
            );
        }
        for order in client.orders().search_open(account.id).await? {
            println!(
                "  order {} {:?} {:?} {} x{} limit={:?} stop={:?}",
                order.id,
                order.side,
                order.order_type,
                order.contract_id,
                order.size,
                order.limit_price,
                order.stop_price
            );
        }
    }
    Ok(())
}
