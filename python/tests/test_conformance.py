"""Conformance with the OpenAPI specification (SDK_BEHAVIOR.md section 12.1).

The specification is read from ``spec/projectx-openapi.json`` at the repository
root, relative to this file.
"""

from __future__ import annotations

import dataclasses
import json
import re
from decimal import Decimal
from pathlib import Path
from typing import Any

import pytest

from topstepx._base import parse_ping
from topstepx._serde import Model, loads, parse_timestamp
from topstepx._validation import validate_request
from topstepx.enums import SCHEMA_ENUMS
from topstepx.models import SCHEMA_MODELS, RequestModel
from topstepx.operations import OPERATIONS, AuthMode, RateBucket, RetryClass

SPEC_PATH = Path(__file__).resolve().parent.parent.parent / "spec" / "projectx-openapi.json"
SPEC: dict[str, Any] = json.loads(SPEC_PATH.read_text(encoding="utf-8"), parse_float=Decimal)
SCHEMAS: dict[str, Any] = SPEC["components"]["schemas"]

# Retry classes from the table in SDK_BEHAVIOR.md section 3.
WRITE_OPERATIONS = {
    "Auth_LoginApp",
    "Auth_LoginKey",
    "Auth_Logout",
    "Auth_Validate",
    "Order_PlaceOrder",
    "Order_CancelOrder",
    "Order_ModifyOrder",
    "Position_CloseContractPosition",
    "Position_PartialCloseContractPosition",
}
# Deliberate leniencies (SDK_BEHAVIOR.md sections 4 and 10).
LENIENT_OPTIONAL = {("OrderModel", "updateTimestamp")}


def _ref_name(schema: dict[str, Any]) -> str:
    return str(schema["$ref"]).rsplit("/", 1)[-1]


def _spec_operations() -> dict[str, tuple[str, str, dict[str, Any]]]:
    result = {}
    for path, item in SPEC["paths"].items():
        for method, operation in item.items():
            result[operation["operationId"]] = (method.upper(), path, operation)
    return result


SPEC_OPERATIONS = _spec_operations()
SDK_OPERATIONS = {op.operation_id: op for op in OPERATIONS}


# ---------------------------------------------------------------------------
# Equivalence: null == absent, numbers by value, timestamps by instant
# ---------------------------------------------------------------------------

_TIMESTAMP_SHAPE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}")


def assert_equivalent(expected: Any, actual: Any, path: str = "$") -> None:
    if isinstance(expected, dict) and isinstance(actual, dict):
        for key in sorted(set(expected) | set(actual)):
            assert_equivalent(expected.get(key), actual.get(key), f"{path}.{key}")
        return
    if isinstance(expected, list) and isinstance(actual, list):
        assert len(expected) == len(actual), f"{path}: length {len(expected)} != {len(actual)}"
        for index, (left, right) in enumerate(zip(expected, actual, strict=True)):
            assert_equivalent(left, right, f"{path}[{index}]")
        return
    if expected is None or actual is None:
        assert expected is None, f"{path}: {expected!r} != {actual!r}"
        assert actual is None, f"{path}: {expected!r} != {actual!r}"
        return
    if isinstance(expected, bool) or isinstance(actual, bool):
        assert type(expected) is type(actual), f"{path}: {expected!r} vs {actual!r}"
        assert expected == actual, f"{path}: {expected!r} != {actual!r}"
        return
    if isinstance(expected, (int, Decimal, float)) and isinstance(actual, (int, Decimal, float)):
        assert Decimal(str(expected)) == Decimal(str(actual)), f"{path}: {expected!r} != {actual!r}"
        return
    if isinstance(expected, str) and isinstance(actual, str):
        if _TIMESTAMP_SHAPE.match(expected) and _TIMESTAMP_SHAPE.match(actual):
            assert parse_timestamp(expected) == parse_timestamp(actual), f"{path}: instants differ"
        else:
            assert expected == actual, f"{path}: {expected!r} != {actual!r}"
        return
    pytest.fail(f"{path}: {expected!r} ({type(expected).__name__}) vs {actual!r}")


def round_trip(model_type: type[Model], data: dict[str, Any]) -> dict[str, Any]:
    """Decode ``data``, re-encode it to JSON text, and parse that text back."""
    instance = model_type.from_dict(data)
    return dict(loads(instance.to_json()))


# ---------------------------------------------------------------------------
# Operations
# ---------------------------------------------------------------------------


def test_all_21_operations_are_covered() -> None:
    assert len(SPEC_OPERATIONS) == 21
    assert set(SDK_OPERATIONS) == set(SPEC_OPERATIONS)
    assert len(OPERATIONS) == 21


@pytest.mark.parametrize("operation_id", sorted(SPEC_OPERATIONS))
def test_operation_matches_the_spec(operation_id: str) -> None:
    method, path, spec_op = SPEC_OPERATIONS[operation_id]
    op = SDK_OPERATIONS[operation_id]
    assert (op.method, op.path) == (method, path)

    if spec_op.get("security") == []:
        assert op.auth is AuthMode.NONE
    elif operation_id == "Status_Ping":
        assert op.auth is AuthMode.OPTIONAL  # bearer if available (section 6)
    else:
        assert op.auth is AuthMode.BEARER

    expected_bucket = (
        RateBucket.HISTORY if operation_id == "History_GetBars" else RateBucket.GENERAL
    )
    assert op.bucket is expected_bucket
    expected_retry = RetryClass.WRITE if operation_id in WRITE_OPERATIONS else RetryClass.READ
    assert op.retry is expected_retry

    request_body = spec_op.get("requestBody")
    if request_body is None:
        assert op.request_type is None
    else:
        schema_name = _ref_name(request_body["content"]["application/json"]["schema"])
        assert op.request_type is not None
        assert op.request_type.__schema__ == schema_name  # type: ignore[attr-defined]
        assert op.request_type.__operation_id__ == operation_id  # type: ignore[attr-defined]

    response_schema = spec_op["responses"]["200"]["content"]["application/json"]["schema"]
    if "$ref" in response_schema:
        assert op.response_type.__schema__ == _ref_name(response_schema)  # type: ignore[attr-defined]
    else:
        assert response_schema == {"type": "string"}
        assert op.response_type is str


# ---------------------------------------------------------------------------
# Examples
# ---------------------------------------------------------------------------


def _request_examples() -> list[tuple[str, str, Any]]:
    cases = []
    for operation_id, (_, _, spec_op) in sorted(SPEC_OPERATIONS.items()):
        content = spec_op.get("requestBody", {}).get("content", {}).get("application/json", {})
        for name, example in content.get("examples", {}).items():
            cases.append((operation_id, name, example["value"]))
    return cases


def _response_examples() -> list[tuple[str, str, Any]]:
    cases = []
    for operation_id, (_, _, spec_op) in sorted(SPEC_OPERATIONS.items()):
        content = spec_op["responses"]["200"]["content"]["application/json"]
        for name, example in content.get("examples", {}).items():
            cases.append((operation_id, name, example["value"]))
    return cases


REQUEST_EXAMPLES = _request_examples()
RESPONSE_EXAMPLES = _response_examples()


def test_example_inventory() -> None:
    # validation-report.json: 44 request/response examples in total
    assert len(REQUEST_EXAMPLES) == 20
    assert len(RESPONSE_EXAMPLES) == 24


@pytest.mark.parametrize(
    ("operation_id", "name", "example"),
    REQUEST_EXAMPLES,
    ids=[f"{op}-{name}" for op, name, _ in REQUEST_EXAMPLES],
)
def test_request_example_round_trips(operation_id: str, name: str, example: Any) -> None:
    op = SDK_OPERATIONS[operation_id]
    assert op.request_type is not None
    request = op.request_type.from_dict(example)  # type: ignore[attr-defined]
    validate_request(request)  # every example is a valid request
    assert_equivalent(example, round_trip(op.request_type, example))  # type: ignore[arg-type]


@pytest.mark.parametrize(
    ("operation_id", "name", "example"),
    RESPONSE_EXAMPLES,
    ids=[f"{op}-{name}" for op, name, _ in RESPONSE_EXAMPLES],
)
def test_response_example_round_trips(operation_id: str, name: str, example: Any) -> None:
    op = SDK_OPERATIONS[operation_id]
    if op.response_type is str:
        decoded = parse_ping(json.dumps(example))
        assert_equivalent(example, loads(json.dumps(decoded)))
        return
    assert_equivalent(example, round_trip(op.response_type, example))  # type: ignore[arg-type]


# ---------------------------------------------------------------------------
# Synthesized full objects
# ---------------------------------------------------------------------------

LARGE_INT64 = 9_007_199_254_740_993  # 2**53 + 1: not representable as a float
EXACT_DECIMAL = Decimal("12345.678901234567890123")


def _resolve(schema: dict[str, Any]) -> dict[str, Any]:
    while "$ref" in schema:
        schema = SCHEMAS[_ref_name(schema)]
    return schema


def synthesize(schema: dict[str, Any], name: str = "value") -> Any:
    """A value holding every property, valid for section 9's rules."""
    if "$ref" in schema:
        return synthesize(_resolve(schema), name)
    if schema.get("type") == "object":
        # (an object-level allOf holds only the trailing-stop conditional, which
        # the synthesized object satisfies because it carries every property)
        value = {prop: synthesize(sub, prop) for prop, sub in schema.get("properties", {}).items()}
        if schema.get("additionalProperties") is not None:
            value["traceId"] = "00-extension-member"
            value["errors"] = {"field": ["message"]}
        return value
    if "allOf" in schema:
        for part in schema["allOf"]:
            if "enum" in part:  # a narrowed enum, such as placeable order types
                return part["enum"][-1]
        return synthesize(schema["allOf"][0], name)
    if "anyOf" in schema:
        options = [s for s in schema["anyOf"] if s.get("type") != "null"]
        return synthesize(options[0], name)
    if "enum" in schema:
        return schema["enum"][-1]
    kind = schema.get("type")
    if isinstance(kind, list):
        kind = next(k for k in kind if k != "null")
    if kind == "array":
        return [synthesize(schema["items"], name), synthesize(schema["items"], name)]
    if kind == "integer":
        value = LARGE_INT64 if schema.get("format") == "int64" else 7
        if "minimum" in schema:
            value = max(value, int(schema["minimum"]))
        if "maximum" in schema:
            value = min(value, int(schema["maximum"]))
        return value
    if kind == "number":
        return EXACT_DECIMAL
    if kind == "boolean":
        return True
    if kind == "string":
        if schema.get("format") == "date-time":
            return "2025-07-01T14:00:00.123456Z"
        if schema.get("format") == "date":
            return "2025-07-01"
        return f"sample-{name}"
    raise AssertionError(f"cannot synthesize {schema}")


OBJECT_SCHEMAS = sorted(name for name, schema in SCHEMAS.items() if schema.get("type") == "object")
ENUM_SCHEMAS = sorted(name for name, schema in SCHEMAS.items() if "enum" in schema)


def test_every_schema_is_modeled() -> None:
    assert len(SCHEMAS) == 69
    assert set(OBJECT_SCHEMAS) == set(SCHEMA_MODELS)
    assert set(ENUM_SCHEMAS) == set(SCHEMA_ENUMS)
    assert len(OBJECT_SCHEMAS) + len(ENUM_SCHEMAS) == 69


@pytest.mark.parametrize("schema_name", OBJECT_SCHEMAS)
def test_synthesized_full_object_round_trips(schema_name: str) -> None:
    schema = SCHEMAS[schema_name]
    data = synthesize(schema, schema_name)
    model_type = SCHEMA_MODELS[schema_name]
    instance = model_type.from_dict(data)
    if isinstance(instance, RequestModel):
        validate_request(instance)
    encoded = round_trip(model_type, data)
    assert set(encoded) == set(data), "missing or excess members after the round trip"
    assert_equivalent(data, encoded)


@pytest.mark.parametrize("schema_name", OBJECT_SCHEMAS)
def test_model_fields_match_schema_properties(schema_name: str) -> None:
    schema = SCHEMAS[schema_name]
    model_type = SCHEMA_MODELS[schema_name]
    properties = schema.get("properties", {})
    required = set(schema.get("required", []))
    wire_fields = {spec.json_name: spec for spec in model_type.__wire_fields__ if not spec.extras}
    assert set(wire_fields) == set(properties), "fields differ from the schema properties"
    defaults = {f.name: f.default for f in dataclasses.fields(model_type)}  # type: ignore[arg-type]

    for prop_name, prop in properties.items():
        spec = wire_fields[prop_name]
        prop_type = prop.get("type")
        nullable = (isinstance(prop_type, list) and "null" in prop_type) or any(
            option.get("type") == "null" for option in prop.get("anyOf", [])
        )
        spec_optional = prop_name not in required or nullable
        if spec_optional:
            # every nullable or not-required field is optional in the SDK
            assert not spec.required, f"{prop_name} must be optional"
        elif (schema_name, prop_name) in LENIENT_OPTIONAL:
            assert not spec.required
        elif not spec.required:
            # required on the wire: a default may exist, but it is never None
            assert defaults[spec.attr] is not None, f"{prop_name} is required by the spec"


# ---------------------------------------------------------------------------
# Enums
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("schema_name", ENUM_SCHEMAS)
def test_enum_values_and_names_match_the_spec(schema_name: str) -> None:
    schema = SCHEMAS[schema_name]
    expected = list(zip(schema["enum"], schema["x-enumNames"], strict=True))
    enum_type = SCHEMA_ENUMS[schema_name]
    assert [(member.value, member.name) for member in enum_type] == expected
    for value, name in expected:
        assert enum_type(value).name == name
        assert enum_type[name].value == value


def test_error_code_enums_count() -> None:
    assert len([name for name in ENUM_SCHEMAS if name.endswith("ErrorCode")]) == 17
