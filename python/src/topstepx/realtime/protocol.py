"""The ASP.NET Core SignalR JSON hub protocol (version 1), without a SignalR library.

Every message is JSON text terminated by the record separator ``0x1E``. A
WebSocket frame may carry several messages, and a message may span frames;
:class:`FrameParser` handles both, for text and binary frames.

Message types used by this SDK: Invocation (1), Completion (3), Ping (6) and
Close (7). Stream messages (2, 4, 5) and unknown types parse as
:class:`OtherMessage` and are ignored by the connection.
"""

from __future__ import annotations

import codecs
from dataclasses import dataclass, field
from typing import Any

from .._serde import dumps, loads
from ..errors import HandshakeError

__all__ = [
    "CLOSE_MESSAGE",
    "HANDSHAKE_REQUEST",
    "PING_MESSAGE",
    "RECORD_SEPARATOR",
    "CloseMessage",
    "CompletionMessage",
    "FrameParser",
    "HubMessage",
    "InvocationMessage",
    "MessageType",
    "OtherMessage",
    "PingMessage",
    "ProtocolError",
    "encode_invocation",
    "encode_message",
    "parse_handshake_response",
    "parse_message",
]

RECORD_SEPARATOR = "\x1e"
"""Terminates every SignalR JSON message."""


class MessageType:
    """SignalR hub message type numbers."""

    INVOCATION = 1
    STREAM_ITEM = 2
    COMPLETION = 3
    STREAM_INVOCATION = 4
    CANCEL_INVOCATION = 5
    PING = 6
    CLOSE = 7


class ProtocolError(ValueError):
    """A hub message or frame violates the JSON hub protocol."""


def encode_message(message: dict[str, Any]) -> str:
    """Serialize one hub message, including the trailing record separator."""
    return dumps(message) + RECORD_SEPARATOR


HANDSHAKE_REQUEST = encode_message({"protocol": "json", "version": 1})
"""The handshake request: ``{"protocol":"json","version":1}`` + ``0x1E``."""
PING_MESSAGE = encode_message({"type": MessageType.PING})
"""A keepalive ping message."""
CLOSE_MESSAGE = encode_message({"type": MessageType.CLOSE})
"""The close message the client sends when it closes the connection."""


def encode_invocation(target: str, arguments: list[Any], invocation_id: str | None) -> str:
    """Serialize an invocation; ``invocation_id=None`` means no completion is expected."""
    message: dict[str, Any] = {"type": MessageType.INVOCATION}
    if invocation_id is not None:
        message["invocationId"] = invocation_id
    message["target"] = target
    message["arguments"] = arguments
    return encode_message(message)


class FrameParser:
    """Splits WebSocket frames into complete JSON messages.

    Incomplete trailing text is buffered until the next frame. Binary frames are
    decoded as UTF-8 incrementally, so a character split across frames survives.

    Args:
        max_buffer: Maximum size (in characters) of an unterminated message.
    """

    def __init__(self, max_buffer: int = 64 * 1024 * 1024) -> None:
        self._buffer = ""
        self._decoder = codecs.getincrementaldecoder("utf-8")()
        self._max_buffer = max_buffer

    @property
    def pending(self) -> str:
        """Buffered text of an incomplete message."""
        return self._buffer

    def feed(self, data: str | bytes | bytearray | memoryview) -> list[str]:
        """Add one frame and return the complete messages it finishes.

        Separators are removed and blank messages are skipped.

        Raises:
            ProtocolError: If the buffer exceeds ``max_buffer`` or a binary frame
                is not valid UTF-8.
        """
        if isinstance(data, str):
            text = data
        else:
            try:
                text = self._decoder.decode(bytes(data))
            except UnicodeDecodeError as exc:
                raise ProtocolError(f"binary frame is not valid UTF-8: {exc}") from None
        buffer = self._buffer + text
        if RECORD_SEPARATOR not in buffer:
            if len(buffer) > self._max_buffer:
                self._buffer = ""
                raise ProtocolError("unterminated hub message exceeds the maximum size")
            self._buffer = buffer
            return []
        *complete, self._buffer = buffer.split(RECORD_SEPARATOR)
        return [message for message in complete if message.strip()]


@dataclass(frozen=True, slots=True)
class InvocationMessage:
    """Type 1. From the server (without ``invocation_id``) it is an event."""

    target: str
    arguments: tuple[Any, ...]
    invocation_id: str | None = None


@dataclass(frozen=True, slots=True)
class CompletionMessage:
    """Type 3: the result (or error) of the invocation ``invocation_id``."""

    invocation_id: str
    result: Any = None
    error: str | None = None


@dataclass(frozen=True, slots=True)
class PingMessage:
    """Type 6: keepalive."""


@dataclass(frozen=True, slots=True)
class CloseMessage:
    """Type 7: the server is closing the connection."""

    error: str | None = None
    allow_reconnect: bool = False


@dataclass(frozen=True, slots=True)
class OtherMessage:
    """Any other message type (streaming or unknown); ignored."""

    type: int
    raw: dict[str, Any] = field(repr=False, compare=False)


HubMessage = InvocationMessage | CompletionMessage | PingMessage | CloseMessage | OtherMessage


def _optional_str(value: Any, name: str) -> str | None:
    if value is None:
        return None
    if isinstance(value, str):
        return value
    raise ProtocolError(f"{name} must be a string")


def parse_message(text: str) -> HubMessage:
    """Parse one JSON hub message (without its separator).

    Numbers decode exactly (``Decimal`` for non-integers).

    Raises:
        ProtocolError: If the text is not a valid hub message.
    """
    try:
        data = loads(text)
    except ValueError as exc:
        raise ProtocolError(f"invalid JSON in hub message: {exc}") from None
    if not isinstance(data, dict):
        raise ProtocolError("hub message is not a JSON object")
    kind = data.get("type")
    if isinstance(kind, bool) or not isinstance(kind, int):
        raise ProtocolError("hub message has no integer 'type'")
    if kind == MessageType.INVOCATION:
        target = data.get("target")
        if not isinstance(target, str):
            raise ProtocolError("invocation message has no 'target'")
        arguments = data.get("arguments", [])
        if arguments is None:
            arguments = []
        if not isinstance(arguments, list):
            raise ProtocolError("invocation 'arguments' is not an array")
        return InvocationMessage(
            target=target,
            arguments=tuple(arguments),
            invocation_id=_optional_str(data.get("invocationId"), "invocationId"),
        )
    if kind == MessageType.COMPLETION:
        invocation_id = _optional_str(data.get("invocationId"), "invocationId")
        if invocation_id is None:
            raise ProtocolError("completion message has no 'invocationId'")
        return CompletionMessage(
            invocation_id=invocation_id,
            result=data.get("result"),
            error=_optional_str(data.get("error"), "error"),
        )
    if kind == MessageType.PING:
        return PingMessage()
    if kind == MessageType.CLOSE:
        allow = data.get("allowReconnect", False)
        return CloseMessage(
            error=_optional_str(data.get("error"), "error"),
            allow_reconnect=allow is True,
        )
    return OtherMessage(type=kind, raw=data)


def parse_handshake_response(text: str) -> None:
    """Check a handshake response; ``{}`` (any object without ``error``) is success.

    Raises:
        HandshakeError: If the server reported an error or the response is malformed.
    """
    try:
        data = loads(text)
    except ValueError:
        raise HandshakeError("handshake response is not valid JSON") from None
    if not isinstance(data, dict):
        raise HandshakeError("handshake response is not a JSON object")
    error = data.get("error")
    if error is not None:
        raise HandshakeError(f"server rejected the handshake: {error}")
