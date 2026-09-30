"""The request pipeline shared by the sync and async clients.

Internal module. Every piece of behavior (validation, encoding, the
``ensure_token`` table, single-flight refresh, the 401 re-login rule, retries,
backoff, ``Retry-After``, rate limiting, error mapping and decoding) lives here,
written once as *flows*: generators that ``yield`` the I/O they need as small
effect objects and receive the results back:

* :class:`SendRequest` - perform an HTTP request, send back the ``httpx.Response``
  (or throw the ``httpx`` exception into the flow);
* :class:`Sleep` - wait for the given number of seconds;
* :data:`ACQUIRE_AUTH_LOCK` - acquire the client's auth lock (a
  ``threading.Lock`` or an ``asyncio.Lock``) and send it back; the flow releases
  it itself with the lock's synchronous ``release()``.

The sync client runs a flow with blocking calls and the async client with
``await``; the drivers (``Client._run`` / ``AsyncClient._run``) are the only
code that differs between them.
"""

from __future__ import annotations

import dataclasses
import itertools
import logging
import threading
import time
from collections.abc import Callable, Generator
from dataclasses import dataclass
from typing import Any, Protocol, TypeVar, cast

import httpx

from ._auth import (
    RENEWAL_COOLDOWN,
    Credentials,
    Session,
    TokenAction,
    new_session,
    plan_token,
)
from ._config import USER_AGENT, ClientSettings
from ._ratelimit import RateLimiter
from ._retry import RETRYABLE_READ_STATUSES, is_retryable_transport_error, parse_retry_after
from ._serde import dumps, loads
from ._validation import validate_request
from .errors import (
    ApiError,
    DecodeError,
    HTTPStatusError,
    NotAuthenticatedError,
    RateLimitError,
    TopstepXError,
    TransportError,
    UnauthorizedError,
)
from .models import (
    LoginApiKeyRequest,
    LoginAppRequest,
    LoginResponse,
    PlaceOrderResponse,
    ProblemDetails,
    RequestModel,
    ResponseEnvelope,
    ValidateResponse,
)
from .operations import (
    AUTH_LOGIN_APP,
    AUTH_LOGIN_KEY,
    AUTH_LOGOUT,
    AUTH_VALIDATE,
    AuthMode,
    Operation,
    RetryClass,
    by_id,
    for_request,
)

T = TypeVar("T")
logger = logging.getLogger("topstepx")


# ---------------------------------------------------------------------------
# Effects
# ---------------------------------------------------------------------------


@dataclass(frozen=True, slots=True)
class SendRequest:
    """Effect: send one HTTP request and return the (fully read) response."""

    method: str
    url: str
    headers: dict[str, str]
    content: bytes | None

    def __repr__(self) -> str:  # never show the Authorization header
        return f"SendRequest({self.method} {self.url})"


@dataclass(frozen=True, slots=True)
class Sleep:
    """Effect: wait ``seconds`` before continuing."""

    seconds: float


class AcquireAuthLock:
    """Effect: acquire the client's auth lock and return it."""

    __slots__ = ()

    def __repr__(self) -> str:
        return "ACQUIRE_AUTH_LOCK"


ACQUIRE_AUTH_LOCK = AcquireAuthLock()

Effect = SendRequest | Sleep | AcquireAuthLock
Flow = Generator[Effect, Any, T]


class Releasable(Protocol):
    """What the flow needs from a lock it was handed: synchronous release."""

    def release(self) -> None:
        """Release the lock."""


# ---------------------------------------------------------------------------
# Response helpers
# ---------------------------------------------------------------------------


def response_text(response: httpx.Response) -> str:
    """The body as text, whatever the ``Content-Type``, without a UTF-8 BOM."""
    try:
        text = response.content.decode(response.encoding or "utf-8")
    except (LookupError, UnicodeDecodeError):
        text = response.content.decode("utf-8", errors="replace")
    return text.removeprefix("﻿")


def parse_ping(text: str) -> str:
    """``Status_Ping`` answers ``pong`` as text/plain or ``"pong"`` as JSON."""
    stripped = text.strip()
    try:
        value = loads(stripped)
    except ValueError:
        return stripped
    return value if isinstance(value, str) else stripped


def _safe_body(model_type: type[ResponseEnvelope], text: str) -> str | None:
    """The body to attach to a :class:`DecodeError`, with secret members redacted.

    Login and validate responses carry tokens; their bodies are attached only
    when the secret members can be redacted, and dropped otherwise.
    """
    secrets = {spec.json_name for spec in model_type.__wire_fields__ if spec.secret}
    if not secrets:
        return text
    try:
        data = loads(text)
    except ValueError:
        return None
    if not isinstance(data, dict):
        return None
    lowered = {name.lower() for name in secrets}
    redacted = {
        key: "<redacted>" if isinstance(key, str) and key.lower() in lowered else value
        for key, value in data.items()
    }
    return dumps(redacted)


def decode_response(op: Operation[Any, T], response: httpx.Response) -> T:
    """Decode a 2xx response body into ``op.response_type``.

    The body is parsed as JSON whatever its ``Content-Type``.

    Raises:
        DecodeError: If the body is not valid JSON or does not match the model.
            The attached body never contains a token.
    """
    text = response_text(response)
    if op.response_type is str:
        return cast("T", parse_ping(text))
    model_type = cast("type[ResponseEnvelope]", op.response_type)
    try:
        data = loads(text)
    except ValueError as exc:
        raise DecodeError(
            f"response body is not valid JSON ({exc})",
            body=_safe_body(model_type, text),
            operation_id=op.operation_id,
        ) from None
    if not isinstance(data, dict):
        raise DecodeError(
            "response body is not a JSON object",
            body=_safe_body(model_type, text),
            operation_id=op.operation_id,
        )
    try:
        return cast("T", model_type.from_dict(data))
    except DecodeError as exc:
        raise DecodeError(
            exc.message,
            path=exc.path,
            body=_safe_body(model_type, text),
            operation_id=op.operation_id,
        ) from None


def raise_for_business_failure(op: Operation[Any, Any], result: object) -> None:
    """Raise :class:`ApiError` when ``result`` is a business failure."""
    if not isinstance(result, ResponseEnvelope) or not result.is_business_failure:
        return
    order_id = result.order_id if isinstance(result, PlaceOrderResponse) else None
    raise ApiError(
        operation_id=op.operation_id,
        path=op.path,
        code=int(result.error_code),
        code_name=result.error_code_name,
        error_message=result.error_message,
        response=result,
        order_id=order_id,
    )


def require(value: T | None, op: Operation[Any, Any], field: str) -> T:
    """Return ``value``, or raise :class:`DecodeError` if a payload is missing.

    Used when a successful response lacks the payload the operation promises.
    """
    if value is None:
        raise DecodeError(f"successful response has no {field!r}", operation_id=op.operation_id)
    return value


def problem_details(text: str) -> ProblemDetails | None:
    """Parse an error body as RFC 7807 ``ProblemDetails`` when it is a JSON object."""
    try:
        data = loads(text)
    except ValueError:
        return None
    if not isinstance(data, dict):
        return None
    try:
        return ProblemDetails.from_dict(data)
    except DecodeError:
        return None


def resolve_target(target: object, request: object = None) -> tuple[Operation[Any, Any], object]:
    """Resolve the argument of ``execute`` into ``(operation, request)``."""
    if isinstance(target, RequestModel):
        if request is not None:
            raise TypeError("pass either a request model or an operation, not both")
        return for_request(target), target
    if isinstance(target, str):
        target = by_id(target)
    if isinstance(target, Operation):
        return target, request
    raise TypeError(
        f"execute() expects a request model, an Operation or an operationId, "
        f"not {type(target).__name__}"
    )


# ---------------------------------------------------------------------------
# The shared client core
# ---------------------------------------------------------------------------


class ClientCore:
    """Configuration and mutable session state of one client, plus its flows.

    Thread-safety: the session is an immutable snapshot replaced atomically;
    token mutations that need exclusion run under the auth lock provided by the
    driver; the rate limiter has its own lock.
    """

    def __init__(
        self, settings: ClientSettings, credentials: Credentials | None, token: str | None
    ) -> None:
        self.settings = settings
        self.credentials = credentials
        self.clock: Callable[[], float] = time.time
        self.limiter = RateLimiter(settings.rate_limits)
        self._generations = itertools.count(1)
        self._session_lock = threading.Lock()  # guards compare-and-replace; never held over I/O
        self._session: Session | None = None
        self._refresh_epoch = 0
        if token:
            self.store_token(token)

    # -- session -----------------------------------------------------------

    @property
    def session(self) -> Session | None:
        """The current session snapshot, if a token is held."""
        return self._session

    def store_token(self, token: str) -> Session:
        """Store ``token`` as the current session and return the snapshot."""
        with self._session_lock:
            session = new_session(token, self.clock(), next(self._generations))
            self._session = session
        return session

    def clear_session(self) -> None:
        """Forget the stored token."""
        with self._session_lock:
            self._session = None

    def block_renewal(self, session: Session) -> Session:
        """Start the renewal cooldown for ``session`` if it is still the stored one.

        Returns the session to keep using.
        """
        with self._session_lock:
            current = self._session
            if current is None or current.generation != session.generation:
                return current if current is not None else session
            blocked = dataclasses.replace(
                current, renewal_blocked_until=self.clock() + RENEWAL_COOLDOWN
            )
            self._session = blocked
        return blocked

    def invalidate_token(self, token: str) -> bool:
        """Forget the stored token if it is still ``token``; return whether it was."""
        with self._session_lock:
            session = self._session
            if session is None or session.token != token:
                return False
            self._session = None
        logger.info("Discarded a token that was rejected by the server")
        return True

    # -- public flows --------------------------------------------------------

    def call(self, op: Operation[Any, T], request: object) -> Flow[T]:
        """Perform ``op`` and raise :class:`ApiError` on a business failure."""
        return self.execute(op, request, check=True)

    def execute(self, op: Operation[Any, T], request: object, *, check: bool) -> Flow[T]:
        """Perform ``op`` and return the typed response (the escape hatch).

        HTTP, transport and decode errors are raised; business failures are
        raised only when ``check`` is true. The stored session is never changed
        except by the automatic login/refresh machinery.
        """
        body = self.prepare(op, request)
        session: Session | None
        if op.auth is AuthMode.BEARER:
            session = yield from self.ensure_session()
        elif op.auth is AuthMode.OPTIONAL:
            session = yield from self.optional_session()
        else:
            session = None
        response = yield from self.send(op, body, session, relogin=True)
        result = decode_response(op, response)
        if check:
            raise_for_business_failure(op, result)
        return result

    def get_token(self) -> Flow[str]:
        """Run ``ensure_token`` and return the resulting token."""
        session = yield from self.ensure_session()
        return session.token

    def login_key(self, user_name: str, api_key: str) -> Flow[str]:
        """``Auth_LoginKey`` with explicit credentials; stores the token."""
        body = self.prepare(
            AUTH_LOGIN_KEY, LoginApiKeyRequest(user_name=user_name, api_key=api_key)
        )
        return (yield from self._explicit_login(AUTH_LOGIN_KEY, body))

    def login_app(self, request: LoginAppRequest) -> Flow[str]:
        """``Auth_LoginApp`` (unsupported by the current guide); stores the token."""
        body = self.prepare(AUTH_LOGIN_APP, request)
        return (yield from self._explicit_login(AUTH_LOGIN_APP, body))

    def validate(self) -> Flow[str]:
        """``Auth_Validate`` with the current token; stores and returns ``newToken``."""
        session = self._session
        if session is None:
            session = yield from self.ensure_session()
        response = yield from self.send(AUTH_VALIDATE, None, session, relogin=True)
        result = decode_response(AUTH_VALIDATE, response)
        raise_for_business_failure(AUTH_VALIDATE, result)
        new_token = require(result.new_token or None, AUTH_VALIDATE, "newToken")
        self.store_token(new_token)
        return new_token

    def logout(self) -> Flow[None]:
        """``Auth_Logout``; clears the stored token whatever the outcome."""
        session = self._session
        if session is None:
            return
        try:
            response = yield from self.send(AUTH_LOGOUT, None, session, relogin=False)
            result = decode_response(AUTH_LOGOUT, response)
            raise_for_business_failure(AUTH_LOGOUT, result)
        finally:
            self.clear_session()

    # -- building blocks -----------------------------------------------------

    def prepare(self, op: Operation[Any, Any], request: object) -> bytes | None:
        """Validate and encode the request body of ``op`` (no I/O)."""
        if op.request_type is None:
            if request is not None:
                raise TypeError(f"{op.operation_id} takes no request body")
            return None
        if not isinstance(request, op.request_type):
            raise TypeError(
                f"{op.operation_id} expects {op.request_type.__name__}, "
                f"not {type(request).__name__}"
            )
        if self.settings.validate_requests:
            validate_request(request)
        model = cast("RequestModel[Any]", request)
        return dumps(model.to_dict()).encode("utf-8")

    def _plan(self, session: Session | None) -> TokenAction:
        return plan_token(
            session, self.clock(), self.settings.refresh_margin, self.credentials is not None
        )

    def _not_authenticated(self) -> NotAuthenticatedError:
        return NotAuthenticatedError(
            "no token is held and no credentials are configured; pass username and "
            "api_key (or token) to the client, or call auth.login_key()"
        )

    def ensure_session(self) -> Flow[Session]:
        """``ensure_token()``: return a usable session, logging in or refreshing first.

        Refreshes and logins are single-flight across concurrent callers.
        """
        session = self._session
        action = self._plan(session)
        if action is TokenAction.USE and session is not None:
            return session
        if action is TokenAction.FAIL:
            raise self._not_authenticated()
        epoch = self._refresh_epoch
        lock = cast("Releasable", (yield ACQUIRE_AUTH_LOCK))
        try:
            session = self._session
            action = self._plan(session)
            if action is TokenAction.USE and session is not None:
                return session
            if action is TokenAction.FAIL:
                raise self._not_authenticated()
            if session is not None and epoch != self._refresh_epoch:
                # A refresh finished while we waited for the lock: use its outcome
                # instead of starting another one.
                return session
            try:
                if action is TokenAction.LOGIN or session is None:
                    return (yield from self._login_with_credentials())
                return (yield from self._refresh(session))
            finally:
                self._refresh_epoch += 1
        finally:
            lock.release()

    def optional_session(self) -> Flow[Session | None]:
        """Session for ``Status_Ping``: the token when held or obtainable, else ``None``.

        ``None`` means ping anonymously rather than failing.
        """
        if self._session is None and self.credentials is None:
            return None
        try:
            return (yield from self.ensure_session())
        except Exception as exc:  # anything but cancellation: ping anonymously
            logger.debug("Pinging anonymously: %s", type(exc).__name__)
            return None

    def _credentials_body(self) -> bytes:
        credentials = self.credentials
        if credentials is None:
            raise self._not_authenticated()
        request = LoginApiKeyRequest(user_name=credentials.username, api_key=credentials.api_key)
        return dumps(request.to_dict()).encode("utf-8")

    def _login_with_credentials(self) -> Flow[Session]:
        return (yield from self._login(AUTH_LOGIN_KEY, self._credentials_body()))

    def _login(self, op: Operation[Any, LoginResponse], body: bytes | None) -> Flow[Session]:
        response = yield from self.send(op, body, None, relogin=False)
        result = decode_response(op, response)
        raise_for_business_failure(op, result)
        token = require(result.token or None, op, "token")
        session = self.store_token(token)
        logger.info("Logged in (%s)", op.operation_id)
        return session

    def _explicit_login(self, op: Operation[Any, LoginResponse], body: bytes | None) -> Flow[str]:
        lock = cast("Releasable", (yield ACQUIRE_AUTH_LOCK))
        try:
            session = yield from self._login(op, body)
            self._refresh_epoch += 1
            return session.token
        finally:
            lock.release()

    def _refresh(self, session: Session) -> Flow[Session]:
        """Renew a token inside the refresh margin (auth lock held)."""
        try:
            response = yield from self.send(AUTH_VALIDATE, None, session, relogin=False)
            result: ValidateResponse = decode_response(AUTH_VALIDATE, response)
            raise_for_business_failure(AUTH_VALIDATE, result)
            new_token = require(result.new_token or None, AUTH_VALIDATE, "newToken")
        except TopstepXError as exc:
            if self.credentials is not None:
                logger.info("Token refresh failed (%s); logging in again", type(exc).__name__)
                return (yield from self._login_with_credentials())
            logger.warning(
                "Token refresh failed (%s); continuing with the current token and "
                "not retrying the refresh for %g s",
                type(exc).__name__,
                RENEWAL_COOLDOWN,
            )
            return self.block_renewal(session)
        logger.debug("Token renewed through Auth_Validate")
        return self.store_token(new_token)

    def _relogin_after_401(self, stale: Session) -> Flow[Session]:
        """Log in again after HTTP 401, unless another caller replaced the token.

        When another caller already replaced the token, that token is used instead.
        """
        lock = cast("Releasable", (yield ACQUIRE_AUTH_LOCK))
        try:
            current = self._session
            if current is not None and current.generation != stale.generation:
                return current
            try:
                return (yield from self._login_with_credentials())
            finally:
                self._refresh_epoch += 1
        finally:
            lock.release()

    def _acquire_slot(self, op: Operation[Any, Any]) -> Flow[None]:
        while True:
            wait = self.limiter.reserve(op.bucket)
            if wait <= 0:
                return
            logger.debug("Rate limit reached for %s; waiting %.3f s", op.bucket.value, wait)
            yield Sleep(wait)

    def build_request(
        self, op: Operation[Any, Any], body: bytes | None, session: Session | None
    ) -> SendRequest:
        """Build the HTTP request of one attempt."""
        headers = {"Accept": "application/json", "User-Agent": USER_AGENT}
        if session is not None:
            headers["Authorization"] = f"Bearer {session.token}"
        content: bytes | None = None
        if op.method == "POST":
            content = body if body is not None else b""
            if body is not None:
                headers["Content-Type"] = "application/json"
        return SendRequest(op.method, self.settings.base_url + op.path, headers, content)

    def send(
        self,
        op: Operation[Any, Any],
        body: bytes | None,
        session: Session | None,
        *,
        relogin: bool,
    ) -> Flow[httpx.Response]:
        """Send ``op`` until it succeeds or fails for good; return the 2xx response.

        Applies rate limiting before every attempt, the retry rules of section 7
        and the 401 re-login rule of section 5, and maps failures to SDK errors.
        """
        settings = self.settings
        retries = 0
        relogged = False
        while True:
            yield from self._acquire_slot(op)
            try:
                response = cast("httpx.Response", (yield self.build_request(op, body, session)))
            except httpx.DecodingError as exc:
                raise DecodeError(
                    f"could not decode the response body ({exc})", operation_id=op.operation_id
                ) from exc
            except httpx.TransportError as exc:
                if (
                    op.retry is RetryClass.READ
                    and retries < settings.max_retries
                    and is_retryable_transport_error(exc)
                ):
                    retries += 1
                    delay = settings.backoff.delay(retries)
                    logger.warning(
                        "%s failed (%s); retry %d/%d in %.2f s",
                        op.operation_id,
                        type(exc).__name__,
                        retries,
                        settings.max_retries,
                        delay,
                    )
                    yield Sleep(delay)
                    continue
                raise TransportError(
                    operation_id=op.operation_id,
                    path=op.path,
                    reason=f"{type(exc).__name__}: {exc}",
                    timeout=isinstance(exc, httpx.TimeoutException),
                ) from exc
            except httpx.RequestError as exc:
                raise TransportError(
                    operation_id=op.operation_id,
                    path=op.path,
                    reason=f"{type(exc).__name__}: {exc}",
                ) from exc

            status = response.status_code
            logger.debug("%s %s -> HTTP %d", op.method, op.path, status)
            if 200 <= status < 300:
                return response

            if status == 401:
                if (
                    relogin
                    and not relogged
                    and session is not None
                    and self.credentials is not None
                    and op.auth is not AuthMode.NONE
                ):
                    relogged = True
                    logger.info("%s returned HTTP 401; logging in again", op.operation_id)
                    session = yield from self._relogin_after_401(session)
                    continue
                text = response_text(response)
                raise UnauthorizedError(
                    operation_id=op.operation_id,
                    path=op.path,
                    status_code=status,
                    body=text,
                    problem=problem_details(text),
                )

            if status == 429:
                retry_after = parse_retry_after(response.headers.get("Retry-After"))
                if retries < settings.max_retries:
                    retries += 1
                    delay = (
                        retry_after if retry_after is not None else settings.backoff.delay(retries)
                    )
                    logger.warning(
                        "%s was rate limited (HTTP 429); retry %d/%d in %.2f s",
                        op.operation_id,
                        retries,
                        settings.max_retries,
                        delay,
                    )
                    yield Sleep(delay)
                    continue
                text = response_text(response)
                raise RateLimitError(
                    operation_id=op.operation_id,
                    path=op.path,
                    status_code=status,
                    body=text,
                    problem=problem_details(text),
                    retry_after=retry_after,
                )

            if (
                status in RETRYABLE_READ_STATUSES
                and op.retry is RetryClass.READ
                and retries < settings.max_retries
            ):
                retries += 1
                delay = settings.backoff.delay(retries)
                logger.warning(
                    "%s returned HTTP %d; retry %d/%d in %.2f s",
                    op.operation_id,
                    status,
                    retries,
                    settings.max_retries,
                    delay,
                )
                yield Sleep(delay)
                continue

            text = response_text(response)
            raise HTTPStatusError(
                operation_id=op.operation_id,
                path=op.path,
                status_code=status,
                body=text,
                problem=problem_details(text),
            )
