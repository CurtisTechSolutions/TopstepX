//! Conformance against `spec/projectx-openapi.json` (see `spec/SDK_BEHAVIOR.md` §12).

use std::collections::BTreeMap;
use std::path::Path;

use chrono::{DateTime, SecondsFormat, Utc};
use serde::de::DeserializeOwned;
use serde::Serialize;
use serde_json::{json, Map, Value};
use topstepx::enums::*;
use topstepx::models::*;
use topstepx::{AuthRequirement, HttpMethod, Operation};

fn spec() -> Value {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../spec/projectx-openapi.json");
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|err| panic!("reading {}: {err}", path.display()));
    serde_json::from_str(&text).expect("valid JSON")
}

type RoundTrip = fn(Value) -> Result<Value, String>;

fn round_trip<T: Serialize + DeserializeOwned>(value: Value) -> Result<Value, String> {
    let typed: T = serde_json::from_value(value).map_err(|err| err.to_string())?;
    serde_json::to_value(&typed).map_err(|err| err.to_string())
}

/// Every object schema of the specification, mapped to its Rust type.
fn object_models() -> BTreeMap<&'static str, RoundTrip> {
    let entries: [(&str, RoundTrip); 45] = [
        ("SearchAccountResponse", round_trip::<SearchAccountResponse>),
        ("TradingAccountModel", round_trip::<TradingAccount>),
        ("SearchAccountRequest", round_trip::<SearchAccountRequest>),
        ("LoginResponse", round_trip::<LoginResponse>),
        ("LoginAppRequest", round_trip::<LoginAppRequest>),
        ("LoginApiKeyRequest", round_trip::<LoginApiKeyRequest>),
        ("LogoutResponse", round_trip::<LogoutResponse>),
        ("ValidateResponse", round_trip::<ValidateResponse>),
        (
            "SearchContractResponse",
            round_trip::<SearchContractResponse>,
        ),
        ("ContractModel", round_trip::<Contract>),
        ("SearchContractRequest", round_trip::<SearchContractRequest>),
        (
            "SearchContractByIdResponse",
            round_trip::<SearchContractByIdResponse>,
        ),
        (
            "SearchContractByIdRequest",
            round_trip::<SearchContractByIdRequest>,
        ),
        (
            "ListAvailableContractResponse",
            round_trip::<ListAvailableContractResponse>,
        ),
        (
            "ListAvailableContractRequest",
            round_trip::<ListAvailableContractRequest>,
        ),
        ("RetrieveBarResponse", round_trip::<RetrieveBarResponse>),
        ("AggregateBarModel", round_trip::<AggregateBar>),
        ("RetrieveBarRequest", round_trip::<RetrieveBarRequest>),
        ("SearchOrderResponse", round_trip::<SearchOrderResponse>),
        ("OrderModel", round_trip::<Order>),
        ("SearchOrderRequest", round_trip::<SearchOrderRequest>),
        (
            "SearchOrderByIdResponse",
            round_trip::<SearchOrderByIdResponse>,
        ),
        (
            "SearchOrderByIdRequest",
            round_trip::<SearchOrderByIdRequest>,
        ),
        (
            "SearchOpenOrderRequest",
            round_trip::<SearchOpenOrderRequest>,
        ),
        (
            "SearchOrdersQueryResponse",
            round_trip::<SearchOrdersQueryResponse>,
        ),
        ("ProblemDetails", round_trip::<ProblemDetails>),
        (
            "SearchOrdersQueryRequest",
            round_trip::<SearchOrdersQueryRequest>,
        ),
        ("GatewayOrderFilter", round_trip::<GatewayOrderFilter>),
        ("PlaceOrderResponse", round_trip::<PlaceOrderResponse>),
        ("PlaceOrderRequest", round_trip::<PlaceOrderRequest>),
        ("PlaceOrderBracket", round_trip::<PlaceOrderBracket>),
        ("CancelOrderResponse", round_trip::<CancelOrderResponse>),
        ("CancelOrderRequest", round_trip::<CancelOrderRequest>),
        ("ModifyOrderResponse", round_trip::<ModifyOrderResponse>),
        ("ModifyOrderRequest", round_trip::<ModifyOrderRequest>),
        (
            "SearchPositionResponse",
            round_trip::<SearchPositionResponse>,
        ),
        ("PositionModel", round_trip::<Position>),
        ("SearchPositionRequest", round_trip::<SearchPositionRequest>),
        ("ClosePositionResponse", round_trip::<ClosePositionResponse>),
        (
            "CloseContractPositionRequest",
            round_trip::<CloseContractPositionRequest>,
        ),
        (
            "PartialClosePositionResponse",
            round_trip::<PartialClosePositionResponse>,
        ),
        (
            "PartialCloseContractPositionRequest",
            round_trip::<PartialCloseContractPositionRequest>,
        ),
        (
            "SearchHalfTradeResponse",
            round_trip::<SearchHalfTradeResponse>,
        ),
        ("HalfTradeModel", round_trip::<HalfTrade>),
        ("SearchTradeRequest", round_trip::<SearchTradeRequest>),
    ];
    entries.into_iter().collect()
}

macro_rules! enum_pairs {
    ($($name:ident),+ $(,)?) => {{
        let mut map: BTreeMap<&'static str, Vec<(i64, String)>> = BTreeMap::new();
        $(
            map.insert(
                stringify!($name),
                $name::KNOWN.iter().map(|v| (i64::from(v.value()), v.name().unwrap().to_owned())).collect(),
            );
        )+
        map
    }};
}

/// Every enum schema of the specification, as (value, name) pairs from the Rust enums.
fn enum_models() -> BTreeMap<&'static str, Vec<(i64, String)>> {
    enum_pairs!(
        SearchAccountErrorCode,
        LoginErrorCode,
        LogoutErrorCode,
        ValidateErrorCode,
        SearchContractErrorCode,
        SearchContractByIdErrorCode,
        ListAvailableContractErrorCode,
        RetrieveBarErrorCode,
        AggregateBarUnit,
        SearchOrderErrorCode,
        OrderStatus,
        OrderType,
        OrderSide,
        SearchOrderByIdErrorCode,
        OrderSortField,
        SortDirection,
        PlaceOrderErrorCode,
        CancelOrderErrorCode,
        ModifyOrderErrorCode,
        SearchPositionErrorCode,
        PositionType,
        ClosePositionErrorCode,
        PartialClosePositionErrorCode,
        SearchTradeErrorCode,
    )
}

/// Normalizes JSON for semantic comparison: null and absent are equal, numbers compare by
/// value, and timestamps compare by instant.
fn normalize(value: &Value) -> Value {
    match value {
        Value::Object(map) => Value::Object(
            map.iter()
                .filter(|(_, v)| !v.is_null())
                .map(|(k, v)| (k.clone(), normalize(v)))
                .collect(),
        ),
        Value::Array(items) => Value::Array(items.iter().map(normalize).collect()),
        Value::Number(number) => json!(number.as_f64().expect("finite number")),
        Value::String(text) => match DateTime::parse_from_rfc3339(text) {
            Ok(time) => Value::String(
                time.with_timezone(&Utc)
                    .to_rfc3339_opts(SecondsFormat::Nanos, true),
            ),
            Err(_) => value.clone(),
        },
        other => other.clone(),
    }
}

fn schema_name(reference: &str) -> &str {
    reference.rsplit('/').next().unwrap()
}

fn resolve<'a>(spec: &'a Value, reference: &str) -> &'a Value {
    &spec["components"]["schemas"][schema_name(reference)]
}

/// Builds an instance holding every property of `schema`, with values that pass the SDK's
/// client-side validation.
fn synthesize(spec: &Value, schema: &Value) -> Value {
    if let Some(reference) = schema.get("$ref").and_then(Value::as_str) {
        return synthesize(spec, resolve(spec, reference));
    }
    // A top-level `allOf` next to `type: object` holds conditionals (PlaceOrderRequest's
    // trailing-stop rule), not a type.
    if let Some(members) = schema
        .get("allOf")
        .and_then(Value::as_array)
        .filter(|_| schema.get("type").is_none())
    {
        // `allOf: [$ref OrderType, {enum: [1, 2, 4, ...]}]` narrows placement types.
        if let Some(restricted) = members
            .iter()
            .find_map(|m| m.get("enum").and_then(Value::as_array))
        {
            return restricted[0].clone();
        }
        return synthesize(spec, &members[0]);
    }
    if let Some(members) = schema.get("anyOf").and_then(Value::as_array) {
        let member = members
            .iter()
            .find(|m| m.get("type") != Some(&json!("null")))
            .expect("non-null member");
        return synthesize(spec, member);
    }
    if let Some(values) = schema.get("enum").and_then(Value::as_array) {
        return values[0].clone();
    }
    let kind = match &schema["type"] {
        Value::String(kind) => kind.as_str(),
        Value::Array(kinds) => kinds
            .iter()
            .filter_map(Value::as_str)
            .find(|k| *k != "null")
            .expect("a type"),
        other => panic!("schema without a type: {other}"),
    };
    match kind {
        "object" => {
            let mut map = Map::new();
            if let Some(properties) = schema.get("properties").and_then(Value::as_object) {
                for (name, property) in properties {
                    map.insert(name.clone(), synthesize(spec, property));
                }
            }
            if schema.get("additionalProperties").is_some() {
                map.insert("traceId".into(), json!("00-trace-01"));
            }
            Value::Object(map)
        }
        "array" => json!([synthesize(spec, &schema["items"])]),
        "integer" => json!(7),
        "number" => json!(1.5),
        "boolean" => json!(true),
        "string" => match schema.get("format").and_then(Value::as_str) {
            Some("date-time") => json!("2025-01-02T03:04:05Z"),
            Some("date") => json!("2025-01-02"),
            _ => json!("text"),
        },
        other => panic!("unsupported type {other}"),
    }
}

fn operations(spec: &Value) -> Vec<(String, String, String, Value)> {
    let mut out = Vec::new();
    for (path, item) in spec["paths"].as_object().unwrap() {
        for (method, operation) in item.as_object().unwrap() {
            out.push((
                operation["operationId"].as_str().unwrap().to_owned(),
                method.to_uppercase(),
                path.clone(),
                operation.clone(),
            ));
        }
    }
    out
}

#[test]
fn every_operation_is_covered_with_method_path_and_auth() {
    let spec = spec();
    let ops = operations(&spec);
    assert_eq!(ops.len(), 21);
    assert_eq!(Operation::ALL.len(), 21);
    for (id, method, path, operation) in &ops {
        let op = Operation::from_operation_id(id).unwrap_or_else(|| panic!("{id} is not covered"));
        assert_eq!(op.path(), path, "{id}");
        let expected = match method.as_str() {
            "GET" => HttpMethod::Get,
            "POST" => HttpMethod::Post,
            other => panic!("unexpected method {other}"),
        };
        assert_eq!(op.method(), expected, "{id}");
        let anonymous = operation
            .get("security")
            .and_then(Value::as_array)
            .is_some_and(Vec::is_empty);
        assert_eq!(op.auth() == AuthRequirement::None, anonymous, "{id}");
    }
}

#[test]
fn every_schema_is_modeled() {
    let spec = spec();
    let schemas = spec["components"]["schemas"].as_object().unwrap();
    assert_eq!(schemas.len(), 69);
    let objects = object_models();
    let enums = enum_models();
    assert_eq!(objects.len() + enums.len(), 69);
    for (name, schema) in schemas {
        match schema["type"].as_str() {
            Some("object") => assert!(objects.contains_key(name.as_str()), "{name} has no model"),
            Some("integer") => assert!(enums.contains_key(name.as_str()), "{name} has no enum"),
            other => panic!("{name}: unexpected schema type {other:?}"),
        }
    }
}

#[test]
fn enums_match_names_and_values() {
    let spec = spec();
    for (name, pairs) in enum_models() {
        let schema = &spec["components"]["schemas"][name];
        let values: Vec<i64> = schema["enum"]
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v.as_i64().unwrap())
            .collect();
        let names: Vec<String> = schema["x-enumNames"]
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v.as_str().unwrap().to_owned())
            .collect();
        let expected: Vec<(i64, String)> = values.into_iter().zip(names).collect();
        assert_eq!(pairs, expected, "{name}");
    }
}

fn check_round_trip(context: &str, model: &str, input: &Value) {
    let round_trip = object_models()[model];
    let output = round_trip(input.clone())
        .unwrap_or_else(|err| panic!("{context}: {model} failed to decode: {err}"));
    assert_eq!(
        normalize(&output),
        normalize(input),
        "{context}: {model} did not round-trip"
    );
}

#[test]
fn request_and_response_examples_round_trip() {
    let spec = spec();
    let mut checked = 0;
    for (id, _, _, operation) in operations(&spec) {
        if let Some(content) = operation.pointer("/requestBody/content/application~1json") {
            let model = schema_name(content["schema"]["$ref"].as_str().unwrap());
            for (name, example) in content["examples"].as_object().unwrap() {
                check_round_trip(
                    &format!("{id} request example `{name}`"),
                    model,
                    &example["value"],
                );
                checked += 1;
            }
        }
        let content = &operation["responses"]["200"]["content"]["application/json"];
        let Some(reference) = content["schema"].get("$ref").and_then(Value::as_str) else {
            assert_eq!(id, "Status_Ping", "only ping returns a bare string");
            continue;
        };
        for (name, example) in content["examples"].as_object().unwrap() {
            check_round_trip(
                &format!("{id} response example `{name}`"),
                schema_name(reference),
                &example["value"],
            );
            checked += 1;
        }
    }
    // 44 examples in total, one of which is ping's bare string.
    assert_eq!(checked, 43);
}

#[test]
fn synthesized_objects_round_trip_every_property() {
    let spec = spec();
    for name in object_models().keys() {
        let instance = synthesize(&spec, &spec["components"]["schemas"][*name]);
        check_round_trip("synthesized", name, &instance);
    }
}

#[test]
fn response_examples_expose_business_results() {
    use topstepx::GatewayResponse;
    let spec = spec();
    let place = &spec["paths"]["/api/Order/place"]["post"]["responses"]["200"]["content"]
        ["application/json"]["examples"]["rejected"]["value"];
    let response: PlaceOrderResponse = serde_json::from_value(place.clone()).unwrap();
    let error = response.check(Operation::OrderPlace).unwrap_err();
    assert_eq!(error.code, PlaceOrderErrorCode::OrderRejected.into());
    assert_eq!(error.order_id, Some(457));
    assert_eq!(error.message.as_deref(), Some("Trail Distance not set."));

    let login = &spec["paths"]["/api/Auth/loginKey"]["post"]["responses"]["200"]["content"]
        ["application/json"]["examples"]["invalidCredentials"]["value"];
    let response: LoginResponse = serde_json::from_value(login.clone()).unwrap();
    let error = response.check(Operation::AuthLoginKey).unwrap_err();
    assert_eq!(error.code, LoginErrorCode::InvalidCredentials.into());
}
