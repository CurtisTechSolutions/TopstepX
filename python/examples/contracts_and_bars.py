"""Search contracts and download recent 5-minute bars (read-only, asyncio).

Usage::

    export TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=...
    python examples/contracts_and_bars.py [SEARCH_TEXT]      # default: ES
"""

from __future__ import annotations

import asyncio
import sys
from datetime import datetime, timedelta, timezone

from _common import configure_logging, require_credentials

from topstepx import AggregateBarUnit, AsyncClient, RetrieveBarRequest


async def main(search_text: str) -> None:
    """Print the contracts matching ``search_text`` and bars of the active one."""
    require_credentials()
    configure_logging()
    async with AsyncClient.from_env() as client:
        contracts = await client.contracts.search(search_text, live=False)
        if not contracts:
            print(f"No contracts match {search_text!r}.")
            return
        for contract in contracts:
            marker = "*" if contract.active_contract else " "
            print(
                f"{marker} {contract.id:<24} {contract.name:<8} tick={contract.tick_size} "
                f"(${contract.tick_value}/tick)  {contract.description}"
            )

        contract = next((c for c in contracts if c.active_contract), contracts[0])
        end = datetime.now(timezone.utc)
        request = RetrieveBarRequest(
            contract_id=contract.id,
            start_time=end - timedelta(days=1),
            end_time=end,
            unit=AggregateBarUnit.Minute,
            unit_number=5,
            limit=500,
        )
        bars = await client.history.retrieve_bars(request)
        print(f"\n{len(bars)} five-minute bars for {contract.name} over the last 24 hours:")
        for bar in bars[:20]:
            print(
                f"{bar.timestamp:%Y-%m-%d %H:%M}Z  O={bar.open} H={bar.high} L={bar.low} "
                f"C={bar.close} V={bar.volume}"
            )
        if len(bars) > 20:
            print(f"... and {len(bars) - 20} more")


if __name__ == "__main__":
    asyncio.run(main(sys.argv[1] if len(sys.argv) > 1 else "ES"))
