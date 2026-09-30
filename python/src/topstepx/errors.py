"""Exceptions raised by the SDK.

Every exception derives from :class:`TopstepXError`, so ``except TopstepXError``
catches everything this package raises on purpose::

    TopstepXError
    ├── ApiError                 business failure reported in an HTTP 200 body
    ├── HTTPStatusError          non-2xx HTTP response
    │   ├── UnauthorizedError    HTTP 401 (after the one re-login retry)
    │   └── RateLimitError       HTTP 429 (after the retry budget is spent)
    ├── TransportError           DNS, connection, TLS or timeout failure
    ├── DecodeError              a body or payload that cannot be decoded
    ├── ValidationError          client-side request validation (also a ValueError)
    ├── NotAuthenticatedError    no token and no credentials to obtain one
    └── HubError                 realtime (SignalR) failures
        ├── HubConnectError      the WebSocket could not be opened
        │   ├── TokenRejectedError  the upgrade was rejected with HTTP 401
        │   └── HandshakeError   the SignalR handshake failed
        ├── NotConnectedError    invocation while the hub is not connected
        ├── ConnectionLostError  the connection dropped under a pending call
        ├── ServerCloseError     the server sent a close message
        ├── InvocationError      the server completed an invocation with an error
        └── HubTimeoutError      an invocation timed out (also a TimeoutError)

No exception message, ``repr`` or attribute contains an API key or a token.
"""

from __future__ import annotations

from collections.abc import Sequence
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from .models import ProblemDetails, ResponseEnvelope

__all__ = [
    "ApiError",
    "ConnectionLostError",
    "DecodeError",
    "HTTPStatusError",
    "HandshakeError",
    "HubConnectError",
    "HubError",
    "HubTimeoutError",
    "InvocationError",
    "NotAuthenticatedError",
    "NotConnectedError",
    "RateLimitError",
    "ServerCloseError",
    "TokenRejectedError",
    "TopstepXError",
    "TransportError",
    "UnauthorizedError",
    "ValidationError",
]

_BODY_PREVIEW = 500


def _rebuild(cls: type[TopstepXError], args: tuple[Any, ...], state: dict[str, Any]) -> Any:
    error = cls.__new__(cls)
    error.args = args
    error.__dict__.update(state)
    return error


def _preview(text: str | None) -> str:
    if not text:
        return ""
    text = " ".join(text.split())
    if len(text) > _BODY_PREVIEW:
        return text[:_BODY_PREVIEW] + "..."
    return text


class TopstepXError(Exception):
    """Base class of every exception raised by this SDK."""

    def __reduce__(self) -> tuple[Any, ...]:
        # Subclasses take keyword-only constructor arguments; rebuild from state so
        # that errors survive pickling (for example across process pools).
        return (_rebuild, (type(self), self.args, dict(self.__dict__)))


class ApiError(TopstepXError):
    """A business failure reported in the response body.

    The server answered (usually with HTTP 200) but the body reports
    ``success == false`` or a non-zero ``errorCode``.

    Attributes:
        operation_id: The operation that failed, for example ``"Order_PlaceOrder"``.
        path: The request path, for example ``"/api/Order/place"``.
        code: The raw integer ``errorCode``.
        code_name: The enum name of ``code`` (``"InvalidCredentials"``), or
            ``"Unrecognized(<n>)"`` when the operation's enum lacks the value.
        error_message: The server's ``errorMessage``; may be ``None``.
        order_id: For ``Order_PlaceOrder`` only, the ``orderId`` the server returned
            with the failure, if any. A returned identifier does not mean the order
            was accepted.
        response: The complete typed response envelope.
    """

    def __init__(
        self,
        *,
        operation_id: str,
        path: str,
        code: int,
        code_name: str,
        error_message: str | None,
        response: ResponseEnvelope | None = None,
        order_id: int | None = None,
    ) -> None:
        self.operation_id = operation_id
        self.path = path
        self.code = code
        self.code_name = code_name
        self.error_message = error_message
        self.order_id = order_id
        self.response = response
        text = f"{operation_id} failed with errorCode {code} ({code_name})"
        if error_message:
            text += f": {error_message}"
        if order_id is not None:
            text += f" [orderId={order_id}]"
        super().__init__(text)


class HTTPStatusError(TopstepXError):
    """The server answered with a non-2xx HTTP status.

    Attributes:
        operation_id: The operation that was called.
        path: The request path.
        status_code: The HTTP status code.
        body: The response body as text (may be empty).
        problem: The body parsed as RFC 7807 ``ProblemDetails`` when it is a JSON
            object, including extension members; otherwise ``None``.
    """

    def __init__(
        self,
        *,
        operation_id: str,
        path: str,
        status_code: int,
        body: str,
        problem: ProblemDetails | None = None,
        message: str | None = None,
    ) -> None:
        self.operation_id = operation_id
        self.path = path
        self.status_code = status_code
        self.body = body
        self.problem = problem
        if message is None:
            message = f"{operation_id} ({path}) failed with HTTP {status_code}"
            detail = None
            if problem is not None:
                detail = " - ".join(p for p in (problem.title, problem.detail) if p)
            detail = detail or _preview(body)
            if detail:
                message += f": {detail}"
        super().__init__(message)


class UnauthorizedError(HTTPStatusError):
    """HTTP 401: the token is missing, invalid or expired.

    Raised after the SDK has already tried one re-login (when credentials are
    configured) and the retried request was still rejected.
    """


class RateLimitError(HTTPStatusError):
    """HTTP 429: the request was throttled and the retry budget is exhausted.

    Attributes:
        retry_after: The server's ``Retry-After`` value in seconds, if present.
    """

    def __init__(
        self,
        *,
        operation_id: str,
        path: str,
        status_code: int,
        body: str,
        problem: ProblemDetails | None = None,
        retry_after: float | None = None,
    ) -> None:
        self.retry_after = retry_after
        message = f"{operation_id} ({path}) was rate limited (HTTP {status_code})"
        if retry_after is not None:
            message += f"; retry after {retry_after:g} s"
        super().__init__(
            operation_id=operation_id,
            path=path,
            status_code=status_code,
            body=body,
            problem=problem,
            message=message,
        )


class TransportError(TopstepXError):
    """The request could not be completed: DNS, connection, TLS or timeout failure.

    The underlying ``httpx`` exception is available as ``__cause__``.

    Attributes:
        operation_id: The operation that was called.
        path: The request path.
        timeout: ``True`` when the failure was a timeout.
    """

    def __init__(self, *, operation_id: str, path: str, reason: str, timeout: bool = False) -> None:
        self.operation_id = operation_id
        self.path = path
        self.timeout = timeout
        super().__init__(f"{operation_id} ({path}) failed: {reason}")


class DecodeError(TopstepXError, ValueError):
    """A response body or realtime payload could not be decoded.

    Attributes:
        message: What went wrong.
        path: The JSON path of the offending value, such as ``"orders[0].status"``.
        body: The raw body text, for REST responses.
        operation_id: The operation, for REST responses.
    """

    def __init__(
        self,
        message: str,
        *,
        path: str | None = None,
        body: str | None = None,
        operation_id: str | None = None,
    ) -> None:
        self.message = message
        self.path = path
        self.body = body
        self.operation_id = operation_id
        text = message
        if path:
            text = f"{path}: {text}"
        if operation_id:
            text = f"{operation_id}: {text}"
        super().__init__(text)


class ValidationError(TopstepXError, ValueError):
    """A request failed client-side validation before any network I/O.

    Validation reproduces the rules of the specification (see
    ``SDK_BEHAVIOR.md`` section 9) and can be disabled per client with
    ``validate_requests=False``; the server remains the final authority.

    Attributes:
        request_type: Name of the request model that failed validation.
        violations: One human-readable message per violated rule.
    """

    def __init__(self, request_type: str, violations: Sequence[str]) -> None:
        self.request_type = request_type
        self.violations = tuple(violations)
        super().__init__(f"invalid {request_type}: " + "; ".join(self.violations))


class NotAuthenticatedError(TopstepXError):
    """No token is held and no credentials are configured to obtain one.

    Raised for an authenticated operation before any network I/O.
    """


class HubError(TopstepXError):
    """Base class of realtime (SignalR hub) errors."""


class HubConnectError(HubError):
    """The hub WebSocket could not be opened.

    Causes include network and TLS failures and an HTTP status on the upgrade.

    Attributes:
        status_code: The HTTP status of a rejected upgrade request, if any.
    """

    def __init__(self, message: str, *, status_code: int | None = None) -> None:
        self.status_code = status_code
        super().__init__(message)


class TokenRejectedError(HubConnectError):
    """The hub rejected the token: the WebSocket upgrade returned HTTP 401.

    The rejected token has already been reported to the token provider; a
    provider backed by ``Client``/``AsyncClient`` discards it when it is still
    the stored token, so the next attempt (a reconnect, or another ``start()``)
    logs in again instead of reusing it. The message never contains the token.
    """


class HandshakeError(HubConnectError):
    """The SignalR handshake failed.

    The server returned an error, the response was malformed, or it did not
    arrive within the handshake timeout.
    """


class NotConnectedError(HubError):
    """A hub invocation was attempted while the hub is not connected.

    The hub was not started, is reconnecting, or is closed. Invocations fail
    fast instead of queueing.
    """


class ConnectionLostError(HubError):
    """The hub connection dropped, or the hub was closed, under a pending call.

    Also used as the cause when the connection is detected as dead.
    """


class ServerCloseError(HubError):
    """The server sent a SignalR close message.

    Attributes:
        error: The server's error text, if any.
        allow_reconnect: Whether the server allowed the client to reconnect.
    """

    def __init__(self, error: str | None, *, allow_reconnect: bool) -> None:
        self.error = error
        self.allow_reconnect = allow_reconnect
        text = "server closed the connection"
        if error:
            text += f": {error}"
        super().__init__(text)


class InvocationError(HubError):
    """The server completed a hub invocation with an error.

    Attributes:
        method: The hub method that was invoked.
        error: The server's error text.
    """

    def __init__(self, method: str, error: str) -> None:
        self.method = method
        self.error = error
        super().__init__(f"hub method {method} failed: {error}")


class HubTimeoutError(HubError, TimeoutError):
    """A hub invocation did not complete within the invocation timeout."""
