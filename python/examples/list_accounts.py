"""List your trading accounts (read-only).

Usage::

    export TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=...
    python examples/list_accounts.py [--all]
"""

from __future__ import annotations

import sys

from _common import configure_logging, require_credentials

from topstepx import Client


def main(argv: list[str]) -> None:
    """Print the accounts; pass ``--all`` to include inactive ones."""
    require_credentials()
    configure_logging()
    only_active = "--all" not in argv
    with Client.from_env() as client:
        accounts = client.accounts.search(only_active_accounts=only_active)
        if not accounts:
            print("No accounts found.")
        for account in accounts:
            kind = "simulated" if account.simulated else "LIVE"
            print(
                f"{account.id:>10}  {account.name:<32} balance={account.balance:>14}  "
                f"{kind:<9} can_trade={account.can_trade}"
            )


if __name__ == "__main__":
    main(sys.argv[1:])
