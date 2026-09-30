//! Generic hub connection: a background task owns the socket and talks to handles through
//! channels.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use serde_json::Value;
use tokio::net::TcpStream;
use tokio::sync::{broadcast, mpsc, oneshot, watch};
use tokio::time::{sleep_until, timeout, timeout_at, Instant};
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::http::header::{HeaderValue, AUTHORIZATION};
use tokio_tungstenite::tungstenite::protocol::CloseFrame;
use tokio_tungstenite::tungstenite::{Error as WsError, Message};
use tokio_tungstenite::{connect_async, MaybeTlsStream, WebSocketStream};
use url::Url;

use super::protocol::{self, FrameBuffer, HubMessage};
use super::{ConnectionEvent, ConnectionState, HubEvent, HubOptions, RealtimeError, TokenProvider};

type WsStream = WebSocketStream<MaybeTlsStream<TcpStream>>;
type Reply = oneshot::Sender<Result<Value, RealtimeError>>;

/// A subscription to replay after reconnecting.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct Subscription {
    pub(crate) method: &'static str,
    pub(crate) arguments: Vec<Value>,
}

/// How a successful invocation changes the subscription registry.
#[derive(Debug)]
pub(crate) enum RegistryUpdate {
    None,
    Add(Subscription),
    Remove(Subscription),
}

enum Command {
    Invoke {
        target: String,
        arguments: Vec<Value>,
        update: RegistryUpdate,
        reply: Reply,
    },
    Close {
        done: oneshot::Sender<()>,
    },
}

/// An open socket that completed the SignalR handshake.
struct Socket {
    ws: WsStream,
    buffer: FrameBuffer,
    /// Messages that arrived in the same frame as the handshake response.
    backlog: Vec<HubMessage>,
}

/// Builds the WebSocket URL: `https` becomes `wss`, `http` becomes `ws`, and the token is
/// set as `access_token`, replacing one already in the configured URL (the official guide
/// builds hub URLs as `...?access_token=` + token, and two values would reach the server as
/// one comma-joined token).
fn websocket_url(hub: &Url, token: &str) -> Result<Url, RealtimeError> {
    let mut url = hub.clone();
    let scheme = match url.scheme() {
        "https" | "wss" => "wss",
        "http" | "ws" => "ws",
        other => return Err(RealtimeError::Url(format!("unsupported scheme `{other}`"))),
    };
    url.set_scheme(scheme)
        .map_err(|()| RealtimeError::Url(format!("cannot use scheme `{scheme}`")))?;
    let retained: Vec<(String, String)> = url
        .query_pairs()
        .filter(|(key, _)| key != "access_token")
        .map(|(key, value)| (key.into_owned(), value.into_owned()))
        .collect();
    url.set_query(None);
    url.query_pairs_mut()
        .extend_pairs(retained)
        .append_pair("access_token", token);
    Ok(url)
}

fn hub_label(hub: &Url) -> String {
    let mut label = hub.clone();
    label.set_query(None);
    label.to_string()
}

async fn open(
    hub: &Url,
    token: &str,
    handshake_timeout: Duration,
) -> Result<Socket, RealtimeError> {
    let connect_error = |message: String| RealtimeError::Connect {
        url: hub_label(hub),
        message,
    };
    let mut request = websocket_url(hub, token)?
        .as_str()
        .into_client_request()
        .map_err(|err| connect_error(err.to_string()))?;
    let bearer = HeaderValue::from_str(&format!("Bearer {token}")).map_err(|_| {
        RealtimeError::Token("the token contains characters not allowed in a header".into())
    })?;
    request.headers_mut().insert(AUTHORIZATION, bearer);

    let deadline = Instant::now() + handshake_timeout;
    let (mut ws, _response) = timeout_at(deadline, connect_async(request))
        .await
        .map_err(|_| RealtimeError::Timeout {
            what: "the WebSocket connection".into(),
        })?
        .map_err(|err| match err {
            WsError::Http(response) => RealtimeError::UpgradeRejected {
                url: hub_label(hub),
                status: response.status().as_u16(),
            },
            other => connect_error(other.to_string()),
        })?;

    ws.send(Message::text(protocol::HANDSHAKE_REQUEST))
        .await
        .map_err(|err| RealtimeError::Handshake(err.to_string()))?;
    let mut buffer = FrameBuffer::default();
    loop {
        let message =
            timeout_at(deadline, ws.next())
                .await
                .map_err(|_| RealtimeError::Timeout {
                    what: "the handshake response".into(),
                })?;
        let text = match message {
            Some(Ok(Message::Text(text))) => text.as_str().to_owned(),
            Some(Ok(Message::Binary(bytes))) => String::from_utf8(bytes.to_vec())
                .map_err(|_| RealtimeError::Handshake("handshake response is not UTF-8".into()))?,
            Some(Ok(Message::Close(frame))) => {
                return Err(RealtimeError::Handshake(format!(
                    "server closed the connection{}",
                    close_reason(frame)
                )));
            }
            Some(Ok(_)) => continue,
            Some(Err(err)) => return Err(RealtimeError::Handshake(err.to_string())),
            None => {
                return Err(RealtimeError::Handshake(
                    "connection ended during the handshake".into(),
                ))
            }
        };
        let mut records = buffer.push(&text).into_iter();
        if let Some(first) = records.next() {
            protocol::parse_handshake(&first).map_err(RealtimeError::Handshake)?;
            let backlog = records
                .filter_map(|record| protocol::parse_message(&record).ok())
                .collect();
            return Ok(Socket {
                ws,
                buffer,
                backlog,
            });
        }
    }
}

/// Fetches a token and opens a socket. A token refused with HTTP 401 is reported to the
/// provider so the next attempt does not reuse it.
async fn establish(
    hub: &Url,
    tokens: &TokenProvider,
    handshake_timeout: Duration,
) -> Result<Socket, RealtimeError> {
    let token = tokens
        .token()
        .await
        .map_err(|err| RealtimeError::Token(err.to_string()))?;
    let result = open(hub, &token, handshake_timeout).await;
    if let Err(RealtimeError::UpgradeRejected { status: 401, .. }) = &result {
        tokens.reject(&token);
    }
    result
}

fn close_reason(frame: Option<CloseFrame>) -> String {
    match frame {
        Some(frame) if !frame.reason.is_empty() => {
            format!(" ({}: {})", u16::from(frame.code), frame.reason)
        }
        Some(frame) => format!(" ({})", u16::from(frame.code)),
        None => String::new(),
    }
}

async fn close_socket(socket: &mut Socket) {
    let _ = timeout(Duration::from_secs(2), async {
        let _ = socket.ws.send(Message::text(protocol::CLOSE)).await;
        let _ = socket.ws.close(None).await;
    })
    .await;
}

/// Handle to a running connection. Clones share the connection; it closes when the last
/// handle is dropped or [`close`](Self::close) is called.
pub(crate) struct Connection<E> {
    shared: Arc<Shared<E>>,
}

impl<E> Clone for Connection<E> {
    fn clone(&self) -> Self {
        Connection {
            shared: Arc::clone(&self.shared),
        }
    }
}

struct Shared<E> {
    url: Url,
    commands: mpsc::UnboundedSender<Command>,
    /// Never read; used to create receivers with `resubscribe`, so the stream ends when the
    /// driver drops the only sender.
    events: Mutex<broadcast::Receiver<E>>,
    state: watch::Receiver<ConnectionState>,
    invoke_timeout: Duration,
}

impl<E: HubEvent> Connection<E> {
    pub(crate) async fn connect(
        url: Url,
        tokens: TokenProvider,
        options: HubOptions,
    ) -> Result<Self, RealtimeError> {
        let socket = establish(&url, &tokens, options.handshake_timeout).await?;
        tracing::debug!(hub = %hub_label(&url), "hub connected");

        let (command_tx, command_rx) = mpsc::unbounded_channel();
        let (event_tx, event_rx) = broadcast::channel(options.event_capacity.max(1));
        let (state_tx, state_rx) = watch::channel(ConnectionState::Connected);
        let invoke_timeout = options.invoke_timeout;
        let driver = Driver {
            url: url.clone(),
            tokens,
            options,
            commands: command_rx,
            events: event_tx,
            state: state_tx,
            pending: HashMap::new(),
            next_invocation_id: 0,
            subscriptions: Vec::new(),
        };
        tokio::spawn(driver.run(socket));
        Ok(Connection {
            shared: Arc::new(Shared {
                url,
                commands: command_tx,
                events: Mutex::new(event_rx),
                state: state_rx,
                invoke_timeout,
            }),
        })
    }

    pub(crate) async fn invoke(
        &self,
        target: &str,
        arguments: Vec<Value>,
        update: RegistryUpdate,
    ) -> Result<Value, RealtimeError> {
        let (reply, response) = oneshot::channel();
        let command = Command::Invoke {
            target: target.to_owned(),
            arguments,
            update,
            reply,
        };
        self.shared
            .commands
            .send(command)
            .map_err(|_| RealtimeError::Closed)?;
        match timeout(self.shared.invoke_timeout, response).await {
            Ok(Ok(result)) => result,
            Ok(Err(_)) => Err(RealtimeError::Closed),
            Err(_) => Err(RealtimeError::Timeout {
                what: format!("completion of `{target}`"),
            }),
        }
    }

    pub(crate) async fn subscribe(
        &self,
        method: &'static str,
        arguments: Vec<Value>,
    ) -> Result<(), RealtimeError> {
        let update = RegistryUpdate::Add(Subscription {
            method,
            arguments: arguments.clone(),
        });
        self.invoke(method, arguments, update).await.map(drop)
    }

    pub(crate) async fn unsubscribe(
        &self,
        subscribe_method: &'static str,
        unsubscribe_method: &'static str,
        arguments: Vec<Value>,
    ) -> Result<(), RealtimeError> {
        let update = RegistryUpdate::Remove(Subscription {
            method: subscribe_method,
            arguments: arguments.clone(),
        });
        self.invoke(unsubscribe_method, arguments, update)
            .await
            .map(drop)
    }

    pub(crate) fn events(&self) -> broadcast::Receiver<E> {
        self.shared
            .events
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .resubscribe()
    }

    pub(crate) fn state(&self) -> ConnectionState {
        *self.shared.state.borrow()
    }

    pub(crate) async fn wait_for_state(&self, wanted: ConnectionState) -> ConnectionState {
        let mut state = self.shared.state.clone();
        let reached = match state
            .wait_for(|current| *current == wanted || *current == ConnectionState::Closed)
            .await
        {
            Ok(current) => *current,
            Err(_) => ConnectionState::Closed,
        };
        reached
    }

    pub(crate) async fn close(&self) {
        let (done, finished) = oneshot::channel();
        if self.shared.commands.send(Command::Close { done }).is_ok() {
            let _ = finished.await;
        }
    }

    pub(crate) fn url(&self) -> &Url {
        &self.shared.url
    }
}

struct Pending {
    target: String,
    update: RegistryUpdate,
    /// `None` for replayed subscriptions, whose failures become events.
    reply: Option<Reply>,
}

enum SessionEnd {
    Lost(String),
    ServerClosed {
        error: Option<String>,
        allow_reconnect: bool,
    },
    /// Closed by the client: explicitly (with an acknowledgement) or because every handle was
    /// dropped.
    Closed(Option<oneshot::Sender<()>>),
}

enum Reconnect {
    Connected(Box<Socket>),
    GaveUp(String),
    Closed(Option<oneshot::Sender<()>>),
}

/// Background task owning the socket.
struct Driver<E> {
    url: Url,
    tokens: TokenProvider,
    options: HubOptions,
    commands: mpsc::UnboundedReceiver<Command>,
    events: broadcast::Sender<E>,
    state: watch::Sender<ConnectionState>,
    pending: HashMap<String, Pending>,
    next_invocation_id: u64,
    subscriptions: Vec<Subscription>,
}

impl<E: HubEvent> Driver<E> {
    async fn run(mut self, mut socket: Socket) {
        loop {
            let end = self.session(&mut socket).await;
            self.fail_pending(|target| RealtimeError::ConnectionLost { target });
            let cause = match end {
                SessionEnd::Closed(done) => return self.finish(None, done),
                SessionEnd::ServerClosed {
                    error,
                    allow_reconnect: false,
                } => {
                    let detail = error.map(|e| format!(": {e}")).unwrap_or_default();
                    return self
                        .finish(Some(format!("server closed the connection{detail}")), None);
                }
                SessionEnd::ServerClosed {
                    error,
                    allow_reconnect: true,
                } => error.unwrap_or_else(|| "server requested a reconnect".into()),
                SessionEnd::Lost(cause) => cause,
            };
            tracing::warn!(hub = %hub_label(&self.url), %cause, "hub connection lost");
            let _ = self.state.send(ConnectionState::Reconnecting);
            match self.reconnect(cause).await {
                Reconnect::Connected(new_socket) => {
                    socket = *new_socket;
                    match self.replay_subscriptions(&mut socket).await {
                        Ok(()) => {
                            tracing::info!(hub = %hub_label(&self.url), "hub reconnected");
                            let _ = self.state.send(ConnectionState::Connected);
                            self.emit(ConnectionEvent::Reconnected);
                        }
                        // The next session notices the broken socket and reconnects again.
                        Err(cause) => tracing::debug!(%cause, "replaying subscriptions failed"),
                    }
                }
                Reconnect::GaveUp(cause) => return self.finish(Some(cause), None),
                Reconnect::Closed(done) => return self.finish(None, done),
            }
        }
    }

    fn emit(&self, event: ConnectionEvent) {
        let _ = self.events.send(E::from_connection(event));
    }

    async fn session(&mut self, socket: &mut Socket) -> SessionEnd {
        let mut last_sent = Instant::now();
        let mut last_received = Instant::now();
        for message in std::mem::take(&mut socket.backlog) {
            if let Some(end) = self.handle(message) {
                return end;
            }
        }
        loop {
            let ping_at = last_sent + self.options.keep_alive_interval;
            let dead_at = last_received + self.options.server_timeout;
            tokio::select! {
                incoming = socket.ws.next() => {
                    let text = match incoming {
                        Some(Ok(Message::Text(text))) => text.as_str().to_owned(),
                        Some(Ok(Message::Binary(bytes))) => match String::from_utf8(bytes.to_vec()) {
                            Ok(text) => text,
                            Err(_) => {
                                tracing::warn!("ignoring a binary frame that is not UTF-8");
                                continue;
                            }
                        },
                        Some(Ok(Message::Close(frame))) => {
                            return SessionEnd::Lost(format!("server closed the socket{}", close_reason(frame)));
                        }
                        Some(Ok(_)) => {
                            last_received = Instant::now();
                            continue;
                        }
                        Some(Err(err)) => return SessionEnd::Lost(err.to_string()),
                        None => return SessionEnd::Lost("the socket closed".into()),
                    };
                    last_received = Instant::now();
                    for record in socket.buffer.push(&text) {
                        match protocol::parse_message(&record) {
                            Ok(message) => {
                                if let Some(end) = self.handle(message) {
                                    return end;
                                }
                            }
                            Err(error) => tracing::warn!(%error, "ignoring a malformed hub message"),
                        }
                    }
                }
                command = self.commands.recv() => match command {
                    Some(Command::Invoke { target, arguments, update, reply }) => {
                        if let Err(cause) = self.send_invocation(socket, target, arguments, update, Some(reply)).await {
                            return SessionEnd::Lost(cause);
                        }
                        last_sent = Instant::now();
                    }
                    Some(Command::Close { done }) => {
                        close_socket(socket).await;
                        return SessionEnd::Closed(Some(done));
                    }
                    None => {
                        close_socket(socket).await;
                        return SessionEnd::Closed(None);
                    }
                },
                _ = sleep_until(ping_at) => {
                    if let Err(err) = socket.ws.send(Message::text(protocol::PING)).await {
                        return SessionEnd::Lost(format!("keep-alive failed: {err}"));
                    }
                    last_sent = Instant::now();
                }
                _ = sleep_until(dead_at) => {
                    return SessionEnd::Lost(format!(
                        "no message from the server within {:?}",
                        self.options.server_timeout
                    ));
                }
            }
        }
    }

    fn handle(&mut self, message: HubMessage) -> Option<SessionEnd> {
        match message {
            HubMessage::Invocation { target, arguments } => {
                for event in E::from_invocation(target, arguments) {
                    // No receivers is fine; lagging receivers see `Lagged` on their side.
                    let _ = self.events.send(event);
                }
                None
            }
            HubMessage::Completion {
                invocation_id,
                result,
                error,
            } => {
                self.complete(&invocation_id, result, error);
                None
            }
            HubMessage::Close {
                error,
                allow_reconnect,
            } => Some(SessionEnd::ServerClosed {
                error,
                allow_reconnect,
            }),
            HubMessage::Ping | HubMessage::Other => None,
        }
    }

    fn complete(&mut self, invocation_id: &str, result: Value, error: Option<String>) {
        let Some(pending) = self.pending.remove(invocation_id) else {
            tracing::debug!(invocation_id, "completion for an unknown invocation");
            return;
        };
        match error {
            Some(message) => match pending.reply {
                Some(reply) => {
                    let _ = reply.send(Err(RealtimeError::Invocation {
                        target: pending.target,
                        message,
                    }));
                }
                None => self.emit(ConnectionEvent::ResubscribeFailed {
                    method: pending.target,
                    error: message,
                }),
            },
            None => {
                match pending.update {
                    RegistryUpdate::Add(subscription) => {
                        if !self.subscriptions.contains(&subscription) {
                            self.subscriptions.push(subscription);
                        }
                    }
                    RegistryUpdate::Remove(subscription) => {
                        self.subscriptions.retain(|s| *s != subscription)
                    }
                    RegistryUpdate::None => {}
                }
                if let Some(reply) = pending.reply {
                    let _ = reply.send(Ok(result));
                }
            }
        }
    }

    async fn send_invocation(
        &mut self,
        socket: &mut Socket,
        target: String,
        arguments: Vec<Value>,
        update: RegistryUpdate,
        reply: Option<Reply>,
    ) -> Result<(), String> {
        self.next_invocation_id += 1;
        let id = self.next_invocation_id.to_string();
        let frame = protocol::encode_invocation(&id, &target, &arguments);
        self.pending.insert(
            id.clone(),
            Pending {
                target,
                update,
                reply,
            },
        );
        if let Err(err) = socket.ws.send(Message::text(frame)).await {
            if let Some(Pending {
                target,
                reply: Some(reply),
                ..
            }) = self.pending.remove(&id)
            {
                let _ = reply.send(Err(RealtimeError::ConnectionLost { target }));
            }
            return Err(format!("sending an invocation failed: {err}"));
        }
        Ok(())
    }

    async fn replay_subscriptions(&mut self, socket: &mut Socket) -> Result<(), String> {
        for subscription in self.subscriptions.clone() {
            tracing::debug!(method = subscription.method, "replaying subscription");
            self.send_invocation(
                socket,
                subscription.method.to_owned(),
                subscription.arguments,
                RegistryUpdate::None,
                None,
            )
            .await?;
        }
        Ok(())
    }

    async fn reconnect(&mut self, mut cause: String) -> Reconnect {
        let mut attempt: u32 = 0;
        loop {
            attempt += 1;
            if self
                .options
                .reconnect
                .max_attempts
                .is_some_and(|max| attempt > max)
            {
                let tries = attempt - 1;
                return Reconnect::GaveUp(format!(
                    "gave up after {tries} reconnect attempt(s): {cause}"
                ));
            }
            self.emit(ConnectionEvent::Reconnecting {
                attempt,
                cause: cause.clone(),
            });

            let deadline = Instant::now() + self.options.reconnect.delay(attempt);
            loop {
                tokio::select! {
                    _ = sleep_until(deadline) => break,
                    command = self.commands.recv() => match command {
                        Some(Command::Invoke { reply, .. }) => {
                            let _ = reply.send(Err(RealtimeError::NotConnected));
                        }
                        Some(Command::Close { done }) => return Reconnect::Closed(Some(done)),
                        None => return Reconnect::Closed(None),
                    },
                }
            }

            let (url, tokens, handshake_timeout) = (
                self.url.clone(),
                self.tokens.clone(),
                self.options.handshake_timeout,
            );
            let connect = async move { establish(&url, &tokens, handshake_timeout).await };
            tokio::pin!(connect);
            let result = loop {
                tokio::select! {
                    result = &mut connect => break result,
                    command = self.commands.recv() => match command {
                        Some(Command::Invoke { reply, .. }) => {
                            let _ = reply.send(Err(RealtimeError::NotConnected));
                        }
                        Some(Command::Close { done }) => return Reconnect::Closed(Some(done)),
                        None => return Reconnect::Closed(None),
                    },
                }
            };
            match result {
                Ok(socket) => return Reconnect::Connected(Box::new(socket)),
                Err(err) => {
                    tracing::debug!(attempt, error = %err, "reconnect attempt failed");
                    cause = err.to_string();
                }
            }
        }
    }

    fn fail_pending(&mut self, error: impl Fn(String) -> RealtimeError) {
        for (_, pending) in self.pending.drain() {
            if let Some(reply) = pending.reply {
                let _ = reply.send(Err(error(pending.target)));
            }
        }
    }

    fn finish(mut self, cause: Option<String>, done: Option<oneshot::Sender<()>>) {
        self.fail_pending(|_| RealtimeError::Closed);
        self.commands.close();
        while let Ok(command) = self.commands.try_recv() {
            match command {
                Command::Invoke { reply, .. } => {
                    let _ = reply.send(Err(RealtimeError::Closed));
                }
                Command::Close { done } => {
                    let _ = done.send(());
                }
            }
        }
        if let Some(cause) = &cause {
            tracing::warn!(hub = %hub_label(&self.url), %cause, "hub connection closed");
        }
        let _ = self.state.send(ConnectionState::Closed);
        self.emit(ConnectionEvent::Closed { cause });
        if let Some(done) = done {
            let _ = done.send(());
        }
        // Dropping `self` drops the only event sender, which ends every receiver's stream.
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn websocket_urls() {
        let hub = Url::parse("https://rtc.topstepx.com/hubs/user").unwrap();
        let url = websocket_url(&hub, "a b+c").unwrap();
        assert_eq!(
            url.as_str(),
            "wss://rtc.topstepx.com/hubs/user?access_token=a+b%2Bc"
        );
        let local = Url::parse("http://127.0.0.1:5000/hubs/market?x=1").unwrap();
        assert_eq!(
            websocket_url(&local, "t").unwrap().as_str(),
            "ws://127.0.0.1:5000/hubs/market?x=1&access_token=t"
        );
        let stale = Url::parse("https://host/hubs/user?access_token=old&x=1").unwrap();
        assert_eq!(
            websocket_url(&stale, "new").unwrap().as_str(),
            "wss://host/hubs/user?x=1&access_token=new"
        );
        assert!(websocket_url(&Url::parse("ftp://host/hub").unwrap(), "t").is_err());
        assert_eq!(hub_label(&url), "wss://rtc.topstepx.com/hubs/user");
    }
}
