"""JSON, decimal, timestamp and enum (de)serialization shared by every model.

Internal module. Models are frozen, slotted dataclasses declared with the
:func:`model` decorator; each field carries its JSON name and a :class:`Codec`
in its metadata (see :func:`jfield`), and :class:`Model` implements
``from_dict``/``to_dict`` from that metadata.

Numbers: JSON is parsed with ``parse_float=Decimal`` so ``6000.25`` decodes to
exactly ``Decimal("6000.25")``; :func:`dumps` writes decimals as plain JSON
number literals with every digit preserved (never as quoted strings and never
through ``float``).
"""

from __future__ import annotations

import dataclasses
import json
import math
import re
from collections.abc import Callable, Mapping
from datetime import date, datetime, timedelta, timezone
from decimal import Decimal, InvalidOperation
from typing import TYPE_CHECKING, Any, ClassVar, Generic, TypeVar, cast

from .enums import SpecEnum
from .errors import DecodeError

if TYPE_CHECKING:
    from typing_extensions import Self, dataclass_transform
else:  # pragma: no cover - typing helper only

    def dataclass_transform(**_kwargs: Any) -> Callable[[Any], Any]:
        return lambda obj: obj


T = TypeVar("T")
E = TypeVar("E", bound=SpecEnum)
M = TypeVar("M", bound="Model")

_META_KEY = "topstepx"
_ABSENT: Any = object()

# ---------------------------------------------------------------------------
# JSON text
# ---------------------------------------------------------------------------


def _reject_constant(name: str) -> Any:
    raise ValueError(f"invalid JSON constant {name}")


def loads(text: str | bytes) -> Any:
    """Parse JSON text, decoding every non-integer number as an exact ``Decimal``.

    ``NaN`` and ``Infinity`` (which the standard library accepts) are rejected.
    """
    return json.loads(text, parse_float=Decimal, parse_constant=_reject_constant)


def dumps(value: Any) -> str:
    """Serialize JSON-compatible Python values to compact JSON text.

    Supports ``None``, ``bool``, ``int`` (including ``IntEnum``), ``Decimal``,
    ``float``, ``str``, mappings with string keys, lists and tuples. Decimals are
    written as number literals in fixed-point notation with all their digits.
    """
    out: list[str] = []
    _dump(value, out)
    return "".join(out)


def _dump(value: Any, out: list[str]) -> None:
    if value is None:
        out.append("null")
    elif value is True:
        out.append("true")
    elif value is False:
        out.append("false")
    elif isinstance(value, str):
        out.append(json.dumps(value, ensure_ascii=False))
    elif isinstance(value, int):
        out.append(int.__repr__(value))
    elif isinstance(value, Decimal):
        out.append(format_decimal(value))
    elif isinstance(value, float):
        if not math.isfinite(value):
            raise ValueError(f"cannot encode non-finite number {value!r} as JSON")
        out.append(float.__repr__(value))
    elif isinstance(value, Mapping):
        out.append("{")
        first = True
        for key, item in value.items():
            if not isinstance(key, str):
                raise TypeError(f"JSON object keys must be strings, not {type(key).__name__}")
            if not first:
                out.append(",")
            first = False
            out.append(json.dumps(key, ensure_ascii=False))
            out.append(":")
            _dump(item, out)
        out.append("}")
    elif isinstance(value, (list, tuple)):
        out.append("[")
        for index, item in enumerate(value):
            if index:
                out.append(",")
            _dump(item, out)
        out.append("]")
    else:
        raise TypeError(f"object of type {type(value).__name__} is not JSON serializable")


# ---------------------------------------------------------------------------
# Decimals
# ---------------------------------------------------------------------------


def format_decimal(value: Decimal) -> str:
    """Format a finite decimal as a JSON number literal without an exponent."""
    if not value.is_finite():
        raise ValueError(f"cannot encode non-finite decimal {value!r} as JSON")
    return format(value, "f")


def to_decimal(value: Decimal | int | float | str) -> Decimal:
    """Convert a user-supplied price or amount to an exact, finite ``Decimal``.

    Floats are converted through their shortest ``repr`` so ``6000.1`` becomes
    ``Decimal("6000.1")`` rather than its binary expansion.
    """
    if isinstance(value, bool):
        raise TypeError("a boolean is not a valid decimal value")
    if isinstance(value, Decimal):
        result = value
    elif isinstance(value, int):
        result = Decimal(value)
    elif isinstance(value, float):
        result = Decimal(repr(value)) if math.isfinite(value) else Decimal("NaN")
    elif isinstance(value, str):
        try:
            result = Decimal(value.strip())
        except InvalidOperation:
            raise ValueError(f"not a decimal number: {value!r}") from None
    else:
        raise TypeError(f"cannot convert {type(value).__name__} to Decimal")
    if not result.is_finite():
        raise ValueError(f"decimal value must be finite, got {value!r}")
    return result


# ---------------------------------------------------------------------------
# Timestamps
# ---------------------------------------------------------------------------

_TIMESTAMP_RE = re.compile(
    r"""
    (?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})
    (?:
        [Tt\ ]
        (?P<hour>\d{2}):(?P<minute>\d{2})
        (?::(?P<second>\d{2})(?:[.,](?P<fraction>\d+))?)?
        (?P<offset>[Zz]|[+-]\d{2}(?::?\d{2})?)?
    )?
    """,
    re.VERBOSE,
)


def parse_timestamp(text: str) -> datetime:
    """Parse an RFC 3339 timestamp into an aware ``datetime`` in UTC.

    Accepts a ``Z`` suffix or any numeric offset (``+05:30``, ``+0530``, ``+05``),
    any number of fractional-second digits (.NET emits up to seven; digits beyond
    microseconds are truncated), a missing offset (read as UTC), a space instead
    of ``T``, and a bare date (midnight UTC). A leap second (``:60``) is read as
    the first instant of the next minute.

    Raises:
        ValueError: If ``text`` is not a timestamp.
    """
    match = _TIMESTAMP_RE.fullmatch(text.strip())
    if match is None:
        raise ValueError(f"invalid RFC 3339 timestamp: {text!r}")
    fraction = match["fraction"] or ""
    microsecond = int((fraction + "000000")[:6])
    second = int(match["second"] or 0)
    leap = second == 60
    if leap:
        second = 59
    offset_text = match["offset"]
    tzinfo = timezone.utc
    if offset_text and offset_text not in ("Z", "z"):
        sign = -1 if offset_text[0] == "-" else 1
        digits = offset_text[1:].replace(":", "")
        hours, minutes = int(digits[:2]), int(digits[2:4] or 0)
        if hours > 23 or minutes > 59:
            raise ValueError(f"invalid UTC offset in timestamp: {text!r}")
        tzinfo = timezone(sign * timedelta(hours=hours, minutes=minutes))
    try:
        result = datetime(
            int(match["year"]),
            int(match["month"]),
            int(match["day"]),
            int(match["hour"] or 0),
            int(match["minute"] or 0),
            second,
            microsecond,
            tzinfo=tzinfo,
        )
        if leap:
            result += timedelta(seconds=1)
        return result.astimezone(timezone.utc)
    except (ValueError, OverflowError) as exc:
        raise ValueError(f"invalid RFC 3339 timestamp: {text!r} ({exc})") from None


def as_utc(value: datetime | date) -> datetime:
    """Return ``value`` as an aware UTC datetime.

    Naive datetimes are taken to be UTC (the same rule the decoder applies to a
    timestamp without an offset); a ``date`` means midnight UTC.
    """
    if not isinstance(value, datetime):
        return datetime(value.year, value.month, value.day, tzinfo=timezone.utc)
    if value.tzinfo is None or value.utcoffset() is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def format_timestamp(value: datetime | date) -> str:
    """Format a timestamp as RFC 3339 UTC: ``YYYY-MM-DDTHH:MM:SS[.ffffff]Z``."""
    value = as_utc(value)
    text = (
        f"{value.year:04d}-{value.month:02d}-{value.day:02d}"
        f"T{value.hour:02d}:{value.minute:02d}:{value.second:02d}"
    )
    if value.microsecond:
        text += f".{value.microsecond:06d}"
    return text + "Z"


# ---------------------------------------------------------------------------
# Codecs
# ---------------------------------------------------------------------------


def _kind(raw: object) -> str:
    if raw is None:
        return "null"
    if isinstance(raw, bool):
        return "boolean"
    if isinstance(raw, int):
        return "integer"
    if isinstance(raw, (Decimal, float)):
        return "number"
    if isinstance(raw, str):
        return "string"
    if isinstance(raw, Mapping):
        return "object"
    if isinstance(raw, (list, tuple)):
        return "array"
    return type(raw).__name__


class Codec(Generic[T]):
    """Converts one JSON value to a Python value and back."""

    __slots__ = ()

    def decode(self, raw: Any, path: str) -> T:
        """Decode the JSON value ``raw`` found at ``path``.

        Raises:
            DecodeError: If ``raw`` has the wrong shape.
        """
        raise NotImplementedError

    def encode(self, value: T) -> Any:
        """Encode ``value`` to a JSON-compatible value for :func:`dumps`."""
        raise NotImplementedError


class _BoolCodec(Codec[bool]):
    __slots__ = ()

    def decode(self, raw: Any, path: str) -> bool:
        if isinstance(raw, bool):
            return raw
        raise DecodeError(f"expected a boolean, got {_kind(raw)}", path=path)

    def encode(self, value: bool) -> Any:
        return bool(value)


class _StrCodec(Codec[str]):
    __slots__ = ()

    def decode(self, raw: Any, path: str) -> str:
        if isinstance(raw, str):
            return raw
        raise DecodeError(f"expected a string, got {_kind(raw)}", path=path)

    def encode(self, value: str) -> Any:
        if not isinstance(value, str):
            raise TypeError(f"expected str, got {type(value).__name__}")
        return value


def _integral(raw: Any, *, allow_strings: bool) -> int | None:
    if isinstance(raw, bool):
        return None
    if isinstance(raw, int):
        return int(raw)
    if isinstance(raw, str) and allow_strings:
        try:
            raw = Decimal(raw.strip())
        except InvalidOperation:
            return None
    if isinstance(raw, float) and raw.is_integer():
        return int(raw)
    if isinstance(raw, Decimal) and raw.is_finite() and raw == raw.to_integral_value():
        return int(raw)
    return None


class _IntCodec(Codec[int]):
    __slots__ = ("_allow_strings",)

    def __init__(self, *, allow_strings: bool) -> None:
        self._allow_strings = allow_strings

    def decode(self, raw: Any, path: str) -> int:
        result = _integral(raw, allow_strings=self._allow_strings)
        if result is None:
            raise DecodeError(f"expected an integer, got {_kind(raw)} {raw!r}", path=path)
        return result

    def encode(self, value: int) -> Any:
        if isinstance(value, bool) or not isinstance(value, int):
            raise TypeError(f"expected int, got {type(value).__name__}")
        return int(value)


class _DecimalCodec(Codec[Decimal]):
    __slots__ = ()

    def decode(self, raw: Any, path: str) -> Decimal:
        if isinstance(raw, (Decimal, int, float, str)) and not isinstance(raw, bool):
            try:
                return to_decimal(raw)
            except (TypeError, ValueError):
                pass
        raise DecodeError(f"expected a number, got {_kind(raw)} {raw!r}", path=path)

    def encode(self, value: Decimal) -> Any:
        return to_decimal(value)


class _TimestampCodec(Codec[datetime]):
    __slots__ = ()

    def decode(self, raw: Any, path: str) -> datetime:
        if isinstance(raw, str):
            try:
                return parse_timestamp(raw)
            except ValueError as exc:
                raise DecodeError(str(exc), path=path) from None
        if isinstance(raw, datetime):
            return as_utc(raw)
        raise DecodeError(f"expected an RFC 3339 timestamp string, got {_kind(raw)}", path=path)

    def encode(self, value: datetime) -> Any:
        raw: object = value
        if isinstance(raw, str):
            return format_timestamp(parse_timestamp(raw))
        if not isinstance(raw, date):
            raise TypeError(f"expected datetime, got {type(raw).__name__}")
        return format_timestamp(raw)


class _AnyCodec(Codec[Any]):
    __slots__ = ()

    def decode(self, raw: Any, path: str) -> Any:
        return raw

    def encode(self, value: Any) -> Any:
        return value


class EnumCodec(Codec[E]):
    """Codec for a :class:`~topstepx.enums.SpecEnum`.

    Unknown values decode to pseudo-members instead of failing.
    """

    __slots__ = ("_enum",)

    def __init__(self, enum_type: type[E]) -> None:
        self._enum = enum_type

    def decode(self, raw: Any, path: str) -> E:
        number = _integral(raw, allow_strings=True)
        if number is not None:
            return self._enum(number)
        if isinstance(raw, str) and raw in self._enum.__members__:
            return self._enum[raw]
        raise DecodeError(
            f"expected an integer {self._enum.__name__} value, got {_kind(raw)} {raw!r}",
            path=path,
        )

    def encode(self, value: E) -> Any:
        raw: object = value
        if isinstance(raw, bool) or not isinstance(raw, int):
            raise TypeError(f"expected {self._enum.__name__} or int, got {type(raw).__name__}")
        return int(raw)


class ModelCodec(Codec[M]):
    """Codec for a nested :class:`Model`."""

    __slots__ = ("_model",)

    def __init__(self, model_type: type[M]) -> None:
        self._model = model_type

    def decode(self, raw: Any, path: str) -> M:
        if not isinstance(raw, Mapping):
            raise DecodeError(f"expected an object, got {_kind(raw)}", path=path)
        return self._model._decode(raw, path)

    def encode(self, value: M) -> Any:
        if not isinstance(value, self._model):
            raise TypeError(f"expected {self._model.__name__}, got {type(value).__name__}")
        return value.to_dict()


class ListCodec(Codec[tuple[T, ...]]):
    """Codec for a JSON array; decodes to a tuple."""

    __slots__ = ("_item",)

    def __init__(self, item: Codec[T]) -> None:
        self._item = item

    def decode(self, raw: Any, path: str) -> tuple[T, ...]:
        if not isinstance(raw, (list, tuple)):
            raise DecodeError(f"expected an array, got {_kind(raw)}", path=path)
        return tuple(self._item.decode(item, f"{path}[{i}]") for i, item in enumerate(raw))

    def encode(self, value: tuple[T, ...]) -> Any:
        if isinstance(value, (str, bytes, Mapping)):
            raise TypeError(f"expected a sequence, got {type(value).__name__}")
        return [self._item.encode(item) for item in value]


BOOL: Codec[bool] = _BoolCodec()
"""Strict JSON boolean."""
STR: Codec[str] = _StrCodec()
"""JSON string."""
INT: Codec[int] = _IntCodec(allow_strings=False)
"""Integer; an integral number such as ``3.0`` is accepted."""
LENIENT_INT: Codec[int] = _IntCodec(allow_strings=True)
"""Integer that also accepts numeric strings such as ``"3"`` (realtime volumes)."""
DECIMAL: Codec[Decimal] = _DecimalCodec()
"""Exact decimal; numeric strings are accepted when decoding."""
TIMESTAMP: Codec[datetime] = _TimestampCodec()
"""RFC 3339 timestamp, decoded to an aware UTC datetime."""
ANY: Codec[Any] = _AnyCodec()
"""Raw JSON value, passed through unchanged."""


def enum(enum_type: type[E]) -> EnumCodec[E]:
    """Return a codec for ``enum_type``."""
    return EnumCodec(enum_type)


def nested(model_type: type[M]) -> ModelCodec[M]:
    """Return a codec for the nested model ``model_type``."""
    return ModelCodec(model_type)


def array(item: Codec[T]) -> ListCodec[T]:
    """Return a codec for a JSON array whose items use ``item``."""
    return ListCodec(item)


# ---------------------------------------------------------------------------
# Field metadata and models
# ---------------------------------------------------------------------------


@dataclasses.dataclass(frozen=True, slots=True)
class FieldSpec:
    """Wire description of one model field."""

    attr: str
    json_name: str
    codec: Codec[Any]
    required: bool
    secret: bool
    extras: bool


@dataclasses.dataclass(frozen=True, slots=True)
class _Meta:
    json_name: str
    codec: Codec[Any]
    secret: bool
    extras: bool


def jfield(
    json_name: str,
    codec: Codec[Any],
    *,
    default: Any = dataclasses.MISSING,
    default_factory: Any = dataclasses.MISSING,
    secret: bool = False,
    extras: bool = False,
) -> Any:
    """Declare a model field: its JSON name, codec and (optional) default.

    Args:
        json_name: The exact camelCase name used on the wire.
        codec: How the value is decoded and encoded.
        default: Default value; a field without a default is required.
        default_factory: Factory for a default value.
        secret: Redact the value in ``repr`` (API keys, passwords, tokens).
        extras: This mapping field collects JSON members the model does not
            declare (used by ``ProblemDetails``).
    """
    meta = {_META_KEY: _Meta(json_name, codec, secret, extras)}
    return dataclasses.field(
        default=default,
        default_factory=default_factory,
        metadata=meta,
        hash=False if extras else None,
    )


@dataclass_transform(kw_only_default=True, frozen_default=True, field_specifiers=(jfield,))
def model(cls: type[M]) -> type[M]:
    """Turn ``cls`` into a frozen, slotted, keyword-only dataclass with wire metadata."""
    new_cls = cast(
        "type[M]",
        dataclasses.dataclass(frozen=True, slots=True, kw_only=True, repr=False)(cls),
    )
    specs: list[FieldSpec] = []
    for field in dataclasses.fields(new_cls):  # type: ignore[arg-type]
        meta = field.metadata.get(_META_KEY)
        if not isinstance(meta, _Meta):
            raise TypeError(f"{cls.__name__}.{field.name} must be declared with jfield()")
        required = (
            field.default is dataclasses.MISSING and field.default_factory is dataclasses.MISSING
        )
        specs.append(
            FieldSpec(field.name, meta.json_name, meta.codec, required, meta.secret, meta.extras)
        )
    new_cls.__wire_fields__ = tuple(specs)
    new_cls.__wire_names__ = frozenset(s.json_name for s in specs if not s.extras)
    return new_cls


class Model:
    """Base class of every JSON model.

    REST requests, responses and objects, and realtime payloads are models.

    Models are immutable. Use :meth:`from_dict`/:meth:`to_dict` (or the JSON-text
    variants) to convert them. Unknown JSON members are ignored when decoding;
    ``None`` fields are omitted when encoding.
    """

    __slots__ = ()
    __schema__: ClassVar[str] = ""
    __wire_fields__: ClassVar[tuple[FieldSpec, ...]] = ()
    __wire_names__: ClassVar[frozenset[str]] = frozenset()

    @classmethod
    def from_dict(cls, data: Mapping[str, Any]) -> Self:
        """Decode an instance from a parsed JSON object.

        Raises:
            DecodeError: If a required field is missing or a value has the wrong type.
        """
        if not isinstance(data, Mapping):
            raise DecodeError(f"expected an object, got {_kind(data)}")
        return cls._decode(data, "")

    @classmethod
    def from_json(cls, text: str | bytes) -> Self:
        """Decode an instance from JSON text (numbers decode exactly).

        Raises:
            DecodeError: If the text is not valid JSON or does not match the model.
        """
        try:
            data = loads(text)
        except ValueError as exc:
            raise DecodeError(f"invalid JSON: {exc}") from None
        return cls.from_dict(data)

    @classmethod
    def _decode(cls, data: Mapping[str, Any], path: str) -> Self:
        kwargs: dict[str, Any] = {}
        extras_attr: str | None = None
        folded: dict[str, str] | None = None
        for spec in cls.__wire_fields__:
            if spec.extras:
                extras_attr = spec.attr
                continue
            raw = data.get(spec.json_name, _ABSENT)
            if raw is _ABSENT:
                # Lenient fallback: match member names case-insensitively.
                if folded is None:
                    folded = {str(k).lower(): k for k in data}
                key = folded.get(spec.json_name.lower())
                if key is not None:
                    raw = data[key]
            field_path = f"{path}.{spec.json_name}" if path else spec.json_name
            if raw is _ABSENT or raw is None:
                if spec.required:
                    what = "is missing" if raw is _ABSENT else "must not be null"
                    raise DecodeError(f"required field {what}", path=field_path)
                continue
            kwargs[spec.attr] = spec.codec.decode(raw, field_path)
        if extras_attr is not None:
            names = cls.__wire_names__
            kwargs[extras_attr] = {k: v for k, v in data.items() if k not in names}
        return cls(**kwargs)

    def to_dict(self) -> dict[str, Any]:
        """Encode to a JSON-compatible dict using the exact wire names.

        ``None`` values are omitted. Decimal values stay ``Decimal``; serialize the
        result with :meth:`to_json` (or any encoder that writes decimals as numbers).
        """
        out: dict[str, Any] = {}
        extras: Mapping[str, Any] | None = None
        for spec in self.__wire_fields__:
            value = getattr(self, spec.attr)
            if spec.extras:
                extras = value
                continue
            if value is None:
                continue
            out[spec.json_name] = spec.codec.encode(value)
        if extras:
            for key, value in extras.items():
                out.setdefault(key, value)
        return out

    def to_json(self) -> str:
        """Encode to compact JSON text with exact decimal digits."""
        return dumps(self.to_dict())

    def __repr__(self) -> str:
        parts = []
        for spec in self.__wire_fields__:
            value = getattr(self, spec.attr)
            shown = "<redacted>" if spec.secret and value is not None else repr(value)
            parts.append(f"{spec.attr}={shown}")
        return f"{type(self).__name__}({', '.join(parts)})"
