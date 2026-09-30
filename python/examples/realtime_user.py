"""Stream account, order, position and trade updates (read-only, asyncio).

Usage::

    export TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=...
    python examples/realtime_user.py [SECONDS]      # default: run until Ctrl+C
"""

from __future__ import annotations

import asyncio
import contextlib
import sys

from _common import configure_logging, require_credentials

from topstepx import AsyncClient, UserHub
from topstepx.realtime import (
    Closed,
    Reconnecting,
    StateEvent,
    UnknownEvent,
    UserAccountEvent,
    UserOrderEvent,
    UserPositionEvent,
    UserTradeEvent,
)


async def main(seconds: float | None) -> None:
    """Print user-hub events for ``seconds`` (or until interrupted)."""
    require_credentials()
    configure_logging()
    async with AsyncClient.from_env() as client:
        accounts = await client.accounts.search()
        # The hub takes its token from the client (ensure_token) on every connect.
        async with UserHub(client) as hub:
            await hub.subscribe_accounts()
            for account in accounts:
                await hub.subscribe_orders(account.id)
                await hub.subscribe_positions(account.id)
                await hub.subscribe_trades(account.id)
            print(f"Subscribed to {len(accounts)} account(s); waiting for updates...")

            async def consume() -> None:
                async for event in hub.events():
                    if isinstance(event, UserAccountEvent):
                        print(f"account  {event.account.id}: balance={event.account.balance}")
                    elif isinstance(event, UserOrderEvent):
                        order = event.order
                        status = order.status.name if order.status is not None else "?"
                        print(f"order    {order.id}: {status} size={order.size}")
                    elif isinstance(event, UserPositionEvent):
                        position = event.position
                        print(f"position {position.id}: size={position.size}")
                    elif isinstance(event, UserTradeEvent):
                        trade = event.trade
                        print(f"trade    {trade.id}: {trade.size} @ {trade.price}")
                    elif isinstance(event, Reconnecting):
                        print(f"(reconnecting, attempt {event.attempt}: {event.cause})")
                    elif isinstance(event, Closed):
                        print(f"(closed: {event.cause})")
                    elif isinstance(event, StateEvent):
                        print(f"({type(event).__name__})")
                    elif isinstance(event, UnknownEvent):
                        print(f"(unhandled {event.target}: {event.error})")

            with contextlib.suppress(asyncio.TimeoutError):
                await asyncio.wait_for(consume(), seconds)
            if hub.dropped_events:
                print(f"{hub.dropped_events} event(s) were dropped because printing fell behind")


if __name__ == "__main__":
    duration = float(sys.argv[1]) if len(sys.argv) > 1 else None
    with contextlib.suppress(KeyboardInterrupt):
        asyncio.run(main(duration))
