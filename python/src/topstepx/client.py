"""Synchronous REST client.

:class:`Client` is thread-safe: one instance can serve many threads, and token
refreshes are single-flight across them. For asyncio use
:class:`~topstepx.AsyncClient`, which has the same endpoint groups and methods.
"""

from __future__ import annotations

import threading
import time
from collections.abc import Callable, Mapping
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
    "AccountsAPI",
    "AuthAPI",
    "Client",
    "ContractsAPI",
    "HistoryAPI",
    "OrdersAPI",
    "PositionsAPI",
    "StatusAPI",
    "TradesAPI",
]

T = TypeVar("T")
RespT = TypeVar("RespT")
EnvT = TypeVar("EnvT", bound=ResponseEnvelope)


class Client:
    """Synchronous client of the ProjectX Gateway REST API.

    Endpoint groups are attributes: :attr:`auth`, :attr:`accounts`,
    :attr:`contracts`, :attr:`history`, :attr:`orders`, :attr:`positions`,
    :attr:`trades` and :attr:`status`. Every high-level method checks the
    business result and raises :class:`~topstepx.errors.ApiError` on failure;
    :meth:`execute` returns the full response envelope instead.

    Authentication is lazy: with ``username`` and ``api_key`` the first
    authenticated call logs in, the token is renewed through ``Auth_Validate``
    within ``refresh_margin`` of its expiry, and an HTTP 401 triggers one
    re-login and retry.

    Example::

        with Client(username="me", api_key="...") as client:
            for account in client.accounts.search():
                print(account.id, account.name, account.balance)

    Args:
        username: Platform username (use together with ``api_key``).
        api_key: API key (a secret; never logged or shown in ``repr``).
        token: A pre-existing token, instead of or in addition to credentials.
        base_url: REST base URL. Other firms run the gateway on other hosts.
        user_hub_url: SignalR user hub URL (used by ``UserHub(client)``).
        market_hub_url: SignalR market hub URL (used by ``MarketHub(client)``).
        timeout: Request timeout in seconds (connect timeout 10 s), an
            ``httpx.Timeout``, or ``None`` for no timeout.
        max_retries: Retry budget per request for 429, and for 502/503/504 and
            transport errors on read operations. ``0`` disables retries.
        backoff: Retry backoff (1 s, x2, capped at 30 s by default).
        rate_limit: ``True`` for the documented client-side limits, ``False`` to
            disable them, or a :class:`~topstepx.RateLimits` to replace them.
        validate_requests: Run client-side request validation (default ``True``).
        refresh_margin: Renew the token this long before it expires (seconds or
            ``timedelta``; default one hour).
        transport: An ``httpx`` transport, for example ``httpx.MockTransport`` in
            tests. Mutually exclusive with ``http_client``.
        http_client: A preconfigured ``httpx.Client``. The caller keeps ownership
            and must close it.
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
        transport: httpx.BaseTransport | None = None,
        http_client: httpx.Client | None = None,
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
        self._http = http_client or httpx.Client(transport=transport, timeout=settings.timeout)
        self._auth_lock = threading.Lock()
        self._sleep: Callable[[float], None] = time.sleep
        self.auth = AuthAPI(self)
        """Authentication: ``login_key``, ``login_app``, ``logout``, ``validate``."""
        self.accounts = AccountsAPI(self)
        """Accounts: ``search``."""
        self.contracts = ContractsAPI(self)
        """Contracts: ``search``, ``search_by_id``, ``available``."""
        self.history = HistoryAPI(self)
        """Historical bars: ``retrieve_bars``."""
        self.orders = OrdersAPI(self)
        """Orders: ``search``, ``search_by_id``, ``search_open``, ``query``,
        ``place``, ``cancel``, ``modify``."""
        self.positions = PositionsAPI(self)
        """Positions: ``search_open``, ``close``, ``partial_close``."""
        self.trades = TradesAPI(self)
        """Trades: ``search``."""
        self.status = StatusAPI(self)
        """Status: ``ping``."""

    @classmethod
    def from_env(cls, *, environ: Mapping[str, str] | None = None, **overrides: Any) -> Client:
        """Build a client from the ``TOPSTEPX_*`` environment variables.

        Reads ``TOPSTEPX_USERNAME``, ``TOPSTEPX_API_KEY`` and the optional
        ``TOPSTEPX_API_URL``, ``TOPSTEPX_USER_HUB_URL`` and
        ``TOPSTEPX_MARKET_HUB_URL``. Keyword arguments override the environment.

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

    def get_token(self) -> str:
        """Return a usable token, logging in or refreshing first (``ensure_token``).

        The realtime hubs use this as their token provider.

        Raises:
            NotAuthenticatedError: No token and no credentials.
        """
        return self._run(self._core.get_token())

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
    def execute(self, target: RequestModel[EnvT], /, *, check: bool = False) -> EnvT: ...
    @overload
    def execute(self, target: ops.Operation[None, RespT], /, *, check: bool = False) -> RespT: ...
    @overload
    def execute(
        self, target: ops.Operation[Any, Any] | str, request: Any = None, /, *, check: bool = False
    ) -> Any: ...
    def execute(self, target: Any, request: Any = None, /, *, check: bool = False) -> Any:
        """Low-level escape hatch: perform any operation, return the full envelope.

        A business failure is returned, not raised (unless ``check=True``), so
        fields the high-level methods discard remain available, such as the
        ``orderId`` of a rejected order. HTTP, transport and decode errors are
        still raised. Authentication, retries, rate limiting and validation apply
        as usual, but the stored session is not changed by the response (so
        ``execute`` of a login or logout does not store or clear a token).

        Args:
            target: A request model (its operation is implied), an
                :class:`~topstepx.operations.Operation` or an ``operationId``.
            request: The request body when ``target`` is an operation that takes
                one.
            check: Raise :class:`~topstepx.errors.ApiError` on a business failure.

        Returns:
            The operation's response model (a ``str`` for ``Status_Ping``).
        """
        op, body = resolve_target(target, request)
        return self._run(self._core.execute(op, body, check=check))

    def _call(self, op: ops.Operation[Any, T], request: object) -> T:
        return self._run(self._core.call(op, request))

    def _run(self, flow: Flow[T]) -> T:
        """Drive a shared flow with blocking I/O."""
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
                        result: Any = self._http.send(request)
                    elif isinstance(effect, Sleep):
                        self._sleep(effect.seconds)
                        result = None
                    elif effect is ACQUIRE_AUTH_LOCK:
                        self._auth_lock.acquire()
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

    def close(self) -> None:
        """Close the underlying HTTP client (unless it was supplied by the caller)."""
        if self._owns_http:
            self._http.close()

    def __enter__(self) -> Self:
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self.close()

    def __repr__(self) -> str:
        return (
            f"Client(base_url={self.base_url!r}, "
            f"has_credentials={self._core.credentials is not None}, "
            f"has_token={self._core.session is not None})"
        )


class _Group:
    __slots__ = ("_client",)

    def __init__(self, client: Client) -> None:
        self._client = client


class AuthAPI(_Group):
    """Authentication and session operations."""

    __slots__ = ()

    def login_key(self, user_name: str, api_key: str) -> str:
        """Log in with a username and API key (``Auth_LoginKey``); store the token.

        The credentials are used for this call only; pass them to the client
        constructor to enable automatic re-login.

        Returns:
            The new token.

        Raises:
            ApiError: On a business failure (for example ``InvalidCredentials``,
                code 3); nothing is stored.
        """
        return self._client._run(self._client._core.login_key(user_name, api_key))

    def login_app(self, request: LoginAppRequest) -> str:
        """Application login (``Auth_LoginApp``) and store the returned token.

        This operation exists only in Swagger; **the current authentication
        guide does not support it.** Use :meth:`login_key`.

        Returns:
            The new token.
        """
        return self._client._run(self._client._core.login_app(request))

    def logout(self) -> None:
        """End the session (``Auth_Logout``) and clear the stored token.

        The token is cleared whatever the result; a business failure is still
        raised. A later authenticated call logs in again if credentials are
        configured. Does nothing when no token is held.
        """
        self._client._run(self._client._core.logout())

    def validate(self) -> str:
        """Validate the current token (``Auth_Validate``) and store the renewed one.

        Returns:
            The new token (``newToken``).
        """
        return self._client._run(self._client._core.validate())


class AccountsAPI(_Group):
    """Trading accounts."""

    __slots__ = ()

    def search(self, only_active_accounts: bool = True) -> list[TradingAccount]:
        """Search accounts (``Account_SearchAccounts``).

        Args:
            only_active_accounts: Return only active accounts.
        """
        request = SearchAccountRequest(only_active_accounts=only_active_accounts)
        return list(self._client._call(ops.ACCOUNT_SEARCH_ACCOUNTS, request).accounts)


class ContractsAPI(_Group):
    """Contract discovery."""

    __slots__ = ()

    def search(self, search_text: str, live: bool = False) -> list[Contract]:
        """Search contracts by name (``Contract_SearchContracts``; at most 20).

        Args:
            search_text: Name to search for, for example ``"ES"``.
            live: Select the live data subscription.
        """
        request = SearchContractRequest(search_text=search_text, live=live)
        return list(self._client._call(ops.CONTRACT_SEARCH_CONTRACTS, request).contracts)

    def search_by_id(self, contract_id: str) -> Contract:
        """Look up one contract (``Contract_SearchContractById``).

        Args:
            contract_id: The contract id.
        """
        op = ops.CONTRACT_SEARCH_CONTRACT_BY_ID
        request = SearchContractByIdRequest(contract_id=contract_id)
        return require(self._client._call(op, request).contract, op, "contract")

    def available(self, live: bool = False) -> list[Contract]:
        """List available contracts (``Contract_AvailableContracts``).

        Args:
            live: Select the live data subscription.
        """
        request = ListAvailableContractRequest(live=live)
        return list(self._client._call(ops.CONTRACT_AVAILABLE_CONTRACTS, request).contracts)


class HistoryAPI(_Group):
    """Historical bars."""

    __slots__ = ()

    def retrieve_bars(self, request: RetrieveBarRequest) -> list[AggregateBar]:
        """Retrieve OHLCV bars (``History_GetBars``; rate bucket: 50 per 30 s).

        Args:
            request: The bar request (``limit`` at most 20,000).
        """
        return list(self._client._call(ops.HISTORY_GET_BARS, request).bars)


class OrdersAPI(_Group):
    """Orders. **Orders are real**: use simulated accounts while developing."""

    __slots__ = ()

    def search(self, request: SearchOrderRequest) -> list[Order]:
        """Search orders created in a time range (``Order_SearchOrders``).

        Args:
            request: Account and time range.
        """
        return list(self._client._call(ops.ORDER_SEARCH_ORDERS, request).orders)

    def search_by_id(self, account_id: int, order_id: int) -> Order:
        """Look up one order (``Order_SearchOrderById``; Swagger-only).

        Args:
            account_id: Account id (must belong to the caller).
            order_id: Order id.
        """
        op = ops.ORDER_SEARCH_ORDER_BY_ID
        request = SearchOrderByIdRequest(account_id=account_id, order_id=order_id)
        return require(self._client._call(op, request).order, op, "order")

    def search_open(self, account_id: int) -> list[Order]:
        """List open orders (``Order_SearchOpenOrders``).

        Args:
            account_id: Account id.
        """
        request = SearchOpenOrderRequest(account_id=account_id)
        return list(self._client._call(ops.ORDER_SEARCH_OPEN_ORDERS, request).orders)

    def query(self, request: SearchOrdersQueryRequest) -> OrderQueryResult:
        """Filter and paginate orders (``Order_SearchOrdersQuery``; Swagger-only).

        Args:
            request: Filter, pagination and sorting.

        Returns:
            The orders and, when requested and returned, the total count.
        """
        response = self._client._call(ops.ORDER_SEARCH_ORDERS_QUERY, request)
        return OrderQueryResult(orders=list(response.orders), total_count=response.total_count)

    def place(self, request: PlaceOrderRequest) -> int:
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
        return require(self._client._call(op, request).order_id, op, "orderId")

    def cancel(self, account_id: int, order_id: int) -> None:
        """Cancel an order (``Order_CancelOrder``).

        Args:
            account_id: Account id.
            order_id: Order id.
        """
        request = CancelOrderRequest(account_id=account_id, order_id=order_id)
        self._client._call(ops.ORDER_CANCEL_ORDER, request)

    def modify(self, request: ModifyOrderRequest) -> None:
        """Modify an open order (``Order_ModifyOrder``).

        Args:
            request: The order and the fields to change.
        """
        self._client._call(ops.ORDER_MODIFY_ORDER, request)


class PositionsAPI(_Group):
    """Positions. **Closing positions trades**: use simulated accounts while developing."""

    __slots__ = ()

    def search_open(self, account_id: int) -> list[Position]:
        """List open positions (``Position_SearchOpenPositions``).

        Args:
            account_id: Account id.
        """
        request = SearchPositionRequest(account_id=account_id)
        return list(self._client._call(ops.POSITION_SEARCH_OPEN_POSITIONS, request).positions)

    def close(self, account_id: int, contract_id: str) -> None:
        """Close a contract position (``Position_CloseContractPosition``).

        Args:
            account_id: Account id.
            contract_id: Contract id.
        """
        request = CloseContractPositionRequest(account_id=account_id, contract_id=contract_id)
        self._client._call(ops.POSITION_CLOSE_CONTRACT_POSITION, request)

    def partial_close(self, account_id: int, contract_id: str, size: int) -> None:
        """Reduce a contract position (``Position_PartialCloseContractPosition``).

        Args:
            account_id: Account id.
            contract_id: Contract id.
            size: Quantity to close (at least 1, at most the position size).
        """
        request = PartialCloseContractPositionRequest(
            account_id=account_id, contract_id=contract_id, size=size
        )
        self._client._call(ops.POSITION_PARTIAL_CLOSE_CONTRACT_POSITION, request)


class TradesAPI(_Group):
    """Trade executions."""

    __slots__ = ()

    def search(self, request: SearchTradeRequest) -> list[HalfTrade]:
        """Search trades in a time range (``Trade_SearchHalfTurnTrades``).

        Args:
            request: Account and time range.
        """
        return list(self._client._call(ops.TRADE_SEARCH_HALF_TURN_TRADES, request).trades)


class StatusAPI(_Group):
    """Service status."""

    __slots__ = ()

    def ping(self) -> str:
        """Ping the API (``Status_Ping``; Swagger-only) and return ``"pong"``.

        Sends the token when one is held or obtainable, otherwise pings anonymously.
        """
        return self._client._call(ops.STATUS_PING, None)
