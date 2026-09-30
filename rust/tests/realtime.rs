//! Realtime behavior against a local SignalR hub (see `spec/SDK_BEHAVIOR.md` §11–§12).
#![cfg(feature = "realtime")]

use std::net::SocketAddr;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use serde_json::{json, Value};
use tokio::net::TcpListener;
use tokio::sync::{broadcast, mpsc};
use tokio::time::timeout;
use tokio_tungstenite::tungstenite::handshake::server::{ErrorResponse, Request, Response};
use tokio_tungstenite::tungstenite::Message;
use topstepx::enums::{DomType, OrderStatus, TradeLogType};
use topstepx::realtime::{
    ConnectionEvent, ConnectionState, HubOptions, MarketEvent, MarketHub, RealtimeError,
    ReconnectPolicy, TokenProvider, UnknownEvent, UserEvent, UserHub,
};
use topstepx::Error;
use url::Url;

const RS: char = '\u{1e}';
const WAIT: Duration = Duration::from_secs(5);

#[derive(Debug)]
enum ServerEvent {
    Connected {
        index: usize,
        access_token: Option<String>,
        authorization: Option<String>,
    },
    Message {
        index: usize,
        value: Value,
    },
    Disconnected {
        index: usize,
    },
}

enum Outgoing {
    Frame(String),
    Drop,
}

#[derive(Clone, Default)]
struct HubConfig {
    /// Invocations of these targets complete with an error.
    fail_targets: Vec<&'static str>,
    /// Replies to the handshake with this error.
    handshake_error: Option<&'static str>,
    /// Extra records sent in the same frame as the handshake response.
    after_handshake: Option<String>,
    /// Stops accepting connections after this many.
    max_connections: Option<usize>,
}

struct MockHub {
    addr: SocketAddr,
    events: mpsc::UnboundedReceiver<ServerEvent>,
    connections: Arc<Mutex<Vec<mpsc::UnboundedSender<Outgoing>>>>,
}

impl MockHub {
    async fn start(config: HubConfig) -> MockHub {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let (event_tx, events) = mpsc::unbounded_channel();
        let connections: Arc<Mutex<Vec<mpsc::UnboundedSender<Outgoing>>>> = Arc::default();
        let registry = connections.clone();
        tokio::spawn(async move {
            loop {
                let Ok((stream, _)) = listener.accept().await else {
                    return;
                };
                let (out_tx, out_rx) = mpsc::unbounded_channel();
                let index = {
                    let mut registry = registry.lock().unwrap();
                    registry.push(out_tx);
                    registry.len() - 1
                };
                tokio::spawn(serve(
                    stream,
                    index,
                    event_tx.clone(),
                    out_rx,
                    config.clone(),
                ));
                if config.max_connections.is_some_and(|max| index + 1 >= max) {
                    return; // drops the listener: later connects are refused
                }
            }
        });
        MockHub {
            addr,
            events,
            connections,
        }
    }

    fn url(&self) -> Url {
        Url::parse(&format!("http://{}/hubs/user", self.addr)).unwrap()
    }

    async fn next(&mut self) -> ServerEvent {
        timeout(WAIT, self.events.recv())
            .await
            .expect("server event in time")
            .expect("server running")
    }

    async fn expect_connected(&mut self) -> (usize, Option<String>, Option<String>) {
        match self.next().await {
            ServerEvent::Connected {
                index,
                access_token,
                authorization,
            } => (index, access_token, authorization),
            other => panic!("expected a connection, got {other:?}"),
        }
    }

    /// Next message that is not a keep-alive ping.
    async fn expect_message(&mut self) -> (usize, Value) {
        loop {
            match self.next().await {
                ServerEvent::Message { value, .. } if value["type"] == 6 => continue,
                ServerEvent::Message { index, value } => return (index, value),
                other => panic!("expected a message, got {other:?}"),
            }
        }
    }

    fn send(&self, index: usize, message: Value) {
        let frame = format!("{message}{RS}");
        self.connections.lock().unwrap()[index]
            .send(Outgoing::Frame(frame))
            .unwrap();
    }

    fn send_raw(&self, index: usize, frame: String) {
        self.connections.lock().unwrap()[index]
            .send(Outgoing::Frame(frame))
            .unwrap();
    }

    fn drop_connection(&self, index: usize) {
        self.connections.lock().unwrap()[index]
            .send(Outgoing::Drop)
            .unwrap();
    }
}

// tungstenite's accept callback signature fixes the large `ErrorResponse` error type.
#[allow(clippy::result_large_err)]
async fn serve(
    stream: tokio::net::TcpStream,
    index: usize,
    events: mpsc::UnboundedSender<ServerEvent>,
    mut outgoing: mpsc::UnboundedReceiver<Outgoing>,
    config: HubConfig,
) {
    let seen: Arc<Mutex<(Option<String>, Option<String>)>> = Arc::default();
    let capture = seen.clone();
    let callback =
        move |request: &Request, response: Response| -> Result<Response, ErrorResponse> {
            let url = Url::parse(&format!("ws://localhost{}", request.uri())).unwrap();
            let token = url
                .query_pairs()
                .find(|(k, _)| k == "access_token")
                .map(|(_, v)| v.into_owned());
            let authorization = request
                .headers()
                .get("authorization")
                .map(|v| v.to_str().unwrap().to_owned());
            *capture.lock().unwrap() = (token, authorization);
            Ok(response)
        };
    let Ok(ws) = tokio_tungstenite::accept_hdr_async(stream, callback).await else {
        return;
    };
    let (access_token, authorization) = seen.lock().unwrap().clone();
    let _ = events.send(ServerEvent::Connected {
        index,
        access_token,
        authorization,
    });
    let (mut sink, mut source) = ws.split();

    // Handshake.
    let Some(Ok(Message::Text(handshake))) = source.next().await else {
        return;
    };
    assert_eq!(
        handshake.as_str(),
        "{\"protocol\":\"json\",\"version\":1}\u{1e}"
    );
    if let Some(error) = config.handshake_error {
        let _ = sink
            .send(Message::text(format!("{}{RS}", json!({"error": error}))))
            .await;
        return;
    }
    let mut reply = format!("{{}}{RS}");
    if let Some(extra) = &config.after_handshake {
        reply.push_str(extra);
    }
    let _ = sink.send(Message::text(reply)).await;

    loop {
        tokio::select! {
            incoming = source.next() => match incoming {
                Some(Ok(Message::Text(text))) => {
                    for record in text.as_str().split(RS).filter(|r| !r.is_empty()) {
                        let value: Value = serde_json::from_str(record).unwrap();
                        if value["type"] == 1 {
                            let target = value["target"].as_str().unwrap();
                            let completion = if config.fail_targets.contains(&target) {
                                json!({"type": 3, "invocationId": value["invocationId"], "error": format!("{target} refused")})
                            } else {
                                json!({"type": 3, "invocationId": value["invocationId"], "result": null})
                            };
                            let _ = sink.send(Message::text(format!("{completion}{RS}"))).await;
                        }
                        let _ = events.send(ServerEvent::Message { index, value });
                    }
                }
                Some(Ok(_)) => {}
                _ => {
                    let _ = events.send(ServerEvent::Disconnected { index });
                    return;
                }
            },
            command = outgoing.recv() => match command {
                Some(Outgoing::Frame(frame)) => {
                    let _ = sink.send(Message::text(frame)).await;
                }
                Some(Outgoing::Drop) | None => {
                    let _ = events.send(ServerEvent::Disconnected { index });
                    return;
                }
            },
        }
    }
}

fn fast_options() -> HubOptions {
    HubOptions::default()
        .keep_alive_interval(Duration::from_secs(10))
        .server_timeout(Duration::from_secs(20))
        .handshake_timeout(Duration::from_secs(2))
        .invoke_timeout(Duration::from_secs(2))
        .reconnect(ReconnectPolicy::new(
            vec![Duration::ZERO],
            Duration::from_millis(50),
            None,
        ))
}

/// Token provider returning `token-1`, `token-2`, ... on successive calls.
fn counting_tokens() -> (TokenProvider, Arc<AtomicUsize>) {
    let calls = Arc::new(AtomicUsize::new(0));
    let counter = calls.clone();
    let provider = TokenProvider::new(move || {
        let n = counter.fetch_add(1, Ordering::SeqCst) + 1;
        async move { Ok(format!("token-{n}")) }
    });
    (provider, calls)
}

async fn next_event<E: Clone>(events: &mut broadcast::Receiver<E>) -> E {
    timeout(WAIT, events.recv())
        .await
        .expect("event in time")
        .expect("stream open")
}

async fn next_user_data(events: &mut broadcast::Receiver<UserEvent>) -> UserEvent {
    loop {
        match next_event(events).await {
            UserEvent::Connection(_) => continue,
            other => return other,
        }
    }
}

fn order(id: i64) -> Value {
    json!({"id": id, "accountId": 7, "contractId": "CON.F.US.EP.U25", "symbolId": "F.US.EP",
           "creationTimestamp": "2025-07-01T14:00:00.1234567+00:00", "status": 1, "type": 1, "side": 0,
           "size": 1, "limitPrice": 6000.25, "fillVolume": 0})
}

#[tokio::test]
async fn handshake_subscription_and_event_dispatch() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let hub = UserHub::connect(
        server.url(),
        TokenProvider::fixed("token-abc"),
        fast_options(),
    )
    .await
    .unwrap();
    let (index, access_token, authorization) = server.expect_connected().await;
    assert_eq!(access_token.as_deref(), Some("token-abc"));
    assert_eq!(authorization.as_deref(), Some("Bearer token-abc"));
    assert_eq!(hub.state(), ConnectionState::Connected);

    let mut events = hub.events();
    hub.subscribe_orders(7).await.unwrap();
    let (_, invocation) = server.expect_message().await;
    assert_eq!(
        invocation,
        json!({"type": 1, "invocationId": "1", "target": "SubscribeOrders", "arguments": [7]})
    );
    hub.subscribe_accounts().await.unwrap();
    assert_eq!(server.expect_message().await.1["arguments"], json!([]));

    server.send(
        index,
        json!({"type": 1, "target": "GatewayUserOrder", "arguments": [order(1)]}),
    );
    server.send(index, json!({"type": 1, "target": "GatewayUserOrder", "arguments": [{"action": 1, "data": order(2)}]}));
    // Two records in one frame, the second an array of both forms.
    server.send_raw(
        index,
        format!(
            "{}{RS}{}{RS}",
            json!({"type": 1, "target": "GatewayUserAccount", "arguments": [{"id": 7, "name": "A", "balance": 1.5}]}),
            json!({"type": 1, "target": "GatewayUserOrder", "arguments": [[order(3), {"action": 2, "data": order(4)}]]})
        ),
    );
    server.send(
        index,
        json!({"type": 1, "target": "GatewayBrandNew", "arguments": [1, 2]}),
    );

    let mut orders = Vec::new();
    for _ in 0..4 {
        match next_user_data(&mut events).await {
            UserEvent::Order { order, action } => orders.push((order.id, action, order.status)),
            UserEvent::Account { account, .. } => assert_eq!(account.id, 7),
            other => panic!("unexpected {other:?}"),
        }
    }
    // The account event arrives between orders 2 and 3.
    assert!(
        matches!(next_user_data(&mut events).await, UserEvent::Order { order, action: Some(2) } if order.id == 4)
    );
    assert_eq!(
        orders,
        vec![
            (1, None, Some(OrderStatus::Open)),
            (2, Some(1), Some(OrderStatus::Open)),
            (3, None, Some(OrderStatus::Open))
        ]
    );
    match next_user_data(&mut events).await {
        UserEvent::Unknown(UnknownEvent {
            target,
            arguments,
            error: None,
            ..
        }) => {
            assert_eq!(target, "GatewayBrandNew");
            assert_eq!(arguments, vec![json!(1), json!(2)]);
        }
        other => panic!("unexpected {other:?}"),
    }
    hub.close().await;
}

#[tokio::test]
async fn hub_errors_are_returned_to_the_caller() {
    let mut server = MockHub::start(HubConfig {
        fail_targets: vec!["SubscribeTrades"],
        ..HubConfig::default()
    })
    .await;
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), fast_options())
        .await
        .unwrap();
    server.expect_connected().await;
    let err = hub.subscribe_trades(7).await.unwrap_err();
    match err {
        Error::Realtime(RealtimeError::Invocation { target, message }) => {
            assert_eq!(target, "SubscribeTrades");
            assert_eq!(message, "SubscribeTrades refused");
        }
        other => panic!("{other:?}"),
    }
    assert_eq!(
        hub.invoke("Echo", vec![json!("x")]).await.unwrap(),
        json!(null)
    );
}

#[tokio::test]
async fn keep_alive_pings_are_sent() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let options = fast_options().keep_alive_interval(Duration::from_millis(50));
    let _hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), options)
        .await
        .unwrap();
    server.expect_connected().await;
    let mut pings = 0;
    while pings < 2 {
        if let ServerEvent::Message { value, .. } = server.next().await {
            if value == json!({"type": 6}) {
                pings += 1;
            }
        }
    }
}

#[tokio::test]
async fn reconnects_with_a_fresh_token_and_replays_active_subscriptions() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let (tokens, calls) = counting_tokens();
    let hub = UserHub::connect(server.url(), tokens, fast_options())
        .await
        .unwrap();
    let (first, token, _) = server.expect_connected().await;
    assert_eq!(token.as_deref(), Some("token-1"));
    let mut events = hub.events();

    hub.subscribe_accounts().await.unwrap();
    hub.subscribe_orders(7).await.unwrap();
    hub.subscribe_positions(7).await.unwrap();
    hub.subscribe_orders(7).await.unwrap(); // duplicate: replayed once
    hub.unsubscribe_positions(7).await.unwrap();
    for _ in 0..5 {
        server.expect_message().await;
    }

    server.drop_connection(first);
    assert!(matches!(server.next().await, ServerEvent::Disconnected { index } if index == first));
    let (second, token, authorization) = server.expect_connected().await;
    assert_eq!(second, 1);
    assert_eq!(token.as_deref(), Some("token-2"));
    assert_eq!(authorization.as_deref(), Some("Bearer token-2"));

    let replayed: Vec<(String, Value)> = vec![
        server.expect_message().await.1,
        server.expect_message().await.1,
    ]
    .into_iter()
    .map(|m| {
        (
            m["target"].as_str().unwrap().to_owned(),
            m["arguments"].clone(),
        )
    })
    .collect();
    assert_eq!(
        replayed,
        vec![
            ("SubscribeAccounts".to_owned(), json!([])),
            ("SubscribeOrders".to_owned(), json!([7]))
        ]
    );

    let mut saw_reconnecting = false;
    loop {
        match next_event(&mut events).await {
            UserEvent::Connection(ConnectionEvent::Reconnecting { attempt: 1, .. }) => {
                saw_reconnecting = true
            }
            UserEvent::Connection(ConnectionEvent::Reconnected) => break,
            other => panic!("unexpected {other:?}"),
        }
    }
    assert!(saw_reconnecting);
    assert_eq!(hub.state(), ConnectionState::Connected);
    assert_eq!(calls.load(Ordering::SeqCst), 2);

    // The new connection delivers events.
    server.send(
        second,
        json!({"type": 1, "target": "GatewayUserOrder", "arguments": [order(9)]}),
    );
    assert!(
        matches!(next_user_data(&mut events).await, UserEvent::Order { order, .. } if order.id == 9)
    );
}

#[tokio::test]
async fn silent_server_is_detected_and_replaced() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let options = fast_options()
        .keep_alive_interval(Duration::from_millis(50))
        .server_timeout(Duration::from_millis(300));
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), options)
        .await
        .unwrap();
    let mut events = hub.events();
    server.expect_connected().await;
    match next_event(&mut events).await {
        UserEvent::Connection(ConnectionEvent::Reconnecting { cause, .. }) => {
            assert!(cause.contains("no message from the server"), "{cause}");
        }
        other => panic!("unexpected {other:?}"),
    }
    loop {
        if let ServerEvent::Connected { index, .. } = server.next().await {
            assert_eq!(index, 1);
            break;
        }
    }
}

#[tokio::test]
async fn invocations_fail_fast_while_reconnecting() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let options = fast_options().reconnect(ReconnectPolicy::new(
        vec![Duration::from_secs(30)],
        Duration::from_secs(30),
        None,
    ));
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), options)
        .await
        .unwrap();
    let (index, ..) = server.expect_connected().await;
    server.drop_connection(index);
    assert_eq!(
        hub.wait_for_state(ConnectionState::Reconnecting).await,
        ConnectionState::Reconnecting
    );
    let err = hub.subscribe_orders(1).await.unwrap_err();
    assert!(
        matches!(err, Error::Realtime(RealtimeError::NotConnected)),
        "{err:?}"
    );
    hub.close().await;
    assert_eq!(hub.state(), ConnectionState::Closed);
}

#[tokio::test]
async fn close_sends_a_close_message_and_ends_the_stream() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), fast_options())
        .await
        .unwrap();
    server.expect_connected().await;
    let mut events = hub.events();
    hub.close().await;
    assert_eq!(server.expect_message().await.1, json!({"type": 7}));
    assert_eq!(
        next_event(&mut events).await,
        UserEvent::Connection(ConnectionEvent::Closed { cause: None })
    );
    assert!(matches!(
        timeout(WAIT, events.recv()).await.unwrap(),
        Err(broadcast::error::RecvError::Closed)
    ));
    assert_eq!(hub.state(), ConnectionState::Closed);
    let err = hub.subscribe_accounts().await.unwrap_err();
    assert!(
        matches!(err, Error::Realtime(RealtimeError::Closed)),
        "{err:?}"
    );
}

#[tokio::test]
async fn server_close_is_final_unless_reconnect_is_allowed() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), fast_options())
        .await
        .unwrap();
    let (index, ..) = server.expect_connected().await;
    let mut events = hub.events();
    server.send(
        index,
        json!({"type": 7, "error": "maintenance", "allowReconnect": true}),
    );
    let (second, ..) = loop {
        if let ServerEvent::Connected {
            index,
            access_token,
            authorization,
        } = server.next().await
        {
            break (index, access_token, authorization);
        }
    };
    assert_eq!(second, 1);
    assert_eq!(
        hub.wait_for_state(ConnectionState::Connected).await,
        ConnectionState::Connected
    );

    server.send(second, json!({"type": 7, "error": "bye"}));
    loop {
        match next_event(&mut events).await {
            UserEvent::Connection(ConnectionEvent::Closed { cause }) => {
                assert_eq!(cause.as_deref(), Some("server closed the connection: bye"));
                break;
            }
            UserEvent::Connection(_) => continue,
            other => panic!("unexpected {other:?}"),
        }
    }
    assert_eq!(hub.state(), ConnectionState::Closed);
}

#[tokio::test]
async fn gives_up_after_the_configured_attempts() {
    let mut server = MockHub::start(HubConfig {
        max_connections: Some(1),
        ..HubConfig::default()
    })
    .await;
    let options = fast_options().reconnect(ReconnectPolicy::new(
        vec![Duration::ZERO; 2],
        Duration::ZERO,
        Some(2),
    ));
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), options)
        .await
        .unwrap();
    let (index, ..) = server.expect_connected().await;
    let mut events = hub.events();
    server.drop_connection(index);
    let mut attempts = Vec::new();
    loop {
        match next_event(&mut events).await {
            UserEvent::Connection(ConnectionEvent::Reconnecting { attempt, .. }) => {
                attempts.push(attempt)
            }
            UserEvent::Connection(ConnectionEvent::Closed { cause }) => {
                assert!(cause
                    .unwrap()
                    .contains("gave up after 2 reconnect attempt(s)"));
                break;
            }
            other => panic!("unexpected {other:?}"),
        }
    }
    assert_eq!(attempts, vec![1, 2]);
}

#[tokio::test]
async fn handshake_errors_fail_the_connect() {
    let server = MockHub::start(HubConfig {
        handshake_error: Some("unsupported protocol"),
        ..HubConfig::default()
    })
    .await;
    let err = UserHub::connect(server.url(), TokenProvider::fixed("t"), fast_options())
        .await
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "SignalR handshake failed: unsupported protocol"
    );
}

#[tokio::test]
async fn messages_sharing_the_handshake_frame_are_processed() {
    let close = format!(
        "{}{RS}",
        json!({"type": 7, "error": "in the handshake frame"})
    );
    let mut server = MockHub::start(HubConfig {
        after_handshake: Some(close),
        ..HubConfig::default()
    })
    .await;
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), fast_options())
        .await
        .unwrap();
    server.expect_connected().await;
    assert_eq!(
        hub.wait_for_state(ConnectionState::Closed).await,
        ConnectionState::Closed
    );
}

#[tokio::test]
async fn market_hub_events_and_subscriptions() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let hub = MarketHub::connect(server.url(), TokenProvider::fixed("t"), fast_options())
        .await
        .unwrap();
    let (index, ..) = server.expect_connected().await;
    let mut events = hub.events();
    hub.subscribe_contract_quotes("CON.F.US.EP.U25")
        .await
        .unwrap();
    hub.subscribe_contract_trades("CON.F.US.EP.U25")
        .await
        .unwrap();
    hub.subscribe_contract_market_depth("CON.F.US.EP.U25")
        .await
        .unwrap();
    hub.unsubscribe_contract_trades("CON.F.US.EP.U25")
        .await
        .unwrap();
    let targets: Vec<String> = [
        server.expect_message().await.1,
        server.expect_message().await.1,
        server.expect_message().await.1,
        server.expect_message().await.1,
    ]
    .iter()
    .map(|m| m["target"].as_str().unwrap().to_owned())
    .collect();
    assert_eq!(
        targets,
        [
            "SubscribeContractQuotes",
            "SubscribeContractTrades",
            "SubscribeContractMarketDepth",
            "UnsubscribeContractTrades"
        ]
    );

    let id = "CON.F.US.EP.U25";
    server.send(index, json!({"type": 1, "target": "GatewayQuote", "arguments": [id, {"bestBid": 6000.25, "bestAsk": 6000.5}]}));
    server.send(index, json!({"type": 1, "target": "GatewayTrade", "arguments": [id, [{"price": 6000.5, "type": 1, "volume": 3}]]}));
    server.send(index, json!({"type": 1, "target": "GatewayDepth", "arguments": [id, {"type": 2, "price": 6000.25, "volume": 12.0}]}));

    let mut received = Vec::new();
    while received.len() < 3 {
        match next_event(&mut events).await {
            MarketEvent::Connection(_) => {}
            event => received.push(event),
        }
    }
    assert!(
        matches!(&received[0], MarketEvent::Quote { contract_id, quote } if contract_id == id && quote.best_ask.is_some())
    );
    assert!(
        matches!(&received[1], MarketEvent::Trades { trades, .. } if trades[0].trade_type == Some(TradeLogType::Sell))
    );
    assert!(
        matches!(&received[2], MarketEvent::Depth { entries, .. } if entries[0].depth_type == Some(DomType::Bid) && entries[0].volume == Some(12))
    );
}

#[tokio::test]
async fn dropping_every_handle_closes_the_connection() {
    let mut server = MockHub::start(HubConfig::default()).await;
    let hub = UserHub::connect(server.url(), TokenProvider::fixed("t"), fast_options())
        .await
        .unwrap();
    server.expect_connected().await;
    let mut events = hub.events();
    let clone = hub.clone();
    drop(hub);
    drop(clone);
    assert_eq!(server.expect_message().await.1, json!({"type": 7}));
    assert_eq!(
        next_event(&mut events).await,
        UserEvent::Connection(ConnectionEvent::Closed { cause: None })
    );
}
