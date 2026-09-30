"""Place a limit order on a SIMULATED account, then cancel it.

ORDERS ARE REAL. This example refuses to run unless TOPSTEPX_ALLOW_TRADING=1,
and it only ever uses a simulated account that can trade.

Usage::

    export TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=...
    export TOPSTEPX_ALLOW_TRADING=1
    python examples/place_order.py CONTRACT_ID LIMIT_PRICE [--size N] [--sell]

Pick a limit price far from the market so the order rests; it is cancelled at once.
"""

from __future__ import annotations

import argparse
import os
import sys
from decimal import Decimal

from _common import configure_logging, require_credentials

from topstepx import ApiError, Client, OrderSide, PlaceOrderRequest


def main(argv: list[str]) -> None:
    """Place and cancel one limit order, only when explicitly allowed."""
    if os.environ.get("TOPSTEPX_ALLOW_TRADING") != "1":
        sys.exit(
            "Refusing to place orders: orders are real. Set TOPSTEPX_ALLOW_TRADING=1 to run "
            "this example (it only uses simulated accounts)."
        )
    parser = argparse.ArgumentParser(description=__doc__.split("\n", 1)[0])
    parser.add_argument("contract_id")
    parser.add_argument("limit_price", type=Decimal)
    parser.add_argument("--size", type=int, default=1)
    parser.add_argument("--sell", action="store_true")
    args = parser.parse_args(argv)

    require_credentials()
    configure_logging()
    with Client.from_env() as client:
        accounts = [a for a in client.accounts.search() if a.simulated and a.can_trade]
        if not accounts:
            sys.exit("No simulated account that can trade was found; refusing to trade.")
        account = accounts[0]
        side = OrderSide.Ask if args.sell else OrderSide.Bid
        request = PlaceOrderRequest.limit(
            account.id, args.contract_id, side, args.size, args.limit_price
        )
        print(f"Placing on simulated account {account.name} ({account.id}): {request}")
        try:
            order_id = client.orders.place(request)
        except ApiError as error:
            # A rejected placement may still carry an orderId; it does not mean acceptance.
            sys.exit(f"Rejected: {error} (orderId={error.order_id})")
        print(f"Placed order {order_id}; cancelling it")
        client.orders.cancel(account.id, order_id)
        print("Cancelled.")


if __name__ == "__main__":
    main(sys.argv[1:])
