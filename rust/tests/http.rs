//! HTTP behavior against a local mock server (see `spec/SDK_BEHAVIOR.md` §4–§9, §12).

use std::str::FromStr;
use std::time::{Duration, Instant};

use base64::Engine;
use serde_json::{json, Value};
use topstepx::enums::*;
use topstepx::models::*;
use topstepx::{
    Client, ClientBuilder, Decimal, Error, ErrorCode, Operation, RateLimit, RateLimits, RetryPolicy,
};
use wiremock::matchers::{body_json, header, method, path};
use wiremock::{Mock, MockServer, Request, ResponseTemplate};

const USER: &str = "trader";
const KEY: &str = "api-key";

fn jwt(expires_in: chrono::Duration) -> String {
    let encode =
        |value: Value| base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(value.to_string());
    let exp = (chrono::Utc::now() + expires_in).timestamp();
    format!(
        "{}.{}.sig",
        encode(json!({"alg": "HS256"})),
        encode(json!({"sub": USER, "exp": exp}))
    )
}

fn fresh_token(tag: &str) -> String {
    // Distinct, long-lived tokens: the tag goes into an unsigned claim-free suffix.
    format!("{}{tag}", jwt(chrono::Duration::hours(24)))
}

fn ok(extra: Value) -> ResponseTemplate {
    let mut body =
        json!({"success": true, "errorCode": 0, "errorMessage": null, "hasBusinessFailure": false});
    body.as_object_mut()
        .unwrap()
        .extend(extra.as_object().unwrap().clone());
    ResponseTemplate::new(200).set_body_json(body)
}

fn builder(server: &MockServer) -> ClientBuilder {
    Client::builder()
        .base_url(server.uri())
        .credentials(USER, KEY)
        .retry_policy(RetryPolicy {
            max_retries: 3,
            initial_backoff: Duration::from_millis(5),
            max_backoff: Duration::from_millis(20),
        })
        .disable_rate_limiting()
}

async fn mount_login(server: &MockServer, token: &str) {
    Mock::given(method("POST"))
        .and(path("/api/Auth/loginKey"))
        .and(body_json(json!({"userName": USER, "apiKey": KEY})))
        .respond_with(ok(json!({"token": token})))
        .mount(server)
        .await;
}

fn account_json() -> Value {
    json!({"id": 123, "name": "EXAMPLE", "balance": 50000.5, "canTrade": true, "isVisible": true, "simulated": true})
}

async fn requests_to(server: &MockServer, target: &str) -> Vec<Request> {
    server
        .received_requests()
        .await
        .unwrap()
        .into_iter()
        .filter(|r| r.url.path() == target)
        .collect()
}

#[tokio::test]
async fn lazy_login_sends_bearer_token_and_decodes_decimals() {
    let server = MockServer::start().await;
    let token = fresh_token("a");
    mount_login(&server, &token).await;
    Mock::given(method("POST"))
        .and(path("/api/Account/search"))
        .and(header("authorization", format!("Bearer {token}").as_str()))
        .and(body_json(json!({"onlyActiveAccounts": true})))
        .respond_with(ok(json!({"accounts": [account_json()]})))
        .expect(1)
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    assert!(!client.has_token());
    let accounts = client.accounts().search(true).await.unwrap();
    assert_eq!(accounts.len(), 1);
    assert_eq!(accounts[0].balance, Decimal::from_str("50000.5").unwrap());
    assert!(client.has_token());
    assert_eq!(requests_to(&server, "/api/Auth/loginKey").await.len(), 1);
    let search = &requests_to(&server, "/api/Account/search").await[0];
    assert_eq!(search.headers["content-type"], "application/json");
    assert_eq!(search.headers["accept"], "application/json");
}

#[tokio::test]
async fn concurrent_calls_share_a_single_login() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Position/searchOpen"))
        .respond_with(ok(json!({"positions": []})))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let calls = (0..8).map(|_| {
        let client = client.clone();
        tokio::spawn(async move { client.positions().search_open(1).await })
    });
    for call in calls {
        call.await.unwrap().unwrap();
    }
    assert_eq!(requests_to(&server, "/api/Auth/loginKey").await.len(), 1);
}

#[tokio::test]
async fn invalid_credentials_are_a_business_failure() {
    let server = MockServer::start().await;
    Mock::given(path("/api/Auth/loginKey"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "success": false, "errorCode": 3, "errorMessage": null, "hasBusinessFailure": true, "token": null
        })))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let err = client.accounts().search(true).await.unwrap_err();
    let api = err.as_api().unwrap_or_else(|| panic!("{err:?}"));
    assert_eq!(api.operation, Operation::AuthLoginKey);
    assert_eq!(
        api.code,
        ErrorCode::Login(LoginErrorCode::InvalidCredentials)
    );
    assert_eq!(api.message, None);
    assert!(requests_to(&server, "/api/Account/search").await.is_empty());
    assert!(!client.has_token());
}

#[tokio::test]
async fn rejected_order_keeps_the_returned_order_id() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Order/place"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "success": false, "errorCode": 2, "errorMessage": "Trail Distance not set.",
            "hasBusinessFailure": true, "orderId": 457
        })))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let request = PlaceOrderRequest::market(123, "CON.F.US.EP.U25", OrderSide::Bid, 1);
    let err = client.orders().place(&request).await.unwrap_err();
    let api = err.as_api().unwrap();
    assert_eq!(api.code, PlaceOrderErrorCode::OrderRejected.into());
    assert_eq!(api.order_id, Some(457));
    assert_eq!(api.message.as_deref(), Some("Trail Distance not set."));

    let raw = client.execute_raw(&request).await.unwrap();
    assert!(!raw.success);
    assert_eq!(raw.order_id, Some(457));
}

#[tokio::test]
async fn unauthorized_triggers_one_relogin_and_retry() {
    let server = MockServer::start().await;
    let (first, second) = (fresh_token("first"), fresh_token("second"));
    Mock::given(path("/api/Auth/loginKey"))
        .respond_with(ok(json!({"token": first})))
        .up_to_n_times(1)
        .with_priority(1)
        .mount(&server)
        .await;
    mount_login(&server, &second).await;
    Mock::given(path("/api/Order/searchOpen"))
        .and(header("authorization", format!("Bearer {first}").as_str()))
        .respond_with(ResponseTemplate::new(401))
        .mount(&server)
        .await;
    Mock::given(path("/api/Order/searchOpen"))
        .and(header("authorization", format!("Bearer {second}").as_str()))
        .respond_with(ok(json!({"orders": []})))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    assert!(client.orders().search_open(1).await.unwrap().is_empty());
    assert_eq!(requests_to(&server, "/api/Auth/loginKey").await.len(), 2);
    assert_eq!(requests_to(&server, "/api/Order/searchOpen").await.len(), 2);
}

#[tokio::test]
async fn persistent_unauthorized_fails_after_a_single_relogin() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Trade/search"))
        .respond_with(ResponseTemplate::new(401))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let request = SearchTradeRequest::new(1, chrono::Utc::now());
    let err = client.trades().search(&request).await.unwrap_err();
    assert!(
        matches!(
            err,
            Error::Unauthorized {
                operation: Operation::TradeSearch,
                ..
            }
        ),
        "{err:?}"
    );
    assert_eq!(requests_to(&server, "/api/Auth/loginKey").await.len(), 2);
    assert_eq!(requests_to(&server, "/api/Trade/search").await.len(), 2);
}

#[tokio::test]
async fn unauthorized_without_credentials_is_returned() {
    let server = MockServer::start().await;
    Mock::given(path("/api/Account/search"))
        .respond_with(ResponseTemplate::new(401))
        .mount(&server)
        .await;
    let client = Client::builder()
        .base_url(server.uri())
        .token(fresh_token("x"))
        .build()
        .unwrap();
    let err = client.accounts().search(false).await.unwrap_err();
    assert!(matches!(err, Error::Unauthorized { .. }), "{err:?}");
    assert_eq!(requests_to(&server, "/api/Account/search").await.len(), 1);
}

#[tokio::test]
async fn rate_limited_requests_honor_retry_after() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Contract/available"))
        .respond_with(ResponseTemplate::new(429).insert_header("Retry-After", "1"))
        .up_to_n_times(1)
        .with_priority(1)
        .mount(&server)
        .await;
    Mock::given(path("/api/Contract/available"))
        .respond_with(ok(json!({"contracts": []})))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    client.login().await.unwrap();
    let started = Instant::now();
    client.contracts().available(false).await.unwrap();
    assert!(
        started.elapsed() >= Duration::from_secs(1),
        "waited {:?}",
        started.elapsed()
    );
    assert_eq!(
        requests_to(&server, "/api/Contract/available").await.len(),
        2
    );
}

#[tokio::test]
async fn rate_limit_errors_surface_after_retries_even_for_writes() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Order/cancel"))
        .respond_with(ResponseTemplate::new(429).insert_header("Retry-After", "0"))
        .mount(&server)
        .await;

    let client = builder(&server).max_retries(2).build().unwrap();
    let err = client.orders().cancel(1, 2).await.unwrap_err();
    match err {
        Error::RateLimited {
            operation,
            retry_after,
            ..
        } => {
            assert_eq!(operation, Operation::OrderCancel);
            assert_eq!(retry_after, Some(Duration::ZERO));
        }
        other => panic!("{other:?}"),
    }
    assert_eq!(
        requests_to(&server, "/api/Order/cancel").await.len(),
        3,
        "429 is retried even for writes"
    );
}

#[tokio::test]
async fn writes_are_not_retried_on_503_but_reads_are() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Order/place"))
        .respond_with(ResponseTemplate::new(503))
        .mount(&server)
        .await;
    Mock::given(path("/api/Order/searchOpen"))
        .respond_with(ResponseTemplate::new(503))
        .up_to_n_times(2)
        .with_priority(1)
        .mount(&server)
        .await;
    Mock::given(path("/api/Order/searchOpen"))
        .respond_with(ok(json!({"orders": []})))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let err = client
        .orders()
        .place(&PlaceOrderRequest::market(1, "C", OrderSide::Bid, 1))
        .await
        .unwrap_err();
    assert_eq!(
        err.status(),
        Some(reqwest::StatusCode::SERVICE_UNAVAILABLE),
        "{err:?}"
    );
    assert_eq!(requests_to(&server, "/api/Order/place").await.len(), 1);

    client.orders().search_open(1).await.unwrap();
    assert_eq!(requests_to(&server, "/api/Order/searchOpen").await.len(), 3);
}

#[tokio::test]
async fn problem_details_are_parsed_from_400() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Order/v2/query"))
        .respond_with(
            ResponseTemplate::new(400).set_body_raw(
                json!({
                    "type": "https://tools.ietf.org/html/rfc9110#section-15.5.1",
                    "title": "One or more validation errors occurred.",
                    "status": 400,
                    "errors": {"PageSize": ["must be positive"]},
                    "traceId": "00-abc-01"
                })
                .to_string(),
                "application/problem+json",
            ),
        )
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let request = SearchOrdersQueryRequest::new(GatewayOrderFilter::new(1)).page(-1, 0);
    let err = client.orders().query(&request).await.unwrap_err();
    let Error::Status(status) = err else {
        panic!("{err:?}")
    };
    assert_eq!(status.status, reqwest::StatusCode::BAD_REQUEST);
    let problem = status.problem.expect("problem details");
    assert_eq!(
        problem.title.as_deref(),
        Some("One or more validation errors occurred.")
    );
    assert_eq!(problem.status, Some(400));
    assert_eq!(problem.extensions["traceId"], "00-abc-01");
    assert!(problem.extensions.contains_key("errors"));
}

#[tokio::test]
async fn ping_accepts_text_and_json_and_goes_anonymous_without_credentials() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/Status/ping"))
        .respond_with(ResponseTemplate::new(200).set_body_raw("pong", "text/plain"))
        .up_to_n_times(1)
        .with_priority(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/Status/ping"))
        .respond_with(ResponseTemplate::new(200).set_body_json("pong"))
        .mount(&server)
        .await;

    let client = Client::builder().base_url(server.uri()).build().unwrap();
    assert_eq!(client.status().ping().await.unwrap(), "pong");
    assert_eq!(client.status().ping().await.unwrap(), "pong");
    for request in requests_to(&server, "/api/Status/ping").await {
        assert!(!request.headers.contains_key("authorization"));
    }
}

#[tokio::test]
async fn token_inside_refresh_margin_is_renewed_with_validate() {
    let server = MockServer::start().await;
    let old = jwt(chrono::Duration::minutes(10));
    let new = fresh_token("renewed");
    Mock::given(method("POST"))
        .and(path("/api/Auth/validate"))
        .and(header("authorization", format!("Bearer {old}").as_str()))
        .respond_with(ok(json!({"newToken": new})))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(path("/api/Auth/loginKey"))
        .respond_with(ResponseTemplate::new(500))
        .expect(0)
        .mount(&server)
        .await;
    Mock::given(path("/api/Account/search"))
        .and(header("authorization", format!("Bearer {new}").as_str()))
        .respond_with(ok(json!({"accounts": []})))
        .expect(2)
        .mount(&server)
        .await;

    let client = builder(&server).token(old).build().unwrap();
    client.accounts().search(true).await.unwrap();
    client.accounts().search(true).await.unwrap();
    let validate = &requests_to(&server, "/api/Auth/validate").await[0];
    assert!(validate.body.is_empty(), "validate posts an empty body");
    assert!(!validate.headers.contains_key("content-type"));
    assert_eq!(client.token().await.unwrap(), new);
}

#[tokio::test]
async fn expired_token_logs_in_instead_of_validating() {
    let server = MockServer::start().await;
    let token = fresh_token("login");
    mount_login(&server, &token).await;
    Mock::given(path("/api/Auth/validate"))
        .respond_with(ResponseTemplate::new(500))
        .expect(0)
        .mount(&server)
        .await;
    Mock::given(path("/api/Account/search"))
        .and(header("authorization", format!("Bearer {token}").as_str()))
        .respond_with(ok(json!({"accounts": []})))
        .expect(1)
        .mount(&server)
        .await;

    let client = builder(&server)
        .token(jwt(-chrono::Duration::minutes(5)))
        .build()
        .unwrap();
    client.accounts().search(true).await.unwrap();
}

#[tokio::test]
async fn failed_renewal_without_credentials_keeps_the_valid_token() {
    let server = MockServer::start().await;
    let token = jwt(chrono::Duration::minutes(10));
    Mock::given(path("/api/Auth/validate"))
        .respond_with(ResponseTemplate::new(401))
        .mount(&server)
        .await;
    Mock::given(path("/api/Account/search"))
        .and(header("authorization", format!("Bearer {token}").as_str()))
        .respond_with(ok(json!({"accounts": []})))
        .expect(1)
        .mount(&server)
        .await;

    let client = Client::builder()
        .base_url(server.uri())
        .token(token)
        .build()
        .unwrap();
    client.accounts().search(true).await.unwrap();
}

#[tokio::test]
async fn logout_clears_the_token_and_the_next_call_logs_in_again() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Auth/logout"))
        .respond_with(ok(json!({})))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(path("/api/Account/search"))
        .respond_with(ok(json!({"accounts": []})))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    client.login().await.unwrap();
    client.auth().logout().await.unwrap();
    assert!(!client.has_token());
    client.accounts().search(true).await.unwrap();
    assert_eq!(requests_to(&server, "/api/Auth/loginKey").await.len(), 2);
    assert!(requests_to(&server, "/api/Auth/logout").await[0]
        .body
        .is_empty());
}

#[tokio::test]
async fn explicit_login_key_and_validate_store_tokens() {
    let server = MockServer::start().await;
    let first = fresh_token("first");
    let renewed = fresh_token("renewed");
    mount_login(&server, &first).await;
    Mock::given(path("/api/Auth/validate"))
        .and(header("authorization", format!("Bearer {first}").as_str()))
        .respond_with(ok(json!({"newToken": renewed})))
        .mount(&server)
        .await;

    let client = Client::builder().base_url(server.uri()).build().unwrap();
    assert!(matches!(
        client.auth().validate().await,
        Err(Error::NotAuthenticated)
    ));
    assert_eq!(client.auth().login_key(USER, KEY).await.unwrap(), first);
    assert_eq!(client.auth().validate().await.unwrap(), renewed);
    assert_eq!(client.token().await.unwrap(), renewed);
}

#[tokio::test]
async fn validation_rejects_before_any_network_io() {
    let server = MockServer::start().await;
    let client = builder(&server).build().unwrap();

    let stop_limit = PlaceOrderRequest::new(1, "C", OrderType::StopLimit, OrderSide::Bid, 1);
    let err = client.orders().place(&stop_limit).await.unwrap_err();
    assert!(
        matches!(
            err,
            Error::InvalidRequest {
                operation: Operation::OrderPlace,
                ..
            }
        ),
        "{err:?}"
    );

    let trailing = PlaceOrderRequest::new(1, "C", OrderType::TrailingStop, OrderSide::Ask, 1);
    assert!(matches!(
        client.orders().place(&trailing).await,
        Err(Error::InvalidRequest { .. })
    ));

    let now = chrono::Utc::now();
    let bars = RetrieveBarRequest::new("C", AggregateBarUnit::Minute, 1, now, now, 20_001);
    assert!(matches!(
        client.history().retrieve_bars(&bars).await,
        Err(Error::InvalidRequest { .. })
    ));

    assert!(matches!(
        client.positions().partial_close(1, "C", 0).await,
        Err(Error::InvalidRequest { .. })
    ));
    assert!(
        server.received_requests().await.unwrap().is_empty(),
        "not even a login"
    );
}

#[tokio::test]
async fn validation_can_be_disabled() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Order/place"))
        .and(body_json(
            json!({"accountId": 1, "contractId": "C", "type": 3, "side": 0, "size": 1}),
        ))
        .respond_with(ok(json!({"orderId": 9})))
        .expect(1)
        .mount(&server)
        .await;
    let client = builder(&server).validate_requests(false).build().unwrap();
    let stop_limit = PlaceOrderRequest::new(1, "C", OrderType::StopLimit, OrderSide::Bid, 1);
    assert_eq!(client.orders().place(&stop_limit).await.unwrap(), 9);
}

#[tokio::test]
async fn decimals_and_timestamps_are_sent_as_numbers_and_rfc3339() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Order/place"))
        .and(body_json(json!({
            "accountId": 123, "contractId": "CON.F.US.EP.U25", "type": 5, "side": 1, "size": 1,
            "trailPrice": 5998.5, "customTag": "tag-1"
        })))
        .respond_with(ok(json!({"orderId": 456})))
        .mount(&server)
        .await;
    Mock::given(path("/api/History/retrieveBars"))
        .and(body_json(json!({
            "contractId": "CON.F.US.EP.U25", "live": false, "startTime": "2025-07-01T00:00:00Z",
            "endTime": "2025-07-02T00:00:00Z", "unit": 2, "unitNumber": 5, "limit": 100, "includePartialBar": false
        })))
        .respond_with(ok(json!({"bars": [
            {"t": "2025-07-01T14:00:00+00:00", "o": 6000.25, "h": 6002, "l": 5999.75, "c": 6001, "v": 125}
        ]})))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let order = PlaceOrderRequest::trailing_stop(
        123,
        "CON.F.US.EP.U25",
        OrderSide::Ask,
        1,
        Decimal::new(59985, 1),
    )
    .with_custom_tag("tag-1");
    assert_eq!(client.orders().place(&order).await.unwrap(), 456);

    let start = chrono::DateTime::parse_from_rfc3339("2025-07-01T00:00:00Z")
        .unwrap()
        .to_utc();
    let end = chrono::DateTime::parse_from_rfc3339("2025-07-02T00:00:00Z")
        .unwrap()
        .to_utc();
    let request = RetrieveBarRequest::new(
        "CON.F.US.EP.U25",
        AggregateBarUnit::Minute,
        5,
        start,
        end,
        100,
    );
    let bars = client.history().retrieve_bars(&request).await.unwrap();
    assert_eq!(bars[0].open, Decimal::from_str("6000.25").unwrap());
    assert_eq!(bars[0].volume, 125);
}

#[tokio::test]
async fn malformed_bodies_and_missing_data_are_reported() {
    let server = MockServer::start().await;
    mount_login(&server, &fresh_token("a")).await;
    Mock::given(path("/api/Account/search"))
        .respond_with(ResponseTemplate::new(200).set_body_raw("<html>oops</html>", "text/html"))
        .mount(&server)
        .await;
    Mock::given(path("/api/Contract/searchById"))
        .respond_with(ok(json!({})))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    let err = client.accounts().search(true).await.unwrap_err();
    assert!(
        matches!(&err, Error::Decode { body, .. } if body.contains("oops")),
        "{err:?}"
    );
    let err = client.contracts().search_by_id("CON.X").await.unwrap_err();
    assert!(
        matches!(
            err,
            Error::MissingData {
                field: "contract",
                ..
            }
        ),
        "{err:?}"
    );
}

#[tokio::test]
async fn client_side_rate_limiter_spaces_requests() {
    let server = MockServer::start().await;
    Mock::given(path("/api/Status/ping"))
        .respond_with(ResponseTemplate::new(200).set_body_raw("pong", "text/plain"))
        .mount(&server)
        .await;
    let limits = RateLimits {
        general: RateLimit::new(2, Duration::from_millis(300)),
        history: RateLimit::new(1, Duration::from_secs(30)),
    };
    let client = Client::builder()
        .base_url(server.uri())
        .rate_limits(limits)
        .build()
        .unwrap();
    let started = Instant::now();
    for _ in 0..3 {
        client.status().ping().await.unwrap();
    }
    assert!(
        started.elapsed() >= Duration::from_millis(300),
        "{:?}",
        started.elapsed()
    );
}

#[tokio::test]
async fn logout_without_a_token_is_a_no_op() {
    let server = MockServer::start().await;
    let client = Client::builder().base_url(server.uri()).build().unwrap();
    client.auth().logout().await.unwrap();
    assert!(server.received_requests().await.unwrap().is_empty());
}

#[tokio::test]
async fn ping_goes_anonymous_when_login_fails() {
    let server = MockServer::start().await;
    Mock::given(path("/api/Auth/loginKey"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "success": false, "errorCode": 3, "errorMessage": null, "token": null
        })))
        .mount(&server)
        .await;
    Mock::given(path("/api/Status/ping"))
        .respond_with(ResponseTemplate::new(200).set_body_raw("pong", "text/plain"))
        .mount(&server)
        .await;

    let client = builder(&server).build().unwrap();
    assert_eq!(client.status().ping().await.unwrap(), "pong");
    let ping = &requests_to(&server, "/api/Status/ping").await[0];
    assert!(!ping.headers.contains_key("authorization"));
}

#[tokio::test]
async fn invalidate_token_only_discards_a_matching_token() {
    let client = Client::builder().token("current").build().unwrap();
    assert!(!client.invalidate_token("older"));
    assert!(client.has_token());
    assert!(client.invalidate_token("current"));
    assert!(!client.has_token());
}

#[tokio::test]
async fn failed_renewal_pauses_further_renewals() {
    let server = MockServer::start().await;
    let token = jwt(chrono::Duration::minutes(10));
    Mock::given(path("/api/Auth/validate"))
        .respond_with(ResponseTemplate::new(500))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(path("/api/Account/search"))
        .and(header("authorization", format!("Bearer {token}").as_str()))
        .respond_with(ok(json!({"accounts": []})))
        .expect(3)
        .mount(&server)
        .await;

    let client = Client::builder()
        .base_url(server.uri())
        .token(token)
        .build()
        .unwrap();
    for _ in 0..3 {
        client.accounts().search(true).await.unwrap();
    }
}
