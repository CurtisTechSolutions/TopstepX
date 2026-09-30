"""SignalR hub connection over WebSockets (asyncio).

:class:`HubConnection` implements the connection lifecycle of
``SDK_BEHAVIOR.md`` section 11 on top of the ``websockets`` library:

* **connect**: ``https``/``http`` become ``wss``/``ws``; the token is sent as the
  ``access_token`` query parameter *and* as ``Authorization: Bearer``;
  negotiation is skipped;
* **handshake** with a timeout; messages sharing the handshake frame are kept;
* **invocations** with increasing string ids, completions, and a timeout;
* **keepalive**: a ping after ``keepalive_interval`` without sending anything,
  and connection loss after ``server_timeout`` without receiving anything;
* **reconnect** with a configurable delay policy and a fresh token per attempt,
  followed by **subscription replay** in insertion order, then ``Reconnected``;
* **rejected tokens**: an upgrade rejected with HTTP 401 reports the token to the
  token provider (a client discards it) and raises :class:`TokenRejectedError`;
* **events** through a bounded buffer that drops the oldest event (counted and
  logged) instead of ever blocking the read loop.

Use :class:`~topstepx.realtime.UserHub` or :class:`~topstepx.realtime.MarketHub`
rather than this class directly.
"""

from __future__ import annotations

import asyncio
import enum
import inspect
import itertools
import logging
import math
import time
from collections import deque
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import suppress
from dataclasses import dataclass
from types import TracebackType
from typing import TYPE_CHECKING, Any, Literal, TypeVar
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

from websockets.asyncio.client import ClientConnection, connect
from websockets.exceptions import ConnectionClosed, InvalidStatus, InvalidURI, WebSocketException
from websockets.typing import Data

from .._config import USER_AGENT
from ..async_client import AsyncClient
from ..client import Client
from ..errors import (
    ConnectionLostError,
    HandshakeError,
    HubConnectError,
    HubError,
    HubTimeoutError,
    InvocationError,
    NotConnectedError,
    ServerCloseError,
    TokenRejectedError,
)
from .events import (
    Closed,
    Connected,
    HubEvent,
    Reconnected,
    Reconnecting,
    SubscriptionReplayFailed,
    UnknownEvent,
)
from .protocol import (
    CLOSE_MESSAGE,
    HANDSHAKE_REQUEST,
    PING_MESSAGE,
    CloseMessage,
    CompletionMessage,
    FrameParser,
    InvocationMessage,
    ProtocolError,
    encode_invocation,
    parse_handshake_response,
    parse_message,
)

if TYPE_CHECKING:
    from typing_extensions import Self

__all__ = [
    "HubConnection",
    "HubOptions",
    "HubState",
    "ReconnectPolicy",
    "TokenProvider",
    "TokenSource",
    "build_hub_url",
]

E = TypeVar("E", bound=HubEvent)

TokenProvider = Callable[[], str | Awaitable[str]]
"""A callable returning a token, synchronously or as an awaitable."""
TokenSource = AsyncClient | Client | TokenProvider | str
"""What a hub accepts as its token source: a client, a provider, or a fixed token."""
Decoder = Callable[[str, tuple[Any, ...]], list[HubEvent]]


class _QuietWebsocketsLogger(logging.LoggerAdapter):  # type: ignore[type-arg]
    """Logger handed to ``websockets`` that never enables DEBUG.

    At DEBUG, ``websockets`` logs the request line (with ``access_token``) and
    the ``Authorization`` header.
    """

    def isEnabledFor(self, level: int) -> bool:
        return level > logging.DEBUG and self.logger.isEnabledFor(level)


_WEBSOCKETS_LOGGER = _QuietWebsocketsLogger(logging.getLogger("topstepx.realtime.websockets"), {})


def build_hub_url(url: str, token: str) -> str:
    """Return the WebSocket URL for ``url`` with ``access_token=<token>`` set.

    ``https``/``http`` become ``wss``/``ws``. The token is URL-encoded and *set*:
    an ``access_token`` already in ``url`` (the official guide builds hub URLs
    as ``...?access_token=`` + token) is replaced in place, and duplicates are
    dropped, because a repeated parameter reaches ASP.NET as one comma-joined
    value. Other query parameters are kept.

    Raises:
        ValueError: If ``url`` is not an absolute http(s)/ws(s) URL.
    """
    parts = urlsplit(url)
    scheme = {"https": "wss", "http": "ws", "wss": "wss", "ws": "ws"}.get(parts.scheme.lower())
    if scheme is None or not parts.netloc:
        raise ValueError(f"hub URL must be an absolute http(s) or ws(s) URL, got {url!r}")
    query: list[tuple[str, str]] = []
    token_set = False
    for key, value in parse_qsl(parts.query, keep_blank_values=True):
        if key != "access_token":
            query.append((key, value))
        elif not token_set:
            query.append((key, token))
            token_set = True
    if not token_set:
        query.append(("access_token", token))
    return urlunsplit((scheme, parts.netloc, parts.path, urlencode(query), ""))


def _redact_url(url: str) -> str:
    parts = urlsplit(url)
    return urlunsplit((parts.scheme, parts.netloc, parts.path, "", ""))


def _scrub(exc: BaseException, token: str) -> str:
    text = f"{type(exc).__name__}: {exc}"
    return text.replace(token, "<redacted>") if token else text


def _describe_close(exc: ConnectionClosed) -> str:
    frame = exc.rcvd or exc.sent
    if frame is None:
        return "no close frame"
    return f"code {frame.code}" + (f", {frame.reason}" if frame.reason else "")


@dataclass(frozen=True, slots=True)
class _TokenSupply:
    """An async token getter plus the hook that reports a rejected token."""

    get: Callable[[], Awaitable[str]]
    reject: Callable[[str], Awaitable[None]]


async def _ignore_rejection(token: str) -> None:
    return None


def resolve_token_source(source: TokenSource) -> _TokenSupply:
    """Turn a token source into an async token getter and a rejection hook.

    * :class:`~topstepx.AsyncClient`: ``get_token()`` (``ensure_token``); a
      rejected token is discarded with ``invalidate_token()``;
    * :class:`~topstepx.Client`: the same, with ``get_token()`` run in a worker
      thread;
    * a ``str``: that fixed token (no refresh; a rejection cannot be acted on);
    * a callable returning a ``str`` or an awaitable ``str``; if it has an
      ``invalidate_token(token)`` attribute (sync or async), rejected tokens are
      reported to it.
    """
    if isinstance(source, (AsyncClient, Client)):
        client = source

        async def reject_on_client(token: str) -> None:
            client.invalidate_token(token)  # no I/O: safe on the event loop

        if isinstance(client, AsyncClient):
            return _TokenSupply(client.get_token, reject_on_client)
        sync_client = client

        async def from_sync_client() -> str:
            return await asyncio.to_thread(sync_client.get_token)

        return _TokenSupply(from_sync_client, reject_on_client)
    if isinstance(source, str):
        fixed = source

        async def from_string() -> str:
            return fixed

        return _TokenSupply(from_string, _ignore_rejection)
    if callable(source):
        provider = source
        hook = getattr(source, "invalidate_token", None)

        async def from_callable() -> str:
            result = provider()
            if inspect.isawaitable(result):
                result = await result
            if not isinstance(result, str):
                raise TypeError(f"token provider returned {type(result).__name__}, not str")
            return result

        async def reject_on_provider(token: str) -> None:
            if callable(hook):
                outcome = hook(token)
                if inspect.isawaitable(outcome):
                    await outcome

        return _TokenSupply(from_callable, reject_on_provider)
    raise TypeError(f"unsupported token source: {type(source).__name__}")


@dataclass(frozen=True, slots=True)
class ReconnectPolicy:
    """When to try reconnecting after a connection loss.

    Attempt ``n`` (1-based) waits ``delays[n - 1]`` seconds, and ``repeat_delay``
    once ``delays`` is exhausted. The default is 0, 1, 2, 5 and 10 s, then every
    30 s, with unlimited attempts.

    Attributes:
        delays: The first delays, in seconds.
        repeat_delay: The delay of every later attempt.
        max_attempts: Maximum number of attempts per connection loss; ``None``
            is unlimited and ``0`` disables reconnecting.
    """

    delays: tuple[float, ...] = (0.0, 1.0, 2.0, 5.0, 10.0)
    repeat_delay: float = 30.0
    max_attempts: int | None = None

    def __post_init__(self) -> None:
        if any(d < 0 for d in self.delays) or self.repeat_delay < 0:
            raise ValueError("reconnect delays must be >= 0")
        if self.max_attempts is not None and self.max_attempts < 0:
            raise ValueError("max_attempts must be >= 0 or None")

    @classmethod
    def disabled(cls) -> ReconnectPolicy:
        """A policy that never reconnects."""
        return cls(max_attempts=0)

    def delay(self, attempt: int) -> float | None:
        """The delay before attempt ``attempt``, or ``None`` when attempts are exhausted."""
        if self.max_attempts is not None and attempt > self.max_attempts:
            return None
        if 1 <= attempt <= len(self.delays):
            return self.delays[attempt - 1]
        return self.repeat_delay


@dataclass(frozen=True, slots=True, kw_only=True)
class HubOptions:
    """Tuning of a hub connection. All durations are in seconds.

    Attributes:
        reconnect: The reconnect policy.
        keepalive_interval: Send a ping after this long without sending anything.
        server_timeout: Treat the connection as lost after this long without
            receiving anything (``None`` disables the check).
        handshake_timeout: Maximum wait for the handshake response.
        invocation_timeout: Maximum wait for an invocation's completion.
        open_timeout: Maximum time to open the WebSocket.
        close_timeout: Maximum time to close the WebSocket gracefully.
        event_buffer_size: Capacity of the event buffer (and of the callback
            queue). When full, the oldest event is dropped and counted.
        max_message_size: Maximum size of one WebSocket message in bytes
            (``None`` for no limit).
        proxy: Passed to ``websockets.connect``: ``True`` uses the proxy settings
            of the environment, ``None`` disables proxies, and a string is a
            proxy URL.
    """

    reconnect: ReconnectPolicy = ReconnectPolicy()
    keepalive_interval: float = 15.0
    server_timeout: float | None = 30.0
    handshake_timeout: float = 15.0
    invocation_timeout: float = 30.0
    open_timeout: float = 10.0
    close_timeout: float = 5.0
    event_buffer_size: int = 10_000
    max_message_size: int | None = 16 * 1024 * 1024
    proxy: str | Literal[True] | None = True

    def __post_init__(self) -> None:
        if self.event_buffer_size < 1:
            raise ValueError("event_buffer_size must be >= 1")
        if self.keepalive_interval <= 0:
            raise ValueError("keepalive_interval must be > 0")


class HubState(enum.Enum):
    """Lifecycle state of a hub."""

    DISCONNECTED = "disconnected"
    """Created, not started."""
    CONNECTING = "connecting"
    """``start()`` is opening the connection."""
    CONNECTED = "connected"
    """Connected; invocations are allowed."""
    RECONNECTING = "reconnecting"
    """The connection was lost; reconnecting and replaying subscriptions."""
    CLOSED = "closed"
    """Closed for good (after ``close()`` or a final failure)."""


class _EventBuffer:
    """A bounded FIFO that drops its oldest entry instead of blocking the producer."""

    def __init__(self, maxsize: int, log: logging.Logger, label: str) -> None:
        self._items: deque[HubEvent] = deque()
        self._maxsize = maxsize
        self._wakeup = asyncio.Event()
        self._ended = False
        self._log = log
        self._label = label
        self._last_report = -math.inf
        self.dropped = 0
        self.consumed = False

    def put(self, event: HubEvent) -> None:
        if self._ended:
            return
        if len(self._items) >= self._maxsize:
            self._items.popleft()
            self.dropped += 1
            now = time.monotonic()
            if now - self._last_report >= 5.0:
                self._last_report = now
                self._log.log(
                    logging.WARNING if self.consumed else logging.DEBUG,
                    "The %s is full (%d events): dropping the oldest events because the "
                    "consumer is falling behind (%d dropped so far)",
                    self._label,
                    self._maxsize,
                    self.dropped,
                )
        self._items.append(event)
        self._wakeup.set()

    def end(self) -> None:
        self._ended = True
        self._wakeup.set()

    async def get(self) -> HubEvent | None:
        while not self._items:
            if self._ended:
                return None
            self._wakeup.clear()
            await self._wakeup.wait()
        return self._items.popleft()


class _Liveness:
    """Per-connection receive time, shared by the read loop and the watchdog."""

    __slots__ = ("last_received", "timeout_reason")

    def __init__(self, now: float) -> None:
        self.last_received = now
        self.timeout_reason: str | None = None


def _expire(future: asyncio.Future[Any], method: str, timeout: float) -> None:
    if not future.done():
        future.set_exception(HubTimeoutError(f"{method} did not complete within {timeout:g} s"))


async def _recv_before(ws: ClientConnection, deadline: float) -> Data:
    """``ws.recv()`` bounded by a loop-time deadline, without ``asyncio.wait_for``.

    Raises:
        asyncio.TimeoutError: If nothing arrives before the deadline.
    """
    loop = asyncio.get_running_loop()
    task = asyncio.ensure_future(ws.recv())
    try:
        done, _ = await asyncio.wait({task}, timeout=max(deadline - loop.time(), 0.0))
    except BaseException:
        task.cancel()
        raise
    if task not in done:
        task.cancel()
        raise asyncio.TimeoutError
    return task.result()


@dataclass(frozen=True, slots=True)
class _ConnectionEnd:
    cause: BaseException | None
    allow_reconnect: bool


@dataclass(frozen=True, slots=True)
class _Opened:
    ws: ClientConnection
    parser: FrameParser
    backlog: list[str]
    attempt: int


class HubConnection:
    """A SignalR hub connection; the base class of ``UserHub`` and ``MarketHub``.

    Lifecycle: create, optionally register callbacks with :meth:`on`, then
    :meth:`start` (or ``async with``), consume :meth:`events`, and :meth:`close`.
    A closed hub cannot be restarted; create a new one.

    Args:
        url: The hub URL (``https://`` is converted to ``wss://``).
        token_source: A client, a token provider, or a fixed token.
        decoder: Turns server invocations into events.
        options: Connection tuning; see :class:`HubOptions`.
        name: Short name used in log records (``topstepx.realtime.<name>``).
    """

    def __init__(
        self,
        url: str,
        token_source: TokenSource,
        *,
        decoder: Decoder,
        options: HubOptions | None = None,
        name: str = "hub",
    ) -> None:
        build_hub_url(url, "")  # validate early
        self._url = url
        self._redacted_url = _redact_url(url)
        self._tokens = resolve_token_source(token_source)
        self._options = options or HubOptions()
        self._decoder = decoder
        self._log = logging.getLogger(f"topstepx.realtime.{name}")
        self._state = HubState.DISCONNECTED
        self._ws: ClientConnection | None = None
        self._runner: asyncio.Task[None] | None = None
        self._dispatcher: asyncio.Task[None] | None = None
        self._pending: dict[str, tuple[asyncio.Future[Any], str]] = {}
        self._invocation_ids = itertools.count(1)
        self._registry: dict[tuple[str, tuple[Any, ...]], None] = {}
        self._events = _EventBuffer(self._options.event_buffer_size, self._log, "event buffer")
        self._handler_events = _EventBuffer(
            self._options.event_buffer_size, self._log, "callback queue"
        )
        self._handler_events.consumed = True
        self._handlers: list[tuple[type[HubEvent], Callable[[Any], object]]] = []
        self._closing = False
        self._closed = asyncio.Event()
        self._send_lock = asyncio.Lock()
        self._last_sent = 0.0

    # -- public API ----------------------------------------------------------

    @property
    def url(self) -> str:
        """The configured hub URL."""
        return self._url

    @property
    def state(self) -> HubState:
        """The current lifecycle state."""
        return self._state

    @property
    def subscriptions(self) -> list[tuple[str, tuple[Any, ...]]]:
        """The subscription registry, in replay order, as ``(method, arguments)``."""
        return list(self._registry)

    @property
    def dropped_events(self) -> int:
        """Events dropped because the event buffer or callback queue was full."""
        return self._events.dropped + self._handler_events.dropped

    async def start(self) -> None:
        """Connect and complete the handshake; emits :class:`Connected`.

        The initial connection is not retried (reconnecting applies only after a
        connection loss).

        Raises:
            HubConnectError: The WebSocket could not be opened.
            HandshakeError: The SignalR handshake failed.
            HubError: The hub was already started.
        """
        if self._state is not HubState.DISCONNECTED:
            raise HubError(f"cannot start a hub that is {self._state.value}")
        self._state = HubState.CONNECTING
        try:
            ws, parser, backlog = await self._open()
        except BaseException:
            if self._state is HubState.CONNECTING:
                self._state = HubState.DISCONNECTED
            raise
        if self._closing:
            await self._discard(ws)
            self._finish(None)
            raise NotConnectedError("the hub was closed while it was starting")
        self._ws = ws
        self._state = HubState.CONNECTED
        self._dispatcher = asyncio.create_task(self._dispatch_handlers())
        self._log.info("Connected to %s", self._redacted_url)
        self._emit(Connected())
        self._runner = asyncio.create_task(self._run(ws, parser, backlog))

    async def close(self) -> None:
        """Close the hub. Idempotent.

        Stops reconnecting, sends a close message, closes the socket, fails
        pending invocations with :class:`ConnectionLostError`, emits
        :class:`Closed` and ends the event stream.
        """
        if self._state is not HubState.CLOSED:
            self._closing = True
            ws = self._ws
            if ws is not None:
                with suppress(HubError, asyncio.TimeoutError):
                    await asyncio.wait_for(
                        self._send(ws, CLOSE_MESSAGE), self._options.close_timeout
                    )
                # Closing the socket ends the read loop on its own, even where a
                # cancellation could be lost (asyncio.wait_for before 3.12).
                await self._discard(ws)
            runner = self._runner
            if runner is not None and not runner.done():
                runner.cancel()
                await asyncio.wait({runner}, timeout=self._options.close_timeout + 1.0)
            self._finish(None)
        await self._drain_dispatcher()

    async def wait_closed(self) -> None:
        """Wait until the hub is closed (by :meth:`close` or a final failure)."""
        await self._closed.wait()

    async def invoke(self, method: str, *arguments: Any) -> Any:
        """Invoke a hub method and wait for its completion.

        This does not touch the subscription registry; use the ``subscribe_*``
        methods for subscriptions that must survive reconnects.

        Returns:
            The completion's ``result`` (``None`` for void methods).

        Raises:
            NotConnectedError: The hub is not connected (fails fast).
            InvocationError: The server completed the invocation with an error.
            HubTimeoutError: No completion within ``invocation_timeout``.
            ConnectionLostError: The connection dropped before the completion.
        """
        ws = self._ws
        if self._state is not HubState.CONNECTED or ws is None:
            raise NotConnectedError(f"cannot invoke {method}: the hub is {self._state.value}")
        return await self._invoke_on(ws, method, arguments)

    async def send(self, method: str, *arguments: Any) -> None:
        """Invoke a hub method without waiting for (or expecting) a completion.

        Raises:
            NotConnectedError: The hub is not connected.
            ConnectionLostError: The connection dropped while sending.
        """
        ws = self._ws
        if self._state is not HubState.CONNECTED or ws is None:
            raise NotConnectedError(f"cannot send {method}: the hub is {self._state.value}")
        await self._send(ws, encode_invocation(method, list(arguments), None))

    async def events(self) -> AsyncIterator[HubEvent]:
        """Iterate over data and state events until the hub is closed.

        Events are buffered from construction, so ``Connected`` is not missed
        when iteration starts after :meth:`start`. The buffer is bounded; when
        the consumer falls behind, the oldest events are dropped (see
        :attr:`dropped_events`). Use a single consumer per hub; for fan-out,
        register callbacks with :meth:`on`.
        """
        self._events.consumed = True
        while True:
            event = await self._events.get()
            if event is None:
                return
            yield event

    def on(self, event_type: type[E], callback: Callable[[E], object]) -> Callable[[], None]:
        """Register ``callback`` for events that are instances of ``event_type``.

        Use ``HubEvent`` as ``event_type`` to receive every event.

        Callbacks may be plain functions or coroutine functions. They run in a
        dedicated task, never in the read loop; exceptions are logged.

        Returns:
            A function that unregisters the callback.
        """
        entry: tuple[type[HubEvent], Callable[[Any], object]] = (event_type, callback)
        self._handlers.append(entry)

        def remove() -> None:
            with suppress(ValueError):
                self._handlers.remove(entry)

        return remove

    async def __aenter__(self) -> Self:
        await self.start()
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.close()

    def __repr__(self) -> str:
        return f"{type(self).__name__}(url={self._redacted_url!r}, state={self._state.value!r})"

    # -- subscriptions ---------------------------------------------------------

    async def _subscribe(self, method: str, *arguments: Any) -> None:
        await self.invoke(method, *arguments)
        self._registry.setdefault((method, arguments), None)

    async def _unsubscribe(self, method: str, subscribe_method: str, *arguments: Any) -> None:
        await self.invoke(method, *arguments)
        self._registry.pop((subscribe_method, arguments), None)

    async def _replay(self, ws: ClientConnection, serve: asyncio.Task[_ConnectionEnd]) -> None:
        for method, arguments in list(self._registry):
            if serve.done() or self._closing:
                return
            try:
                await self._invoke_on(ws, method, arguments)
            except ConnectionLostError:
                return
            except HubError as exc:
                self._log.warning("Re-subscribing with %s failed: %s", method, exc)
                self._emit(SubscriptionReplayFailed(method, arguments, exc))

    # -- connection lifecycle --------------------------------------------------

    async def _open(self) -> tuple[ClientConnection, FrameParser, list[str]]:
        token = await self._tokens.get()
        if not token:
            raise HubConnectError("the token provider returned an empty token")
        options = self._options
        self._log.debug("Connecting to %s", self._redacted_url)
        try:
            ws = await connect(
                build_hub_url(self._url, token),
                additional_headers={"Authorization": f"Bearer {token}"},
                user_agent_header=USER_AGENT,
                open_timeout=options.open_timeout,
                ping_interval=None,
                close_timeout=options.close_timeout,
                max_size=options.max_message_size,
                proxy=options.proxy,
                logger=_WEBSOCKETS_LOGGER,
            )
        except InvalidStatus as exc:
            status = exc.response.status_code
            if status == 401:
                await self._report_rejected(token)
                raise TokenRejectedError(
                    f"the hub at {self._redacted_url} rejected the token (HTTP 401); "
                    "it was reported to the token provider",
                    status_code=status,
                ) from None
            raise HubConnectError(
                f"the WebSocket upgrade to {self._redacted_url} was rejected with HTTP {status}",
                status_code=status,
            ) from None
        except InvalidURI:
            raise HubConnectError(f"invalid hub URL {self._redacted_url}") from None
        except (OSError, asyncio.TimeoutError, WebSocketException) as exc:
            raise HubConnectError(
                f"could not connect to {self._redacted_url}: {_scrub(exc, token)}"
            ) from None
        try:
            parser, backlog = await self._handshake(ws)
        except BaseException:
            await self._discard(ws)
            raise
        return ws, parser, backlog

    async def _report_rejected(self, token: str) -> None:
        self._log.warning("The hub rejected the token (HTTP 401); discarding it")
        try:
            await self._tokens.reject(token)
        except Exception:
            self._log.exception("The token provider failed to discard a rejected token")

    async def _handshake(self, ws: ClientConnection) -> tuple[FrameParser, list[str]]:
        try:
            await self._send(ws, HANDSHAKE_REQUEST)
        except ConnectionLostError as exc:
            raise HandshakeError(f"could not send the handshake: {exc}") from None
        parser = FrameParser()
        loop = asyncio.get_running_loop()
        timeout = self._options.handshake_timeout
        deadline = loop.time() + timeout
        while True:
            try:
                data = await _recv_before(ws, deadline)
            except asyncio.TimeoutError:
                raise HandshakeError(f"no handshake response within {timeout:g} s") from None
            except ConnectionClosed as exc:
                raise HandshakeError(
                    f"the connection closed during the handshake ({_describe_close(exc)})"
                ) from None
            try:
                texts = parser.feed(data)
            except ProtocolError as exc:
                raise HandshakeError(str(exc)) from None
            if texts:
                parse_handshake_response(texts[0])
                return parser, texts[1:]

    async def _run(self, ws: ClientConnection, parser: FrameParser, backlog: list[str]) -> None:
        cause: BaseException | None = None
        attempt = 0
        serve: asyncio.Task[_ConnectionEnd] | None = None
        try:
            while True:
                serve = asyncio.create_task(self._serve(ws, parser, backlog))
                if attempt:
                    await self._replay(ws, serve)
                    if not serve.done() and not self._closing:
                        self._state = HubState.CONNECTED
                        self._log.info(
                            "Reconnected to %s (attempt %d)", self._redacted_url, attempt
                        )
                        self._emit(Reconnected(attempt))
                end = await serve
                serve = None
                self._ws = None
                if self._closing:
                    break
                if not end.allow_reconnect:
                    cause = end.cause
                    break
                self._state = HubState.RECONNECTING
                outcome = await self._reconnect(end.cause)
                if not isinstance(outcome, _Opened):
                    cause = outcome
                    break
                ws, parser, backlog, attempt = (
                    outcome.ws,
                    outcome.parser,
                    outcome.backlog,
                    outcome.attempt,
                )
                self._ws = ws
        except asyncio.CancelledError:
            raise
        except Exception as exc:  # defensive: surface bugs instead of hanging
            self._log.exception("The hub connection failed unexpectedly")
            cause = exc
        finally:
            if serve is not None and not serve.done():
                serve.cancel()
                await asyncio.gather(serve, return_exceptions=True)
            self._finish(None if self._closing else cause)

    async def _reconnect(self, cause: BaseException | None) -> _Opened | BaseException | None:
        policy = self._options.reconnect
        attempt = 0
        last = cause
        while not self._closing:
            attempt += 1
            delay = policy.delay(attempt)
            if delay is None:
                return last
            self._log.warning(
                "Connection to %s lost (%s); reconnect attempt %d in %g s",
                self._redacted_url,
                last,
                attempt,
                delay,
            )
            self._emit(Reconnecting(attempt, last))
            if delay > 0:
                await asyncio.sleep(delay)
                if self._stopping():
                    break
            try:
                ws, parser, backlog = await self._open()
            except Exception as exc:
                last = exc
                continue
            if self._stopping():
                await self._discard(ws)
                break
            return _Opened(ws, parser, backlog, attempt)
        return None

    async def _serve(
        self, ws: ClientConnection, parser: FrameParser, backlog: list[str]
    ) -> _ConnectionEnd:
        liveness = _Liveness(asyncio.get_running_loop().time())
        helpers = [
            asyncio.create_task(self._keepalive(ws)),
            asyncio.create_task(self._watchdog(ws, liveness)),
        ]
        try:
            for text in backlog:
                end = self._handle(text)
                if end is not None:
                    return end
            return await self._read(ws, parser, liveness)
        except asyncio.CancelledError:
            raise
        except Exception as exc:  # defensive: never let a bug kill the runner silently
            self._log.exception("Unexpected error in the hub read loop")
            return _ConnectionEnd(ConnectionLostError(f"read loop failed: {exc!r}"), True)
        finally:
            for helper in helpers:
                helper.cancel()
            self._fail_pending("the connection was lost")
            await asyncio.gather(*helpers, return_exceptions=True)
            await self._discard(ws)

    async def _read(
        self, ws: ClientConnection, parser: FrameParser, liveness: _Liveness
    ) -> _ConnectionEnd:
        # recv() is awaited directly: a timeout wrapper (asyncio.wait_for) could
        # swallow a cancellation on Python < 3.12. Liveness is checked by _watchdog.
        loop = asyncio.get_running_loop()
        while not self._stopping():
            try:
                data = await ws.recv()
            except ConnectionClosed as exc:
                reason = liveness.timeout_reason or (
                    f"the connection closed ({_describe_close(exc)})"
                )
                return _ConnectionEnd(ConnectionLostError(reason), True)
            liveness.last_received = loop.time()
            try:
                texts = parser.feed(data)
            except ProtocolError as exc:
                return _ConnectionEnd(ConnectionLostError(str(exc)), True)
            for text in texts:
                end = self._handle(text)
                if end is not None:
                    return end
        return _ConnectionEnd(None, False)

    async def _keepalive(self, ws: ClientConnection) -> None:
        interval = self._options.keepalive_interval
        loop = asyncio.get_running_loop()
        while True:
            delay = self._last_sent + interval - loop.time()
            if delay > 0:
                await asyncio.sleep(delay)
                continue
            try:
                await self._send(ws, PING_MESSAGE)
            except ConnectionLostError:
                return

    async def _watchdog(self, ws: ClientConnection, liveness: _Liveness) -> None:
        timeout = self._options.server_timeout
        if timeout is None:
            return
        loop = asyncio.get_running_loop()
        while True:
            remaining = liveness.last_received + timeout - loop.time()
            if remaining > 0:
                await asyncio.sleep(remaining)
                continue
            liveness.timeout_reason = f"no message from the server for {timeout:g} s"
            self._log.warning("No message from %s for %g s", self._redacted_url, timeout)
            ws.transport.abort()  # recv() then raises ConnectionClosed
            return

    async def _discard(self, ws: ClientConnection) -> None:
        with suppress(Exception):
            await asyncio.wait_for(ws.close(), self._options.close_timeout + 1.0)

    def _stopping(self) -> bool:
        # A method (not the attribute) so type checkers do not treat the flag as
        # constant across awaits.
        return self._closing

    def _finish(self, cause: BaseException | None) -> None:
        if self._state is HubState.CLOSED:
            return
        self._state = HubState.CLOSED
        self._ws = None
        self._fail_pending("the hub was closed")
        if cause is None:
            self._log.info("Hub %s closed", self._redacted_url)
        else:
            self._log.warning("Hub %s closed: %s", self._redacted_url, cause)
        self._emit(Closed(cause))
        self._events.end()
        self._handler_events.end()
        self._closed.set()

    # -- messages ----------------------------------------------------------------

    async def _send(self, ws: ClientConnection, text: str) -> None:
        async with self._send_lock:
            try:
                await ws.send(text)
            except ConnectionClosed as exc:
                raise ConnectionLostError(
                    f"the connection closed ({_describe_close(exc)})"
                ) from None
            self._last_sent = asyncio.get_running_loop().time()

    async def _invoke_on(
        self, ws: ClientConnection, method: str, arguments: tuple[Any, ...]
    ) -> Any:
        loop = asyncio.get_running_loop()
        invocation_id = str(next(self._invocation_ids))
        future: asyncio.Future[Any] = loop.create_future()
        self._pending[invocation_id] = (future, method)
        timeout = self._options.invocation_timeout
        timer = loop.call_later(timeout, _expire, future, method, timeout)
        try:
            await self._send(ws, encode_invocation(method, list(arguments), invocation_id))
            return await future
        finally:
            timer.cancel()
            self._pending.pop(invocation_id, None)
            if future.done() and not future.cancelled():
                future.exception()  # mark retrieved when an error preempted the await

    def _fail_pending(self, reason: str) -> None:
        pending, self._pending = self._pending, {}
        for future, method in pending.values():
            if not future.done():
                future.set_exception(ConnectionLostError(f"{method}: {reason}"))

    def _handle(self, text: str) -> _ConnectionEnd | None:
        try:
            message = parse_message(text)
        except ProtocolError as exc:
            self._log.warning("Ignoring a malformed hub message: %s", exc)
            return None
        if isinstance(message, InvocationMessage):
            self._dispatch(message.target, message.arguments)
        elif isinstance(message, CompletionMessage):
            entry = self._pending.pop(message.invocation_id, None)
            if entry is not None and not entry[0].done():
                future, method = entry
                if message.error is not None:
                    future.set_exception(InvocationError(method, message.error))
                else:
                    future.set_result(message.result)
        elif isinstance(message, CloseMessage):
            return _ConnectionEnd(
                ServerCloseError(message.error, allow_reconnect=message.allow_reconnect),
                message.allow_reconnect,
            )
        return None

    def _dispatch(self, target: str, arguments: tuple[Any, ...]) -> None:
        try:
            events = self._decoder(target, arguments)
        except Exception as exc:  # decoders never raise; stay defensive anyway
            events = [UnknownEvent(target, arguments, exc)]
        for event in events:
            if isinstance(event, UnknownEvent) and event.error is not None:
                self._log.warning("Could not decode a %s event: %s", target, event.error)
            self._emit(event)

    def _emit(self, event: HubEvent) -> None:
        self._events.put(event)
        if self._handlers:
            self._handler_events.put(event)

    async def _dispatch_handlers(self) -> None:
        while True:
            event = await self._handler_events.get()
            if event is None:
                return
            for event_type, callback in list(self._handlers):
                if not isinstance(event, event_type):
                    continue
                try:
                    result = callback(event)
                    if inspect.isawaitable(result):
                        await result
                except asyncio.CancelledError:
                    raise
                except Exception:
                    self._log.exception("An event callback raised")

    async def _drain_dispatcher(self) -> None:
        task = self._dispatcher
        if task is None or task.done() or task is asyncio.current_task():
            return
        try:
            await asyncio.wait_for(asyncio.shield(task), self._options.close_timeout)
        except asyncio.TimeoutError:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
