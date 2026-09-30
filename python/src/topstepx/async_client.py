"""Asynchronous (asyncio) REST client.

:class:`AsyncClient` mirrors :class:`~topstepx.Client` exactly: same
constructor, same endpoint groups, same method names; every method is a
coroutine. Both clients run the same shared request pipeline, so their
behavior is identical.
"""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable, Mapping
from datetime import timedelta
from types import TracebackType
from typing import TYPE_CHECKING, Any, TypeVar, overload

import httpx

from . import operations as ops
from ._auth import make_credentials
from ._base import (
    ACQUIRE_AUTH_LOCK,
    ClientCore,
    Flow,
    SendRequest,
    Sleep,
    require,
    resolve_target,
)
from ._config import (
    DEFAULT_BASE_URL,
    DEFAULT_MARKET_HUB_URL,
    DEFAULT_MAX_RETRIES,
    DEFAULT_REFRESH_MARGIN,
    DEFAULT_TIMEOUT,
    DEFAULT_USER_HUB_URL,
    Backoff,
    RateLimits,
    build_settings,
    env_overrides,
)
from .models import (
    AggregateBar,
    CancelOrderRequest,
    CloseContractPositionRequest,
    Contract,
    HalfTrade,
    ListAvailableContractRequest,
    LoginAppRequest,
    ModifyOrderRequest,
    Order,
    OrderQueryResult,
    PartialCloseContractPositionRequest,
    PlaceOrderRequest,
    Position,
    RequestModel,
    ResponseEnvelope,
    RetrieveBarRequest,
    SearchAccountRequest,
    SearchContractByIdRequest,
    SearchContractRequest,
    SearchOpenOrderRequest,
    SearchOrderByIdRequest,
    SearchOrderRequest,
    SearchOrdersQueryRequest,
    SearchPositionRequest,
    SearchTradeRequest,
    TradingAccount,
)

if TYPE_CHECKING:
    from typing_extensions import Self

__all__ = [
    "AsyncAccountsAPI",
    "AsyncAuthAPI",
    "AsyncClient",
    "AsyncContractsAPI",
    "AsyncHistoryAPI",
    "AsyncOrdersAPI",
    "AsyncPositionsAPI",
    "AsyncStatusAPI",
    "AsyncTradesAPI",
]

T = TypeVar("T")
RespT = TypeVar("RespT")
EnvT = TypeVar("EnvT", bound=ResponseEnvelope)


class AsyncClient:
    """Asynchronous client of the ProjectX Gateway REST API.

    The asyncio twin of :class:`~topstepx.Client`; see it for the full
    description of the constructor arguments and behavior. Use it as an async
    context manager (or call :meth:`aclose`)::

        async with AsyncClient(username="me", api_key="...") as client:
            accounts = await client.accounts.search()

    Args:
        username: Platform username (use together with ``api_key``).
        api_key: API key (a secret; never logged or shown in ``repr``).
        token: A pre-existing token, instead of or in addition to credentials.
        base_url: REST base URL.
        user_hub_url: SignalR user hub URL (used by ``UserHub(client)``).
        market_hub_url: SignalR market hub URL (used by ``MarketHub(client)``).
        timeout: Request timeout in seconds (connect timeout 10 s), an
            ``httpx.Timeout``, or ``None`` for no timeout.
        max_retries: Retry budget per request; ``0`` disables retries.
        backoff: Retry backoff (1 s, x2, capped at 30 s by default).
        rate_limit: ``True`` for the documented client-side limits, ``False`` to
            disable them, or a :class:`~topstepx.RateLimits` to replace them.
        validate_requests: Run client-side request validation (default ``True``).
        refresh_margin: Renew the token this long before it expires.
        transport: An ``httpx`` async transport (for example ``httpx.MockTransport``).
        http_client: A preconfigured ``httpx.AsyncClient`` owned by the caller.
    """

    def __init__(
        self,
        *,
        username: str | None = None,
        api_key: str | None = None,
        token: str | None = None,
        base_url: str = DEFAULT_BASE_URL,
        user_hub_url: str = DEFAULT_USER_HUB_URL,
        market_hub_url: str = DEFAULT_MARKET_HUB_URL,
        timeout: float | httpx.Timeout | None = DEFAULT_TIMEOUT,
        max_retries: int = DEFAULT_MAX_RETRIES,
        backoff: Backoff | None = None,
        rate_limit: bool | RateLimits = True,
        validate_requests: bool = True,
        refresh_margin: float | timedelta = DEFAULT_REFRESH_MARGIN,
        transport: httpx.AsyncBaseTransport | None = None,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        settings = build_settings(
            base_url=base_url,
            user_hub_url=user_hub_url,
            market_hub_url=market_hub_url,
            timeout=timeout,
            max_retries=max_retries,
            backoff=backoff or Backoff(),
            rate_limit=rate_limit,
            validate_requests=validate_requests,
            refresh_margin=refresh_margin,
        )
        if transport is not None and http_client is not None:
            raise ValueError("pass either transport or http_client, not both")
        self._core = ClientCore(settings, make_credentials(username, api_key), token)
        self._owns_http = http_client is None
        self._http = http_client or httpx.AsyncClient(transport=transport, timeout=settings.timeout)
        self._auth_lock = asyncio.Lock()
        self._sleep: Callable[[float], Awaitable[None]] = asyncio.sleep
        self.auth = AsyncAuthAPI(self)
        """Authentication: ``login_key``, ``login_app``, ``logout``, ``validate``."""
        self.accounts = AsyncAccountsAPI(self)
        """Accounts: ``search``."""
        self.contracts = AsyncContractsAPI(self)
        """Contracts: ``search``, ``search_by_id``, ``available``."""
        self.history = AsyncHistoryAPI(self)
        """Historical bars: ``retrieve_bars``."""
        self.orders = AsyncOrdersAPI(self)
        """Orders: ``search``, ``search_by_id``, ``search_open``, ``query``,
        ``place``, ``cancel``, ``modify``."""
        self.positions = AsyncPositionsAPI(self)
        """Positions: ``search_open``, ``close``, ``partial_close``."""
        self.trades = AsyncTradesAPI(self)
        """Trades: ``search``."""
        self.status = AsyncStatusAPI(self)
        """Status: ``ping``."""

    @classmethod
    def from_env(cls, *, environ: Mapping[str, str] | None = None, **overrides: Any) -> AsyncClient:
        """Build a client from the ``TOPSTEPX_*`` environment variables.

        See :meth:`topstepx.Client.from_env`.

        Args:
            environ: Mapping to read instead of ``os.environ``.
            **overrides: Any constructor keyword argument.
        """
        kwargs: dict[str, Any] = dict(env_overrides(environ))
        kwargs.update(overrides)
        return cls(**kwargs)

    # -- properties --------------------------------------------------------

    @property
    def base_url(self) -> str:
        """The REST base URL."""
        return self._core.settings.base_url

    @property
    def user_hub_url(self) -> str:
        """The configured SignalR user hub URL."""
        return self._core.settings.user_hub_url

    @property
    def market_hub_url(self) -> str:
        """The configured SignalR market hub URL."""
        return self._core.settings.market_hub_url

    @property
    def token(self) -> str | None:
        """The currently stored token, without any refresh (``None`` if none)."""
        session = self._core.session
        return session.token if session is not None else None

    @property
    def token_expires_at(self) -> float | None:
        """Expiry of the stored token as a UNIX timestamp, if a token is held."""
        session = self._core.session
        return session.expires_at if session is not None else None

    # -- operations --------------------------------------------------------

    async def get_token(self) -> str:
        """Return a usable token, logging in or refreshing first (``ensure_token``).

        The realtime hubs use this as their token provider.

        Raises:
            NotAuthenticatedError: No token and no credentials.
        """
        return await self._run(self._core.get_token())

    def invalidate_token(self, token: str) -> bool:
        """Discard the stored token if it is still ``token``.

        The next authenticated call then logs in again with the configured
        credentials instead of reusing the token until it expires. The realtime
        hubs call this when a hub rejects the token with HTTP 401. No I/O.

        Args:
            token: The token that was rejected.

        Returns:
            ``True`` if the stored token was discarded.
        """
        return self._core.invalidate_token(token)

    @overload
    async def execute(self, target: RequestModel[EnvT], /, *, check: bool = False) -> EnvT: ...
    @overload
    async def execute(
        self, target: ops.Operation[None, RespT], /, *, check: bool = False
    ) -> RespT: ...
    @overload
    async def execute(
        self, target: ops.Operation[Any, Any] | str, request: Any = None, /, *, check: bool = False
    ) -> Any: ...
    async def execute(self, target: Any, request: Any = None, /, *, check: bool = False) -> Any:
        """Low-level escape hatch; see :meth:`topstepx.Client.execute`.

        Args:
            target: A request model, an :class:`~topstepx.operations.Operation`
                or an ``operationId``.
            request: The request body when ``target`` is an operation.
            check: Raise :class:`~topstepx.errors.ApiError` on a business failure.

        Returns:
            The operation's full response model (a ``str`` for ``Status_Ping``).
        """
        op, body = resolve_target(target, request)
        return await self._run(self._core.execute(op, body, check=check))

    async def _call(self, op: ops.Operation[Any, T], request: object) -> T:
        return await self._run(self._core.call(op, request))

    async def _run(self, flow: Flow[T]) -> T:
        """Drive a shared flow with asyncio I/O."""
        try:
            effect = next(flow)
            while True:
                try:
                    if isinstance(effect, SendRequest):
                        request = self._http.build_request(
                            effect.method,
                            effect.url,
                            headers=effect.headers,
                            content=effect.content,
                        )
                        result: Any = await self._http.send(request)
                    elif isinstance(effect, Sleep):
                        await self._sleep(effect.seconds)
                        result = None
                    elif effect is ACQUIRE_AUTH_LOCK:
                        await self._auth_lock.acquire()
                        result = self._auth_lock
                    else:  # pragma: no cover - defensive
                        raise RuntimeError(f"unknown effect {effect!r}")
                except BaseException as exc:  # noqa: PERF203 - one try per effect
                    effect = flow.throw(exc)
                else:
                    effect = flow.send(result)
        except StopIteration as stop:
            return stop.value  # type: ignore[no-any-return]
        finally:
            flow.close()

    # -- lifecycle ---------------------------------------------------------

    async def aclose(self) -> None:
        """Close the underlying HTTP client (unless it was supplied by the caller)."""
        if self._owns_http:
            await self._http.aclose()

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()

    def __repr__(self) -> str:
        return (
            f"AsyncClient(base_url={self.base_url!r}, "
            f"has_credentials={self._core.credentials is not None}, "
            f"has_token={self._core.session is not None})"
        )


class _AsyncGroup:
    __slots__ = ("_client",)

    def __init__(self, client: AsyncClient) -> None:
        self._client = client


class AsyncAuthAPI(_AsyncGroup):
    """Authentication and session operations (async)."""

    __slots__ = ()

    async def login_key(self, user_name: str, api_key: str) -> str:
        """Log in with a username and API key (``Auth_LoginKey``) and store the token.

        The credentials are used for this call only; pass them to the client
        constructor to enable automatic re-login.

        Returns:
            The new token.

        Raises:
            ApiError: On a business failure; nothing is stored.
        """
        return await self._client._run(self._client._core.login_key(user_name, api_key))

    async def login_app(self, request: LoginAppRequest) -> str:
        """Application login (``Auth_LoginApp``) and store the token.

        This operation exists only in Swagger; **the current authentication
        guide does not support it.** Use :meth:`login_key`.

        Returns:
            The new token.
        """
        return await self._client._run(self._client._core.login_app(request))

    async def logout(self) -> None:
        """End the session (``Auth_Logout``) and clear the stored token.

        The token is cleared whatever the result. Does nothing when no token is held.
        """
        await self._client._run(self._client._core.logout())

    async def validate(self) -> str:
        """Validate the current token (``Auth_Validate``) and store the renewed one.

        Returns:
            The new token (``newToken``).
        """
        return await self._client._run(self._client._core.validate())


class AsyncAccountsAPI(_AsyncGroup):
    """Trading accounts (async)."""

    __slots__ = ()

    async def search(self, only_active_accounts: bool = True) -> list[TradingAccount]:
        """Search accounts (``Account_SearchAccounts``).

        Args:
            only_active_accounts: Return only active accounts.
        """
        request = SearchAccountRequest(only_active_accounts=only_active_accounts)
        response = await self._client._call(ops.ACCOUNT_SEARCH_ACCOUNTS, request)
        return list(response.accounts)


class AsyncContractsAPI(_AsyncGroup):
    """Contract discovery (async)."""

    __slots__ = ()

    async def search(self, search_text: str, live: bool = False) -> list[Contract]:
        """Search contracts by name (``Contract_SearchContracts``; at most 20).

        Args:
            search_text: Name to search for, for example ``"ES"``.
            live: Select the live data subscription.
        """
        request = SearchContractRequest(search_text=search_text, live=live)
        response = await self._client._call(ops.CONTRACT_SEARCH_CONTRACTS, request)
        return list(response.contracts)

    async def search_by_id(self, contract_id: str) -> Contract:
        """Look up one contract (``Contract_SearchContractById``).

        Args:
            contract_id: The contract id.
        """
        op = ops.CONTRACT_SEARCH_CONTRACT_BY_ID
        request = SearchContractByIdRequest(contract_id=contract_id)
        return require((await self._client._call(op, request)).contract, op, "contract")

    async def available(self, live: bool = False) -> list[Contract]:
        """List available contracts (``Contract_AvailableContracts``).

        Args:
            live: Select the live data subscription.
        """
        request = ListAvailableContractRequest(live=live)
        response = await self._client._call(ops.CONTRACT_AVAILABLE_CONTRACTS, request)
        return list(response.contracts)


class AsyncHistoryAPI(_AsyncGroup):
    """Historical bars (async)."""

    __slots__ = ()

    async def retrieve_bars(self, request: RetrieveBarRequest) -> list[AggregateBar]:
        """Retrieve OHLCV bars (``History_GetBars``; rate bucket: 50 per 30 s).

        Args:
            request: The bar request (``limit`` at most 20,000).
        """
        return list((await self._client._call(ops.HISTORY_GET_BARS, request)).bars)


class AsyncOrdersAPI(_AsyncGroup):
    """Orders (async). **Orders are real**: use simulated accounts while developing."""

    __slots__ = ()

    async def search(self, request: SearchOrderRequest) -> list[Order]:
        """Search orders created in a time range (``Order_SearchOrders``).

        Args:
            request: Account and time range.
        """
        return list((await self._client._call(ops.ORDER_SEARCH_ORDERS, request)).orders)

    async def search_by_id(self, account_id: int, order_id: int) -> Order:
        """Look up one order (``Order_SearchOrderById``; Swagger-only).

        Args:
            account_id: Account id (must belong to the caller).
            order_id: Order id.
        """
        op = ops.ORDER_SEARCH_ORDER_BY_ID
        request = SearchOrderByIdRequest(account_id=account_id, order_id=order_id)
        return require((await self._client._call(op, request)).order, op, "order")

    async def search_open(self, account_id: int) -> list[Order]:
        """List open orders (``Order_SearchOpenOrders``).

        Args:
            account_id: Account id.
        """
        request = SearchOpenOrderRequest(account_id=account_id)
        return list((await self._client._call(ops.ORDER_SEARCH_OPEN_ORDERS, request)).orders)

    async def query(self, request: SearchOrdersQueryRequest) -> OrderQueryResult:
        """Filter and paginate orders (``Order_SearchOrdersQuery``; Swagger-only).

        Args:
            request: Filter, pagination and sorting.

        Returns:
            The orders and, when requested and returned, the total count.
        """
        response = await self._client._call(ops.ORDER_SEARCH_ORDERS_QUERY, request)
        return OrderQueryResult(orders=list(response.orders), total_count=response.total_count)

    async def place(self, request: PlaceOrderRequest) -> int:
        """Place an order (``Order_PlaceOrder``).

        Never retried on 5xx or transport errors.

        Args:
            request: The order; see the ``PlaceOrderRequest`` constructors.

        Returns:
            The new order id.

        Raises:
            ApiError: On a business failure; ``order_id`` carries any id the
                server returned with the rejection.
        """
        op = ops.ORDER_PLACE_ORDER
        return require((await self._client._call(op, request)).order_id, op, "orderId")

    async def cancel(self, account_id: int, order_id: int) -> None:
        """Cancel an order (``Order_CancelOrder``).

        Args:
            account_id: Account id.
            order_id: Order id.
        """
        request = CancelOrderRequest(account_id=account_id, order_id=order_id)
        await self._client._call(ops.ORDER_CANCEL_ORDER, request)

    async def modify(self, request: ModifyOrderRequest) -> None:
        """Modify an open order (``Order_ModifyOrder``).

        Args:
            request: The order and the fields to change.
        """
        await self._client._call(ops.ORDER_MODIFY_ORDER, request)


class AsyncPositionsAPI(_AsyncGroup):
    """Positions (async). **Closing positions trades**: use simulated accounts."""

    __slots__ = ()

    async def search_open(self, account_id: int) -> list[Position]:
        """List open positions (``Position_SearchOpenPositions``).

        Args:
            account_id: Account id.
        """
        request = SearchPositionRequest(account_id=account_id)
        response = await self._client._call(ops.POSITION_SEARCH_OPEN_POSITIONS, request)
        return list(response.positions)

    async def close(self, account_id: int, contract_id: str) -> None:
        """Close a contract position (``Position_CloseContractPosition``).

        Args:
            account_id: Account id.
            contract_id: Contract id.
        """
        request = CloseContractPositionRequest(account_id=account_id, contract_id=contract_id)
        await self._client._call(ops.POSITION_CLOSE_CONTRACT_POSITION, request)

    async def partial_close(self, account_id: int, contract_id: str, size: int) -> None:
        """Reduce a contract position (``Position_PartialCloseContractPosition``).

        Args:
            account_id: Account id.
            contract_id: Contract id.
            size: Quantity to close (at least 1, at most the position size).
        """
        request = PartialCloseContractPositionRequest(
            account_id=account_id, contract_id=contract_id, size=size
        )
        await self._client._call(ops.POSITION_PARTIAL_CLOSE_CONTRACT_POSITION, request)


class AsyncTradesAPI(_AsyncGroup):
    """Trade executions (async)."""

    __slots__ = ()

    async def search(self, request: SearchTradeRequest) -> list[HalfTrade]:
        """Search trades in a time range (``Trade_SearchHalfTurnTrades``).

        Args:
            request: Account and time range.
        """
        response = await self._client._call(ops.TRADE_SEARCH_HALF_TURN_TRADES, request)
        return list(response.trades)


class AsyncStatusAPI(_AsyncGroup):
    """Service status (async)."""

    __slots__ = ()

    async def ping(self) -> str:
        """Ping the API (``Status_Ping``; Swagger-only) and return ``"pong"``.

        Sends the token when one is held or obtainable, otherwise pings anonymously.
        """
        return await self._client._call(ops.STATUS_PING, None)
