package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// hubState is the lifecycle state of an engine.
type hubState int

const (
	stateIdle         hubState = iota // not connected yet, or the first connect failed
	stateConnecting                   // Connect in progress
	stateConnected                    // connected and usable
	stateReconnecting                 // connection lost; reconnecting and replaying
	stateClosed                       // closed for good
)

// dispatchFunc turns a server invocation into events.
type dispatchFunc func(target string, args []json.RawMessage) []Event

// subscription is a registry entry, replayed after every reconnect.
type subscription struct {
	method string
	args   []any
	key    string
}

func subscriptionKey(method string, args []any) string {
	b, _ := json.Marshal(args)
	return method + "\x00" + string(b)
}

// engine implements a SignalR JSON hub client with keepalive, server timeout,
// automatic reconnect and subscription replay. UserHub and MarketHub wrap it.
type engine struct {
	name     string
	cfg      config
	tokens   TokenProvider
	log      *slog.Logger
	dispatch dispatchFunc

	events       chan Event
	emitMu       sync.Mutex // serializes producers; guards eventsClosed
	eventsClosed bool
	dropped      atomic.Uint64

	ctx    context.Context // canceled by Close and on termination
	cancel context.CancelFunc

	mu     sync.Mutex // guards state, cur and subs
	state  hubState
	cur    *transport
	subs   []subscription
	nextID atomic.Uint64

	// subMu serializes subscribe and unsubscribe (invocation plus registry
	// update) with the replay after a reconnect, so that a subscription
	// that succeeded just before a connection loss is never missed by the
	// replay.
	subMu sync.Mutex

	closeOnce  sync.Once
	finishOnce sync.Once
	closing    chan struct{} // closed when Close starts
	done       chan struct{} // closed when the engine terminated
	wg         sync.WaitGroup
}

func newEngine(name string, cfg config, tokens TokenProvider, dispatch dispatchFunc) *engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &engine{
		name:     name,
		cfg:      cfg,
		tokens:   tokens,
		log:      cfg.logger.With("hub", name),
		dispatch: dispatch,
		events:   make(chan Event, cfg.eventBuffer),
		ctx:      ctx,
		cancel:   cancel,
		closing:  make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// transport is one WebSocket connection.
type transport struct {
	ws       *websocket.Conn
	ctx      context.Context // canceled on teardown
	cancel   context.CancelFunc
	lastSent atomic.Int64 // unix nanoseconds of the last message sent
	splitter recordSplitter
	leftover [][]byte // records that shared the handshake response's frame

	pendingMu sync.Mutex
	pending   map[string]chan completion
	lost      bool

	done           chan struct{} // closed when the read loop exits
	cause          error         // why the read loop exited; set before done closes
	allowReconnect bool          // set before done closes
}

type completion struct {
	result json.RawMessage
	err    error
}

func (t *transport) register(id string) (chan completion, bool) {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	if t.lost {
		return nil, false
	}
	ch := make(chan completion, 1)
	t.pending[id] = ch
	return ch, true
}

func (t *transport) unregister(id string) {
	t.pendingMu.Lock()
	delete(t.pending, id)
	t.pendingMu.Unlock()
}

func (t *transport) complete(id string, c completion) {
	t.pendingMu.Lock()
	ch, ok := t.pending[id]
	delete(t.pending, id)
	t.pendingMu.Unlock()
	if ok {
		ch <- c
	}
}

// failAll fails every pending invocation with err and refuses new ones.
func (t *transport) failAll(err error) {
	t.pendingMu.Lock()
	pending := t.pending
	t.pending = make(map[string]chan completion)
	t.lost = true
	t.pendingMu.Unlock()
	for _, ch := range pending {
		ch <- completion{err: err}
	}
}

func (t *transport) write(msg []byte) error { return t.writeWithin(msg, writeTimeout) }

func (t *transport) writeWithin(msg []byte, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(t.ctx, timeout)
	defer cancel()
	if err := t.ws.Write(ctx, websocket.MessageText, msg); err != nil {
		return err
	}
	t.lastSent.Store(time.Now().UnixNano())
	return nil
}

func (e *engine) isClosing() bool {
	select {
	case <-e.closing:
		return true
	default:
		return false
	}
}

// connect dials, performs the handshake and starts the connection loops.
func (e *engine) connect(ctx context.Context) error {
	if e.tokens == nil {
		return errors.New("realtime: no token provider")
	}
	e.mu.Lock()
	switch e.state {
	case stateClosed:
		e.mu.Unlock()
		return ErrClosed
	case stateConnecting, stateConnected, stateReconnecting:
		e.mu.Unlock()
		return ErrAlreadyConnected
	}
	e.state = stateConnecting
	e.mu.Unlock()

	dctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	t, err := e.dial(dctx)
	stop()
	cancel()

	e.mu.Lock()
	if e.state != stateConnecting { // closed meanwhile
		e.mu.Unlock()
		if t != nil {
			_ = t.ws.CloseNow()
			t.cancel()
		}
		return ErrClosed
	}
	if err != nil {
		e.state = stateIdle
		e.mu.Unlock()
		return err
	}
	e.state = stateConnected
	e.cur = t
	e.wg.Add(3) // run, readLoop, keepAlive
	e.mu.Unlock()

	e.log.Debug("realtime: connected")
	e.emit(&StateEvent{State: StateConnected})
	go e.readLoop(t)
	go e.keepAlive(t)
	go e.run(t)
	return nil
}

// dial fetches a fresh token, opens the WebSocket and completes the SignalR
// handshake.
func (e *engine) dial(ctx context.Context) (*transport, error) {
	token, err := e.tokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("realtime: token provider: %w", err)
	}
	if token == "" {
		return nil, errors.New("realtime: token provider returned an empty token")
	}
	target, err := hubURL(e.cfg.url, token)
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithTimeout(ctx, e.cfg.handshakeTimeout)
	defer cancel()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	ws, resp, err := websocket.Dial(hctx, target, &websocket.DialOptions{HTTPClient: e.cfg.httpClient, HTTPHeader: header})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			// Report the rejected token so that the next attempt does not
			// reuse it (contract section 11, rejected tokens).
			e.log.Warn("realtime: token rejected (HTTP 401) during the WebSocket upgrade")
			if e.cfg.tokenRejected != nil {
				e.cfg.tokenRejected(token)
			}
		}
		return nil, sanitizeDialError(err, resp, token)
	}
	ws.SetReadLimit(e.cfg.readLimit)
	tctx, tcancel := context.WithCancel(e.ctx)
	t := &transport{
		ws:       ws,
		ctx:      tctx,
		cancel:   tcancel,
		splitter: recordSplitter{limit: e.cfg.readLimit},
		pending:  make(map[string]chan completion),
		done:     make(chan struct{}),
	}
	if err := e.handshake(hctx, t); err != nil {
		_ = ws.CloseNow()
		tcancel()
		return nil, err
	}
	return t, nil
}

func (e *engine) handshake(ctx context.Context, t *transport) error {
	if err := t.ws.Write(ctx, websocket.MessageText, handshakeRequest); err != nil {
		return &HandshakeError{Message: fmt.Sprintf("send request: %v", err)}
	}
	t.lastSent.Store(time.Now().UnixNano())
	for {
		_, data, err := t.ws.Read(ctx)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return &HandshakeError{Message: fmt.Sprintf("no response within %s", e.cfg.handshakeTimeout)}
			}
			return &HandshakeError{Message: fmt.Sprintf("read response: %v", err)}
		}
		records, err := t.splitter.feed(data)
		if err != nil {
			return &HandshakeError{Message: err.Error()}
		}
		if len(records) == 0 {
			continue // incomplete; wait for the rest
		}
		var resp struct {
			Error *string `json:"error"`
		}
		if err := json.Unmarshal(records[0], &resp); err != nil {
			return &HandshakeError{Message: fmt.Sprintf("invalid response: %v", err)}
		}
		if resp.Error != nil {
			return &HandshakeError{Message: *resp.Error}
		}
		t.leftover = records[1:]
		return nil
	}
}

// readLoop reads frames until the connection ends and records the cause.
func (e *engine) readLoop(t *transport) {
	defer e.wg.Done()
	defer close(t.done)
	for _, rec := range t.leftover {
		if !e.handleRecord(t, rec) {
			return
		}
	}
	t.leftover = nil
	for {
		rctx, cancel := context.WithTimeout(t.ctx, e.cfg.serverTimeout)
		_, data, err := t.ws.Read(rctx)
		timedOut := errors.Is(rctx.Err(), context.DeadlineExceeded)
		cancel()
		if err != nil {
			if timedOut && t.ctx.Err() == nil {
				t.cause = ErrServerTimeout
			} else {
				t.cause = lostError(err)
			}
			t.allowReconnect = true
			return
		}
		records, err := t.splitter.feed(data)
		for _, rec := range records {
			if !e.handleRecord(t, rec) {
				return
			}
		}
		if err != nil {
			t.cause, t.allowReconnect = lostError(err), true
			return
		}
	}
}

// handleRecord processes one message and reports whether reading continues.
func (e *engine) handleRecord(t *transport, rec []byte) bool {
	var m hubMessage
	if err := json.Unmarshal(rec, &m); err != nil {
		e.log.Debug("realtime: ignoring malformed message", "error", err)
		return true
	}
	switch m.Type {
	case msgInvocation:
		if m.Target == "" {
			return true
		}
		for _, ev := range e.dispatch(m.Target, m.Arguments) {
			e.emit(ev)
		}
	case msgCompletion:
		if m.InvocationID == nil {
			return true
		}
		c := completion{result: m.Result}
		if m.Error != nil {
			c.err = &HubError{Message: *m.Error}
		}
		t.complete(*m.InvocationID, c)
	case msgClose:
		ce := &ServerCloseError{AllowReconnect: m.AllowReconnect}
		if m.Error != nil {
			ce.Message = *m.Error
		}
		t.cause, t.allowReconnect = ce, m.AllowReconnect
		return false
	}
	// Pings, stream messages and unknown types need no action.
	return true
}

// keepAlive sends a ping whenever the client has been silent for the
// keepalive interval.
func (e *engine) keepAlive(t *transport) {
	defer e.wg.Done()
	interval := e.cfg.keepAlive
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-t.done:
			return
		case <-timer.C:
		}
		idle := time.Since(time.Unix(0, t.lastSent.Load()))
		if idle < interval {
			timer.Reset(interval - idle)
			continue
		}
		if err := t.write(pingMessage); err != nil {
			e.log.Debug("realtime: keepalive ping failed", "error", err)
			return // the read loop notices the broken connection
		}
		timer.Reset(interval)
	}
}

// run supervises the connection: after each loss it tears the transport
// down and reconnects, or terminates the engine.
func (e *engine) run(t *transport) {
	defer e.wg.Done()
	for {
		<-t.done
		cause, allow := t.cause, t.allowReconnect
		t.failAll(lostError(cause))
		t.cancel()
		_ = t.ws.CloseNow()
		if e.isClosing() {
			return // Close finishes the engine
		}
		e.mu.Lock()
		if e.state == stateClosed {
			e.mu.Unlock()
			return
		}
		if !allow || !e.cfg.reconnect.enabled() {
			e.state = stateClosed
			e.mu.Unlock()
			e.log.Info("realtime: connection ended", "cause", cause)
			e.finish(cause)
			return
		}
		e.state = stateReconnecting
		e.mu.Unlock()
		e.log.Warn("realtime: connection lost; reconnecting", "cause", cause)
		next := e.reconnect(cause)
		if next == nil {
			return
		}
		t = next
	}
}

// reconnect retries per the policy. It returns the new transport, or nil when
// the engine was closed or gave up (in which case it has been finished).
func (e *engine) reconnect(cause error) *transport {
	policy := e.cfg.reconnect
	for attempt := 1; ; attempt++ {
		if policy.MaxAttempts >= 0 && attempt > policy.MaxAttempts {
			e.mu.Lock()
			closed := e.state == stateClosed
			e.state = stateClosed
			e.mu.Unlock()
			if !closed && !e.isClosing() {
				e.finish(fmt.Errorf("realtime: gave up after %d reconnect attempts: %w", policy.MaxAttempts, cause))
			}
			return nil
		}
		e.emit(&StateEvent{State: StateReconnecting, Attempt: attempt, Err: cause})
		if !e.wait(policy.delay(attempt)) {
			return nil
		}
		t, err := e.dial(e.ctx)
		if err != nil {
			if e.isClosing() {
				return nil
			}
			e.log.Warn("realtime: reconnect attempt failed", "attempt", attempt, "error", err)
			cause = err
			continue
		}
		e.mu.Lock()
		if e.state == stateClosed {
			e.mu.Unlock()
			_ = t.ws.CloseNow()
			t.cancel()
			return nil
		}
		e.cur = t
		e.wg.Add(2) // readLoop, keepAlive
		e.mu.Unlock()
		go e.readLoop(t)
		go e.keepAlive(t)

		e.replay(t)

		e.mu.Lock()
		if e.state == stateClosed {
			e.mu.Unlock()
			return t
		}
		select {
		case <-t.done:
			// Lost again during the replay; the caller handles it.
			e.mu.Unlock()
			return t
		default:
		}
		e.state = stateConnected
		e.mu.Unlock()
		e.log.Info("realtime: reconnected", "attempt", attempt)
		e.emit(&StateEvent{State: StateReconnected, Attempt: attempt})
		return t
	}
}

// wait sleeps for d and reports false if the engine is closing.
func (e *engine) wait(d time.Duration) bool {
	if d <= 0 {
		return !e.isClosing()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-e.closing:
		return false
	case <-timer.C:
		return true
	}
}

// replay re-invokes the recorded subscriptions in insertion order. A failure
// is reported as a *SubscriptionErrorEvent.
func (e *engine) replay(t *transport) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	e.mu.Lock()
	subs := append([]subscription(nil), e.subs...)
	e.mu.Unlock()
	for _, s := range subs {
		_, err := e.invokeOn(e.ctx, t, s.method, s.args)
		if err == nil {
			continue
		}
		if errors.Is(err, ErrConnectionLost) || errors.Is(err, ErrClosed) || e.isClosing() {
			return // the connection is gone; the next reconnect replays again
		}
		e.log.Warn("realtime: resubscribe failed", "method", s.method, "error", err)
		e.emit(&SubscriptionErrorEvent{Method: s.method, Arguments: append([]any(nil), s.args...), Err: err})
	}
}

// invoke calls a hub method on the current connection. It fails fast with
// ErrNotConnected while the hub is not connected.
func (e *engine) invoke(ctx context.Context, method string, args []any) (json.RawMessage, error) {
	e.mu.Lock()
	state, t := e.state, e.cur
	e.mu.Unlock()
	switch state {
	case stateConnected:
		return e.invokeOn(ctx, t, method, args)
	case stateClosed:
		return nil, ErrClosed
	default:
		return nil, ErrNotConnected
	}
}

func (e *engine) invokeOn(ctx context.Context, t *transport, method string, args []any) (json.RawMessage, error) {
	id := strconv.FormatUint(e.nextID.Add(1), 10)
	msg, err := encodeInvocation(id, method, args)
	if err != nil {
		return nil, err
	}
	ch, ok := t.register(id)
	if !ok {
		return nil, e.lostOrClosed(nil)
	}
	if err := t.write(msg); err != nil {
		t.unregister(id)
		return nil, e.lostOrClosed(err)
	}
	timer := time.NewTimer(e.cfg.invocationTimeout)
	defer timer.Stop()
	select {
	case c := <-ch:
		if c.err != nil {
			var hubErr *HubError
			if errors.As(c.err, &hubErr) {
				hubErr.Method = method
			}
			return nil, c.err
		}
		return c.result, nil
	case <-ctx.Done():
		t.unregister(id)
		return nil, ctx.Err()
	case <-timer.C:
		t.unregister(id)
		return nil, fmt.Errorf("%w: %s after %s", ErrInvocationTimeout, method, e.cfg.invocationTimeout)
	}
}

func (e *engine) lostOrClosed(cause error) error {
	if e.isClosing() {
		return ErrClosed
	}
	return lostError(cause)
}

// ready fails fast with ErrNotConnected or ErrClosed unless the hub is
// connected.
func (e *engine) ready() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch e.state {
	case stateConnected:
		return nil
	case stateClosed:
		return ErrClosed
	default:
		return ErrNotConnected
	}
}

// subscribe invokes a subscribe method and records it for replay.
func (e *engine) subscribe(ctx context.Context, method string, args ...any) error {
	if err := e.ready(); err != nil {
		return err
	}
	e.subMu.Lock()
	defer e.subMu.Unlock()
	if _, err := e.invoke(ctx, method, args); err != nil {
		return err
	}
	key := subscriptionKey(method, args)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.subs {
		if s.key == key {
			return nil
		}
	}
	e.subs = append(e.subs, subscription{method: method, args: append([]any(nil), args...), key: key})
	return nil
}

// unsubscribe invokes an unsubscribe method and removes the matching
// subscription from the registry.
func (e *engine) unsubscribe(ctx context.Context, method, subscribeMethod string, args ...any) error {
	if err := e.ready(); err != nil {
		return err
	}
	e.subMu.Lock()
	defer e.subMu.Unlock()
	if _, err := e.invoke(ctx, method, args); err != nil {
		return err
	}
	key := subscriptionKey(subscribeMethod, args)
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, s := range e.subs {
		if s.key == key {
			e.subs = append(e.subs[:i], e.subs[i+1:]...)
			break
		}
	}
	return nil
}

// emit delivers ev without ever blocking. When the buffer is full, the
// overflow policy decides which event is dropped; state events are always
// enqueued, evicting the oldest buffered event if needed.
func (e *engine) emit(ev Event) {
	e.emitMu.Lock()
	defer e.emitMu.Unlock()
	if e.eventsClosed {
		return
	}
	select {
	case e.events <- ev:
		return
	default:
	}
	if _, isState := ev.(*StateEvent); !isState && e.cfg.overflow == DropNewest {
		e.recordDrop()
		return
	}
	for {
		select {
		case <-e.events:
			e.recordDrop()
		default:
		}
		select {
		case e.events <- ev:
			return
		default:
		}
	}
}

func (e *engine) recordDrop() {
	n := e.dropped.Add(1)
	if n == 1 || n%1000 == 0 {
		e.log.Warn("realtime: event consumer is falling behind; events dropped", "dropped", n)
	}
}

// finish terminates the engine: it emits the closed event with cause and
// closes the events channel.
func (e *engine) finish(cause error) {
	e.finishOnce.Do(func() {
		e.mu.Lock()
		e.state = stateClosed
		e.mu.Unlock()
		e.emit(&StateEvent{State: StateClosed, Err: cause})
		e.emitMu.Lock()
		e.eventsClosed = true
		close(e.events)
		e.emitMu.Unlock()
		e.cancel()
		close(e.done)
	})
}

// close stops reconnecting, sends a SignalR close message, closes the
// socket, fails pending invocations with ErrClosed, waits for every
// goroutine and ends the event stream. It is idempotent.
func (e *engine) close() error {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.state = stateClosed
		t := e.cur
		close(e.closing)
		e.mu.Unlock()
		if t != nil {
			t.failAll(ErrClosed)
			select {
			case <-t.done:
			default:
				if err := t.writeWithin(closeMessage, closeTimeout); err != nil {
					e.log.Debug("realtime: sending close message failed", "error", err)
				}
				_ = t.ws.Close(websocket.StatusNormalClosure, "")
			}
			t.cancel()
		}
		e.cancel()
		e.wg.Wait()
		e.finish(nil)
	})
	return nil
}
