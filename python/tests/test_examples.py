"""The examples compile, and the trading example refuses to run by default."""

from __future__ import annotations

import importlib
import py_compile
import sys
from collections.abc import Iterator
from pathlib import Path
from types import ModuleType
from typing import Any

import pytest

EXAMPLES = Path(__file__).resolve().parent.parent / "examples"


@pytest.mark.parametrize("path", sorted(EXAMPLES.glob("*.py")), ids=lambda p: p.name)
def test_example_compiles(path: Path) -> None:
    py_compile.compile(str(path), doraise=True)


@pytest.fixture
def place_order(monkeypatch: pytest.MonkeyPatch) -> Iterator[ModuleType]:
    monkeypatch.syspath_prepend(str(EXAMPLES))
    for name in ("TOPSTEPX_ALLOW_TRADING", "TOPSTEPX_USERNAME", "TOPSTEPX_API_KEY"):
        monkeypatch.delenv(name, raising=False)
    module = importlib.import_module("place_order")

    class NoClient:
        def __init__(self, *args: Any, **kwargs: Any) -> None:
            raise AssertionError("the example must not create a client")

        from_env = __init__

    monkeypatch.setattr(module, "Client", NoClient)
    yield module
    sys.modules.pop("place_order", None)


@pytest.mark.parametrize("value", [None, "0", "true", "yes"])
def test_order_example_refuses_without_explicit_opt_in(
    place_order: ModuleType, monkeypatch: pytest.MonkeyPatch, value: str | None
) -> None:
    if value is not None:
        monkeypatch.setenv("TOPSTEPX_ALLOW_TRADING", value)
    with pytest.raises(SystemExit) as info:
        place_order.main(["CON.F.US.EP.U25", "5000"])
    assert "TOPSTEPX_ALLOW_TRADING=1" in str(info.value.code)
