"""Stream quotes, market trades and depth for one contract (read-only, asyncio).

Usage::

    export TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=...
    python examples/realtime_market.py [CONTRACT_ID] [SECONDS]

Without a contract id, the first active contract matching "ES" is used.
"""

from __future__ import annotations

import asyncio
import contextlib
import sys

from _common import configure_logging, require_credentials

from topstepx import AsyncClient, MarketHub
from topstepx.realtime import DepthEvent, MarketTradeEvent, QuoteEvent, StateEvent


async def main(contract_id: str | None, seconds: float | None) -> None:
    """Print market-hub events for one contract."""
    require_credentials()
    configure_logging()
    async with AsyncClient.from_env() as client:
        if contract_id is None:
            contracts = await client.contracts.search("ES")
            active = [c for c in contracts if c.active_contract] or contracts
            if not active:
                sys.exit("No contract found; pass a contract id.")
            contract_id = active[0].id
        print(f"Streaming {contract_id} (Ctrl+C to stop)")

        def on_quote(event: QuoteEvent) -> None:
            quote = event.quote
            print(f"quote  last={quote.last_price} bid={quote.best_bid} ask={quote.best_ask}")

        def on_trades(event: MarketTradeEvent) -> None:
            print("trades " + ", ".join(f"{t.volume}@{t.price}" for t in event.trades))

        def on_depth(event: DepthEvent) -> None:
            print(f"depth  {len(event.entries)} level update(s)")

        # Callbacks run in their own task; the event stream is also available.
        hub = MarketHub(client)
        hub.on(QuoteEvent, on_quote)
        hub.on(MarketTradeEvent, on_trades)
        hub.on(DepthEvent, on_depth)
        hub.on(StateEvent, lambda event: print(f"({event})"))
        async with hub:
            await hub.subscribe_contract_quotes(contract_id)
            await hub.subscribe_contract_trades(contract_id)
            await hub.subscribe_contract_market_depth(contract_id)
            with contextlib.suppress(asyncio.TimeoutError):
                await asyncio.wait_for(hub.wait_closed(), seconds)


if __name__ == "__main__":
    arguments = sys.argv[1:]
    contract = arguments[0] if arguments else None
    duration = float(arguments[1]) if len(arguments) > 1 else None
    with contextlib.suppress(KeyboardInterrupt):
        asyncio.run(main(contract, duration))
