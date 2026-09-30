"""The SignalR user hub: account, order, position and trade updates."""

from __future__ import annotations

from .._config import DEFAULT_USER_HUB_URL
from ..async_client import AsyncClient
from ..client import Client
from .connection import HubConnection, HubOptions, TokenSource
from .events import decode_user_event

__all__ = ["UserHub"]


class UserHub(HubConnection):
    """The user hub (``/hubs/user``).

    Events: :class:`~topstepx.realtime.UserAccountEvent`,
    :class:`~topstepx.realtime.UserOrderEvent`,
    :class:`~topstepx.realtime.UserPositionEvent` and
    :class:`~topstepx.realtime.UserTradeEvent`, plus state and unknown events.

    Example::

        async with AsyncClient.from_env() as client, UserHub(client) as hub:
            await hub.subscribe_orders(account_id)
            async for event in hub.events():
                if isinstance(event, UserOrderEvent):
                    print(event.order.id, event.order.status)

    Successful subscriptions are recorded and replayed, in order, after every
    reconnect.

    Args:
        source: Token source: an :class:`~topstepx.AsyncClient` or
            :class:`~topstepx.Client` (its ``get_token()`` runs ``ensure_token``
            for every connection attempt), a callable returning a token (sync or
            async), or a fixed token string.
        url: Hub URL; defaults to the client's ``user_hub_url`` or
            ``https://rtc.topstepx.com/hubs/user``.
        options: Connection tuning.
    """

    def __init__(
        self,
        source: TokenSource,
        *,
        url: str | None = None,
        options: HubOptions | None = None,
    ) -> None:
        if url is None:
            if isinstance(source, (Client, AsyncClient)):
                url = source.user_hub_url
            else:
                url = DEFAULT_USER_HUB_URL
        super().__init__(url, source, decoder=decode_user_event, options=options, name="user")

    async def subscribe_accounts(self) -> None:
        """Subscribe to account updates (``SubscribeAccounts``)."""
        await self._subscribe("SubscribeAccounts")

    async def unsubscribe_accounts(self) -> None:
        """Unsubscribe from account updates (``UnsubscribeAccounts``)."""
        await self._unsubscribe("UnsubscribeAccounts", "SubscribeAccounts")

    async def subscribe_orders(self, account_id: int) -> None:
        """Subscribe to order updates of an account (``SubscribeOrders``).

        Args:
            account_id: Account id.
        """
        await self._subscribe("SubscribeOrders", int(account_id))

    async def unsubscribe_orders(self, account_id: int) -> None:
        """Unsubscribe from order updates (``UnsubscribeOrders``).

        Args:
            account_id: Account id.
        """
        await self._unsubscribe("UnsubscribeOrders", "SubscribeOrders", int(account_id))

    async def subscribe_positions(self, account_id: int) -> None:
        """Subscribe to position updates of an account (``SubscribePositions``).

        Args:
            account_id: Account id.
        """
        await self._subscribe("SubscribePositions", int(account_id))

    async def unsubscribe_positions(self, account_id: int) -> None:
        """Unsubscribe from position updates (``UnsubscribePositions``).

        Args:
            account_id: Account id.
        """
        await self._unsubscribe("UnsubscribePositions", "SubscribePositions", int(account_id))

    async def subscribe_trades(self, account_id: int) -> None:
        """Subscribe to trade updates of an account (``SubscribeTrades``).

        Args:
            account_id: Account id.
        """
        await self._subscribe("SubscribeTrades", int(account_id))

    async def unsubscribe_trades(self, account_id: int) -> None:
        """Unsubscribe from trade updates (``UnsubscribeTrades``).

        Args:
            account_id: Account id.
        """
        await self._unsubscribe("UnsubscribeTrades", "SubscribeTrades", int(account_id))
