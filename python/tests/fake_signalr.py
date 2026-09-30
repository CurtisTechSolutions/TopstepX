"""A minimal SignalR JSON-protocol hub server for tests, built on ``websockets``.

It performs the handshake, records invocations, answers them with completions,
counts pings, pushes events, and can drop or close connections on demand.
"""

from __future__ import annotations

import asyncio
import json
from collections.abc import Callable
from dataclasses import dataclass, field
from http import HTTPStatus
from typing import Any
from urllib.parse import parse_qs, urlsplit

from websockets.asyncio.server import Server, ServerConnection, serve
from websockets.datastructures import Headers
from websockets.exceptions import ConnectionClosed
from websockets.http11 import Request, Response

RS = "\x1e"

# A completion rule returns (result, error) or None to leave the call hanging.
Rule = Callable[[list[Any]], tuple[Any, str | None] | None]


@dataclass
class Conn:
    ws: ServerConnection
    path: str
    query: dict[str, list[str]]
    headers: Headers
    handshake: dict[str, Any] | None = None
    invocations: list[dict[str, Any]] = field(default_factory=list)
    pings: int = 0
    client_close: bool = False
    closed: asyncio.Event = field(default_factory=asyncio.Event)

    @property
    def token(self) -> str | None:
        values = self.query.get("access_token")
        return values[0] if values else None

    def targets(self) -> list[tuple[str, list[Any]]]:
        return [(m["target"], m.get("arguments", [])) for m in self.invocations]


class FakeSignalR:
    def __init__(self, hub_path: str = "/hubs/user") -> None:
        self.hub_path = hub_path
        self.conns: list[Conn] = []
        self.rules: dict[str, Rule] = {}
        self.handshake_reply: str | None = "{}"
        self.handshake_extra = ""
        self.reject_status: int | None = None  # reject every upgrade
        self.reject_queue: list[int] = []  # reject the next upgrades, one status each
        self.rejected_tokens: list[str | None] = []
        self.ping_interval: float | None = 0.05
        self.port = 0
        self._server: Server | None = None

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.port}{self.hub_path}"

    async def __aenter__(self) -> FakeSignalR:
        self._server = await serve(
            self._handle,
            "127.0.0.1",
            0,
            ping_interval=None,
            process_request=self._process_request,
        )
        self.port = self._server.sockets[0].getsockname()[1]
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.stop()

    async def stop(self) -> None:
        if self._server is not None:
            self._server.close()
            await self._server.wait_closed()
            self._server = None

    def _process_request(self, connection: ServerConnection, request: Request) -> Response | None:
        status = self.reject_status
        if status is None and self.reject_queue:
            status = self.reject_queue.pop(0)
        if status is None:
            return None
        tokens = parse_qs(urlsplit(request.path).query).get("access_token")
        self.rejected_tokens.append(tokens[0] if tokens else None)
        return connection.respond(HTTPStatus(status), "rejected\n")

    async def _handle(self, ws: ServerConnection) -> None:
        assert ws.request is not None
        parts = urlsplit(ws.request.path)
        conn = Conn(ws=ws, path=parts.path, query=parse_qs(parts.query), headers=ws.request.headers)
        self.conns.append(conn)
        pinger: asyncio.Task[None] | None = None
        try:
            first = await ws.recv()
            text = first if isinstance(first, str) else first.decode()
            head, _, buffer = text.partition(RS)
            conn.handshake = json.loads(head)
            if self.handshake_reply is None:
                await ws.wait_closed()
                return
            await ws.send(self.handshake_reply + RS + self.handshake_extra)
            if self.ping_interval:
                pinger = asyncio.create_task(self._ping(ws, self.ping_interval))
            async for frame in ws:
                buffer += frame if isinstance(frame, str) else frame.decode()
                *messages, buffer = buffer.split(RS)
                for message in messages:
                    if message:
                        await self._on_message(conn, json.loads(message))
        except ConnectionClosed:
            pass
        finally:
            if pinger is not None:
                pinger.cancel()
            conn.closed.set()

    async def _ping(self, ws: ServerConnection, interval: float) -> None:
        try:
            while True:
                await asyncio.sleep(interval)
                await ws.send(json.dumps({"type": 6}) + RS)
        except ConnectionClosed:
            pass

    async def _on_message(self, conn: Conn, message: dict[str, Any]) -> None:
        kind = message.get("type")
        if kind == 1:
            conn.invocations.append(message)
            invocation_id = message.get("invocationId")
            if invocation_id is None:
                return
            rule = self.rules.get(message["target"], lambda args: (None, None))
            outcome = rule(message.get("arguments", []))
            if outcome is None:
                return
            result, error = outcome
            completion: dict[str, Any] = {"type": 3, "invocationId": invocation_id}
            if error is not None:
                completion["error"] = error
            elif result is not None:
                completion["result"] = result
            await conn.ws.send(json.dumps(completion) + RS)
        elif kind == 6:
            conn.pings += 1
        elif kind == 7:
            conn.client_close = True

    # -- test controls -----------------------------------------------------------

    async def push(self, target: str, *arguments: Any, conn: int = -1) -> None:
        payload = json.dumps({"type": 1, "target": target, "arguments": list(arguments)})
        await self.conns[conn].ws.send(payload + RS)

    async def send_raw(self, data: str | bytes, conn: int = -1) -> None:
        await self.conns[conn].ws.send(data)

    def drop(self, conn: int = -1) -> None:
        """Abort the TCP connection without a close frame (a network failure)."""
        self.conns[conn].ws.transport.abort()

    async def close_from_server(
        self, error: str | None = None, allow_reconnect: bool = False, conn: int = -1
    ) -> None:
        message: dict[str, Any] = {"type": 7, "allowReconnect": allow_reconnect}
        if error is not None:
            message["error"] = error
        ws = self.conns[conn].ws
        await ws.send(json.dumps(message) + RS)
        await ws.close()


async def wait_until(predicate: Callable[[], bool], timeout: float = 3.0) -> None:
    loop = asyncio.get_running_loop()
    deadline = loop.time() + timeout
    while not predicate():
        if loop.time() > deadline:
            raise AssertionError("condition not met in time")
        await asyncio.sleep(0.005)
