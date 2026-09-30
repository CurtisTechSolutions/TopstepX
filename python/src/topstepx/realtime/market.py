"""The SignalR market hub: quotes, market trades and market depth."""

from __future__ import annotations

from .._config import DEFAULT_MARKET_HUB_URL
from ..async_client import AsyncClient
from ..client import Client
from .connection import HubConnection, HubOptions, TokenSource
from .events import decode_market_event

__all__ = ["MarketHub"]


class MarketHub(HubConnection):
    """The market hub (``/hubs/market``).

    Events: :class:`~topstepx.realtime.QuoteEvent`,
    :class:`~topstepx.realtime.MarketTradeEvent` and
    :class:`~topstepx.realtime.DepthEvent`, plus state and unknown events. Each
    carries the ``contract_id`` from the event arguments.

    Example::

        async with MarketHub(client) as hub:
            await hub.subscribe_contract_quotes("CON.F.US.EP.U25")
            async for event in hub.events():
                if isinstance(event, QuoteEvent):
                    print(event.contract_id, event.quote.last_price)

    Args:
        source: Token source: an :class:`~topstepx.AsyncClient` or
            :class:`~topstepx.Client`, a callable returning a token (sync or
            async), or a fixed token string.
        url: Hub URL; defaults to the client's ``market_hub_url`` or
            ``https://rtc.topstepx.com/hubs/market``.
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
                url = source.market_hub_url
            else:
                url = DEFAULT_MARKET_HUB_URL
        super().__init__(url, source, decoder=decode_market_event, options=options, name="market")

    async def subscribe_contract_quotes(self, contract_id: str) -> None:
        """Subscribe to quotes of a contract (``SubscribeContractQuotes``).

        Args:
            contract_id: Contract id.
        """
        await self._subscribe("SubscribeContractQuotes", str(contract_id))

    async def unsubscribe_contract_quotes(self, contract_id: str) -> None:
        """Unsubscribe from quotes (``UnsubscribeContractQuotes``).

        Args:
            contract_id: Contract id.
        """
        await self._unsubscribe(
            "UnsubscribeContractQuotes", "SubscribeContractQuotes", str(contract_id)
        )

    async def subscribe_contract_trades(self, contract_id: str) -> None:
        """Subscribe to market trades of a contract (``SubscribeContractTrades``).

        Args:
            contract_id: Contract id.
        """
        await self._subscribe("SubscribeContractTrades", str(contract_id))

    async def unsubscribe_contract_trades(self, contract_id: str) -> None:
        """Unsubscribe from market trades (``UnsubscribeContractTrades``).

        Args:
            contract_id: Contract id.
        """
        await self._unsubscribe(
            "UnsubscribeContractTrades", "SubscribeContractTrades", str(contract_id)
        )

    async def subscribe_contract_market_depth(self, contract_id: str) -> None:
        """Subscribe to market depth of a contract (``SubscribeContractMarketDepth``).

        Args:
            contract_id: Contract id.
        """
        await self._subscribe("SubscribeContractMarketDepth", str(contract_id))

    async def unsubscribe_contract_market_depth(self, contract_id: str) -> None:
        """Unsubscribe from market depth (``UnsubscribeContractMarketDepth``).

        Args:
            contract_id: Contract id.
        """
        await self._unsubscribe(
            "UnsubscribeContractMarketDepth", "SubscribeContractMarketDepth", str(contract_id)
        )
