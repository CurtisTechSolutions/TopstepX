package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
	"github.com/coder/websocket"
)

// Realtime behavior against a local SignalR server (contract section 12.4).

func TestHandshakeAndTokenTransport(t *testing.T) {
	s := newTestServer(t)
	const token = "tok en/+&=?"
	hub := NewUserHub(func(context.Context) (string, error) { return token, nil }, fastOptions(s.url)...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	if c.token != token || c.auth != "Bearer "+token {
		t.Fatalf("server saw access_token %q and Authorization %q", c.token, c.auth)
	}
	if strings.Contains(c.query, "tok en") || !strings.Contains(c.query, "access_token=tok+en%2F%2B%26%3D%3F") {
		t.Fatalf("access_token must be URL-encoded: %q", c.query)
	}
	if ev := waitEvent[*StateEvent](t, hub.Events(), nil); ev.State != StateConnected || ev.Err != nil {
		t.Fatalf("first event = %+v, want connected", ev)
	}
	if err := hub.Connect(context.Background()); !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("second Connect: %v", err)
	}
}

func TestHandshakeErrors(t *testing.T) {
	s := newTestServer(t)
	tokens := func(context.Context) (string, error) { return "secret-token", nil }

	s.set(func(s *testServer) {
		s.handshake = func(int) string { return "{\"error\":\"Requested protocol 'json' is not available.\"}\x1e" }
	})
	hub := NewUserHub(tokens, fastOptions(s.url)...)
	var hsErr *HandshakeError
	if err := hub.Connect(context.Background()); !errors.As(err, &hsErr) || !strings.Contains(hsErr.Message, "not available") {
		t.Fatalf("Connect = %v, want *HandshakeError", err)
	}
	hub.Close()

	s.set(func(s *testServer) { s.handshake = func(int) string { return "" } }) // never answers
	hub = NewUserHub(tokens, append(fastOptions(s.url), WithHandshakeTimeout(100*time.Millisecond))...)
	start := time.Now()
	if err := hub.Connect(context.Background()); !errors.As(err, &hsErr) || time.Since(start) > 2*time.Second {
		t.Fatalf("Connect = %v after %v, want a handshake timeout", err, time.Since(start))
	}
	hub.Close()

	s.set(func(s *testServer) {
		s.handshake = nil
		s.reject = func(int, string) int { return http.StatusUnauthorized }
	})
	hub = NewUserHub(tokens, fastOptions(s.url)...)
	err := hub.Connect(context.Background())
	var dialErr *DialError
	if !errors.As(err, &dialErr) || dialErr.StatusCode != http.StatusUnauthorized || !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Connect = %v, want *DialError 401 matching ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("dial error leaks the token: %v", err)
	}
	hub.Close()

	failing := NewUserHub(func(context.Context) (string, error) { return "", errors.New("no session") }, fastOptions(s.url)...)
	if err := failing.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("Connect with a failing provider = %v", err)
	}
	failing.Close()
	if err := NewUserHub(nil).Connect(context.Background()); err == nil {
		t.Fatal("Connect without a provider succeeded")
	}
}

// Messages sharing a frame with the handshake response are processed.
func TestHandshakeFrameCarriesMessages(t *testing.T) {
	s := newTestServer(t)
	s.set(func(s *testServer) {
		s.handshake = func(int) string {
			return "{}\x1e" + `{"type":1,"target":"GatewayUserAccount","arguments":[{"id":7,"name":"A","balance":1.5}]}` + "\x1e" + `{"type":6}` + "\x1e"
		}
	})
	hub := NewUserHub(func(context.Context) (string, error) { return "t", nil }, fastOptions(s.url)...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, hub.Events()); ev.(*StateEvent).State != StateConnected {
		t.Fatalf("first event %T", ev)
	}
	acct := waitEvent[*AccountEvent](t, hub.Events(), nil)
	if acct.Account.ID != 7 || acct.Account.Balance.String() != "1.5" {
		t.Fatalf("account = %+v", acct.Account)
	}
}

func TestInvokeAndCompletion(t *testing.T) {
	s := newTestServer(t)
	s.set(func(s *testServer) {
		s.invoke = func(c *serverConn, m clientMsg) {
			switch m.Target {
			case "SubscribeTrades":
				c.fail(m.InvocationID, "account not found")
			case "Echo":
				c.complete(m.InvocationID, `{"echo":`+string(m.Arguments[0])+`}`)
			default:
				c.complete(m.InvocationID, "")
			}
		}
	})
	hub := NewUserHub(func(context.Context) (string, error) { return "t", nil }, fastOptions(s.url)...)
	defer hub.Close()
	ctx := context.Background()
	if err := hub.SubscribeOrders(ctx, 1); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("before Connect: %v, want ErrNotConnected", err)
	}
	if err := hub.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)

	if err := hub.SubscribeOrders(ctx, 123); err != nil {
		t.Fatal(err)
	}
	m := c.waitMsg(t, isInvocation("SubscribeOrders"))
	if m.InvocationID == "" || len(m.Arguments) != 1 || string(m.Arguments[0]) != "123" {
		t.Fatalf("invocation = %s", m.Raw)
	}
	if err := hub.SubscribeAccounts(ctx); err != nil {
		t.Fatal(err)
	}
	if m2 := c.waitMsg(t, isInvocation("SubscribeAccounts")); m2.InvocationID == m.InvocationID || !strings.Contains(m2.Raw, `"arguments":[]`) {
		t.Fatalf("invocation = %s", m2.Raw)
	}

	err := hub.SubscribeTrades(ctx, 9)
	var hubErr *HubError
	if !errors.As(err, &hubErr) || hubErr.Method != "SubscribeTrades" || hubErr.Message != "account not found" {
		t.Fatalf("SubscribeTrades = %v, want *HubError", err)
	}
	result, err := hub.Invoke(ctx, "Echo", map[string]int{"n": 1})
	if err != nil || string(result) != `{"echo":{"n":1}}` {
		t.Fatalf("Invoke = %s, %v", result, err)
	}
	hub.e.mu.Lock()
	subs := len(hub.e.subs)
	hub.e.mu.Unlock()
	if subs != 2 {
		t.Fatalf("registry has %d entries, want 2 (failed subscriptions are not recorded)", subs)
	}
}

func TestInvocationTimeoutAndContext(t *testing.T) {
	s := newTestServer(t)
	s.set(func(s *testServer) { s.invoke = func(*serverConn, clientMsg) {} }) // never completes
	hub := NewMarketHub(func(context.Context) (string, error) { return "t", nil },
		append(fastOptions(s.url), WithInvocationTimeout(100*time.Millisecond))...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := hub.SubscribeContractQuotes(context.Background(), "C"); !errors.Is(err, ErrInvocationTimeout) {
		t.Fatalf("err = %v, want ErrInvocationTimeout", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := hub.SubscribeContractTrades(ctx, "C"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context error", err)
	}
	// The connection survives both.
	s.set(func(s *testServer) { s.invoke = nil })
	if err := hub.SubscribeContractMarketDepth(context.Background(), "C"); err != nil {
		t.Fatal(err)
	}
}

func TestUserEventForms(t *testing.T) {
	s := newTestServer(t)
	hub := NewUserHub(func(context.Context) (string, error) { return "t", nil }, fastOptions(s.url)...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	order := func(id int, extra string) string {
		return fmt.Sprintf(`{"id":%d,"accountId":2,"contractId":"C","symbolId":"S","status":1,"type":1,"side":0,"size":1,"limitPrice":6000.25,"fillVolume":"2"%s}`, id, extra)
	}
	// Bare, envelope and array forms, several records in one frame.
	c.send(t,
		`{"type":1,"target":"GatewayUserOrder","arguments":[`+order(1, "")+`]}`,
		`{"type":1,"target":"GatewayUserOrder","arguments":[{"action":1,"data":`+order(2, `,"creationTimestamp":"2025-07-01T14:00:00.1234567"`)+`}]}`,
		`{"type":1,"target":"GatewayUserOrder","arguments":[[{"action":0,"data":`+order(3, "")+`},`+order(4, "")+`]]}`,
	)
	// A record split across two frames, sent as binary.
	rec := `{"type":1,"target":"GatewayUserPosition","arguments":[{"id":5,"accountId":2,"contractId":"C","type":1,"size":3,"averagePrice":6000.5}]}` + "\x1e"
	if err := c.ws.Write(context.Background(), websocket.MessageBinary, []byte(rec[:20])); err != nil {
		t.Fatal(err)
	}
	if err := c.ws.Write(context.Background(), websocket.MessageBinary, []byte(rec[20:])); err != nil {
		t.Fatal(err)
	}
	c.send(t,
		`{"type":1,"target":"GatewayUserTrade","arguments":[{"id":6,"accountId":2,"contractId":"C","price":6000,"profitAndLoss":null,"fees":1.5,"side":1,"size":1,"voided":false,"orderId":4}]}`,
		`{"type":1,"target":"GatewayUserAccount","arguments":[{"action":2,"data":[{"id":8,"canTrade":true},{"id":9}]}]}`,
	)

	wantOrders := []struct {
		id     int64
		action *int32
	}{{1, nil}, {2, topstepx.Ptr(int32(1))}, {3, topstepx.Ptr(int32(0))}, {4, nil}}
	for _, want := range wantOrders {
		ev := waitEvent[*OrderEvent](t, hub.Events(), nil)
		if ev.Order.ID != want.id || (ev.Action == nil) != (want.action == nil) || (ev.Action != nil && *ev.Action != *want.action) {
			t.Fatalf("order event = %+v (action %v), want id %d action %v", ev.Order, ev.Action, want.id, want.action)
		}
		if ev.Order.LimitPrice.String() != "6000.25" || *ev.Order.FillVolume != 2 || *ev.Order.Status != topstepx.OrderStatusOpen || len(ev.Raw) == 0 {
			t.Fatalf("order payload = %+v", ev.Order)
		}
		if want.id == 2 && !ev.Order.CreationTimestamp.Equal(time.Date(2025, 7, 1, 14, 0, 0, 123456700, time.UTC)) {
			t.Fatalf("timestamp = %v", ev.Order.CreationTimestamp)
		}
	}
	pos := waitEvent[*PositionEvent](t, hub.Events(), nil)
	if pos.Position.ID != 5 || *pos.Position.Type != topstepx.PositionTypeLong || pos.Position.AveragePrice.String() != "6000.5" {
		t.Fatalf("position = %+v", pos.Position)
	}
	trade := waitEvent[*UserTradeEvent](t, hub.Events(), nil)
	if trade.Trade.ID != 6 || trade.Trade.ProfitAndLoss != nil || trade.Trade.Fees.String() != "1.5" || *trade.Trade.Side != topstepx.OrderSideAsk {
		t.Fatalf("trade = %+v", trade.Trade)
	}
	for _, id := range []int32{8, 9} {
		acct := waitEvent[*AccountEvent](t, hub.Events(), nil)
		if acct.Account.ID != id || acct.Action == nil || *acct.Action != 2 {
			t.Fatalf("account = %+v", acct)
		}
	}
}

func TestMarketEvents(t *testing.T) {
	s := newTestServer(t)
	hub := NewMarketHub(func(context.Context) (string, error) { return "t", nil }, fastOptions(s.url)...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	c.send(t,
		`{"type":1,"target":"GatewayQuote","arguments":["CON.F.US.EP.U25",{"symbol":"F.US.EP","lastPrice":6000.25,"bestBid":6000,"volume":1200.0,"lastUpdated":"2025-07-01T14:00:00Z"}]}`,
		`{"type":1,"target":"GatewayTrade","arguments":["CON.F.US.EP.U25",{"symbolId":"F.US.EP","price":6000.25,"timestamp":"2025-07-01T14:00:00Z","type":0,"volume":"3"}]}`,
		`{"type":1,"target":"GatewayTrade","arguments":["CON.F.US.EP.U25",[{"symbolId":"F.US.EP","price":6000,"type":1,"volume":1},{"symbolId":"F.US.EP","price":6000.5,"type":0,"volume":2.0}]]}`,
		`{"type":1,"target":"GatewayDepth","arguments":["CON.F.US.EP.U25",[{"timestamp":"2025-07-01T14:00:00Z","type":3,"price":6000.25,"volume":10,"currentVolume":"4"},{"type":6,"price":0,"volume":0,"currentVolume":0}]]}`,
		`{"type":1,"target":"GatewayDepth","arguments":["CON.F.US.EP.U25",{"type":11,"price":6000,"volume":1,"currentVolume":1}]}`,
	)
	q := waitEvent[*QuoteEvent](t, hub.Events(), nil)
	if q.ContractID != "CON.F.US.EP.U25" || q.Quote.Symbol != "F.US.EP" || q.Quote.LastPrice.String() != "6000.25" || *q.Quote.Volume != 1200 || q.Quote.High != nil {
		t.Fatalf("quote = %+v", q)
	}
	tr := waitEvent[*MarketTradesEvent](t, hub.Events(), nil)
	if len(tr.Trades) != 1 || *tr.Trades[0].Volume != 3 || *tr.Trades[0].Type != TradeLogTypeBuy {
		t.Fatalf("single trade = %+v", tr)
	}
	tr = waitEvent[*MarketTradesEvent](t, hub.Events(), nil)
	if len(tr.Trades) != 2 || *tr.Trades[1].Volume != 2 || tr.Trades[0].Type.String() != "Sell" {
		t.Fatalf("trade list = %+v", tr)
	}
	d := waitEvent[*DepthEvent](t, hub.Events(), nil)
	if len(d.Entries) != 2 || *d.Entries[0].Type != DomTypeBestAsk || *d.Entries[0].CurrentVolume != 4 || d.Entries[1].Type.String() != "Reset" {
		t.Fatalf("depth = %+v", d)
	}
	d = waitEvent[*DepthEvent](t, hub.Events(), nil)
	if len(d.Entries) != 1 || *d.Entries[0].Type != DomTypeFill {
		t.Fatalf("single depth = %+v", d)
	}
}

func TestUnknownTargetsAndBadPayloads(t *testing.T) {
	s := newTestServer(t)
	hub := NewUserHub(func(context.Context) (string, error) { return "t", nil }, fastOptions(s.url)...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	c.send(t,
		`{"type":1,"target":"GatewayBrandNew","arguments":[1,"two"]}`,
		`{"type":1,"target":"GatewayUserOrder","arguments":["not an object"]}`,
		`{"type":1,"target":"GatewayUserOrder","arguments":[{"id":"x"}]}`,
		`{"type":1,"target":"GatewayUserOrder","arguments":[]}`,
		`not json at all`,
		`{"type":2,"invocationId":"9","item":1}`,
		`{"type":99}`,
		`{"type":1,"target":"GatewayUserOrder","arguments":[{"id":10,"accountId":1,"contractId":"C","symbolId":"S"}]}`,
	)
	unknown := waitEvent[*UnknownEvent](t, hub.Events(), nil)
	if unknown.Target != "GatewayBrandNew" || unknown.Err != nil || len(unknown.Arguments) != 2 || string(unknown.Arguments[1]) != `"two"` {
		t.Fatalf("unknown target = %+v", unknown)
	}
	for i := 0; i < 3; i++ {
		bad := waitEvent[*UnknownEvent](t, hub.Events(), nil)
		if bad.Target != "GatewayUserOrder" || bad.Err == nil {
			t.Fatalf("bad payload = %+v", bad)
		}
	}
	// The connection is unaffected.
	if ev := waitEvent[*OrderEvent](t, hub.Events(), nil); ev.Order.ID != 10 {
		t.Fatalf("order = %+v", ev.Order)
	}
	if err := hub.SubscribeAccounts(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestKeepAlivePing(t *testing.T) {
	s := newTestServer(t)
	hub := NewUserHub(func(context.Context) (string, error) { return "t", nil },
		append(fastOptions(s.url), WithKeepAliveInterval(50*time.Millisecond))...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	start := time.Now()
	for i := 0; i < 3; i++ {
		ping := c.waitMsg(t, func(m clientMsg) bool { return m.Type == 6 })
		if ping.Raw != `{"type":6}` {
			t.Fatalf("ping = %q", ping.Raw)
		}
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("three pings within %v; the interval is 50ms", elapsed)
	}
}

func TestServerTimeoutReconnects(t *testing.T) {
	s := newTestServer(t)
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, append(fastOptions(s.url), WithServerTimeout(150*time.Millisecond), WithKeepAliveInterval(20*time.Millisecond))...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := s.nextConn(t)
	s.set(func(s *testServer) { s.pingEvery = 30 * time.Millisecond }) // later connections stay alive
	// The first connection's server never sends anything, although the client pings.
	first.waitMsg(t, func(m clientMsg) bool { return m.Type == 6 })
	ev := waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnecting))
	if !errors.Is(ev.Err, ErrServerTimeout) || ev.Attempt != 1 {
		t.Fatalf("reconnecting = %+v, want ErrServerTimeout", ev)
	}
	waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnected))
	if second := s.nextConn(t); second.token != "token-2" {
		t.Fatalf("reconnect used %q", second.token)
	}
	// Server pings keep the new connection alive.
	time.Sleep(400 * time.Millisecond)
	if s.attemptCount() != 2 {
		t.Fatalf("attempts = %d; the pinged connection must stay up", s.attemptCount())
	}
}

func TestReconnectWithFreshTokenAndReplay(t *testing.T) {
	s := newTestServer(t)
	tokens, calls := tokenSequence()
	hub := NewUserHub(tokens, fastOptions(s.url)...)
	defer hub.Close()
	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	first := s.nextConn(t)
	if first.token != "token-1" {
		t.Fatalf("first token %q", first.token)
	}
	for _, sub := range []func() error{
		func() error { return hub.SubscribeOrders(ctx, 1) },
		func() error { return hub.SubscribeTrades(ctx, 1) },
		func() error { return hub.SubscribePositions(ctx, 2) },
		func() error { return hub.SubscribeAccounts(ctx) },
		func() error { return hub.SubscribeOrders(ctx, 1) }, // duplicate
		func() error { return hub.UnsubscribePositions(ctx, 2) },
	} {
		if err := sub(); err != nil {
			t.Fatal(err)
		}
	}
	waitEvent[*StateEvent](t, hub.Events(), isState(StateConnected))

	// Hold the replay of SubscribeAccounts until the test has checked that
	// Reconnected is not emitted before the replay completes.
	release := make(chan struct{})
	s.set(func(s *testServer) {
		s.invoke = func(c *serverConn, m clientMsg) {
			if c.n == 2 && m.Target == "SubscribeAccounts" {
				go func() {
					<-release
					c.complete(m.InvocationID, "")
				}()
				return
			}
			c.complete(m.InvocationID, "")
		}
	})
	first.drop()

	ev := waitEvent[*StateEvent](t, hub.Events(), nil)
	if ev.State != StateReconnecting || ev.Attempt != 1 || !errors.Is(ev.Err, ErrConnectionLost) {
		t.Fatalf("event = %+v, want reconnecting attempt 1", ev)
	}
	second := s.nextConn(t)
	if second.token != "token-2" || second.auth != "Bearer token-2" || calls() != 2 {
		t.Fatalf("reconnect used token %q / %q (%d provider calls)", second.token, second.auth, calls())
	}
	var replayed []string
	for len(replayed) < 3 {
		m := second.waitMsg(t, func(m clientMsg) bool { return m.Type == 1 })
		args, _ := json.Marshal(m.Arguments)
		replayed = append(replayed, m.Target+string(args))
	}
	if want := []string{"SubscribeOrders[1]", "SubscribeTrades[1]", "SubscribeAccounts[]"}; fmt.Sprint(replayed) != fmt.Sprint(want) {
		t.Fatalf("replay = %v, want %v in insertion order", replayed, want)
	}
	// Invocations fail fast while the replay is still running.
	if err := hub.SubscribeOrders(ctx, 5); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("during replay: %v, want ErrNotConnected", err)
	}
	select {
	case ev := <-hub.Events():
		t.Fatalf("event %#v before the replay completed", ev)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if ev := waitEvent[*StateEvent](t, hub.Events(), nil); ev.State != StateReconnected || ev.Attempt != 1 {
		t.Fatalf("event = %+v, want reconnected", ev)
	}
	if err := hub.SubscribeOrders(ctx, 5); err != nil {
		t.Fatalf("after reconnect: %v", err)
	}
}

func TestReplayFailureIsSurfaced(t *testing.T) {
	s := newTestServer(t)
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, fastOptions(s.url)...)
	defer hub.Close()
	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	first := s.nextConn(t)
	if err := hub.SubscribeTrades(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if err := hub.SubscribeOrders(ctx, 3); err != nil {
		t.Fatal(err)
	}
	s.set(func(s *testServer) {
		s.invoke = func(c *serverConn, m clientMsg) {
			if c.n > 1 && m.Target == "SubscribeTrades" {
				c.fail(m.InvocationID, "trades unavailable")
				return
			}
			c.complete(m.InvocationID, "")
		}
	})
	first.drop()
	subErr := waitEvent[*SubscriptionErrorEvent](t, hub.Events(), nil)
	var hubErr *HubError
	if subErr.Method != "SubscribeTrades" || len(subErr.Arguments) != 1 || !errors.As(subErr.Err, &hubErr) || hubErr.Message != "trades unavailable" {
		t.Fatalf("subscription error = %+v", subErr)
	}
	waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnected))
	second := s.nextConn(t)
	second.waitMsg(t, isInvocation("SubscribeOrders"))
}

func TestPendingInvocationFailsOnConnectionLoss(t *testing.T) {
	s := newTestServer(t)
	s.set(func(s *testServer) {
		s.invoke = func(c *serverConn, m clientMsg) {
			if c.n == 1 {
				go c.drop()
				return
			}
			c.complete(m.InvocationID, "")
		}
	})
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, fastOptions(s.url)...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := hub.SubscribeOrders(context.Background(), 1); !errors.Is(err, ErrConnectionLost) {
		t.Fatalf("err = %v, want ErrConnectionLost", err)
	}
	waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnected))
	hub.e.mu.Lock()
	n := len(hub.e.subs)
	hub.e.mu.Unlock()
	if n != 0 {
		t.Fatal("a subscription that never completed must not be recorded")
	}
}

func TestInvocationsFailFastWhileReconnecting(t *testing.T) {
	s := newTestServer(t)
	s.set(func(s *testServer) {
		s.reject = func(n int, _ string) int {
			if n > 1 {
				return http.StatusServiceUnavailable
			}
			return 0
		}
	})
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, append(fastOptions(s.url),
		WithReconnectPolicy(ReconnectPolicy{Delays: []time.Duration{20 * time.Millisecond}, MaxAttempts: UnlimitedAttempts}))...)
	defer hub.Close()
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.nextConn(t).drop()
	waitEvent[*StateEvent](t, hub.Events(), func(ev *StateEvent) bool { return ev.State == StateReconnecting && ev.Attempt >= 2 })
	start := time.Now()
	if err := hub.SubscribeAccounts(context.Background()); !errors.Is(err, ErrNotConnected) || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("err = %v after %v, want an immediate ErrNotConnected", err, time.Since(start))
	}
	ev := waitEvent[*StateEvent](t, hub.Events(), func(ev *StateEvent) bool { return ev.State == StateReconnecting && ev.Attempt >= 3 })
	var dialErr *DialError
	if !errors.As(ev.Err, &dialErr) || dialErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("later attempts report the previous failure: %v", ev.Err)
	}
}

func TestServerCloseMessage(t *testing.T) {
	s := newTestServer(t)
	tokens, _ := tokenSequence()

	// allowReconnect: true reconnects.
	hub := NewUserHub(tokens, fastOptions(s.url)...)
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.nextConn(t).send(t, `{"type":7,"error":"server restarting","allowReconnect":true}`)
	ev := waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnecting))
	var closeErr *ServerCloseError
	if !errors.As(ev.Err, &closeErr) || !closeErr.AllowReconnect || closeErr.Message != "server restarting" {
		t.Fatalf("reconnecting cause = %v", ev.Err)
	}
	waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnected))

	// Without allowReconnect the connection ends.
	s.nextConn(t).send(t, `{"type":7,"error":"session revoked"}`)
	rest := waitClosed(t, hub.Events())
	last := rest[len(rest)-1].(*StateEvent)
	if last.State != StateClosed || !errors.As(last.Err, &closeErr) || closeErr.Message != "session revoked" {
		t.Fatalf("final event = %+v", last)
	}
	for _, ev := range rest {
		if se, ok := ev.(*StateEvent); ok && se.State == StateReconnecting {
			t.Fatal("a close without allowReconnect must not reconnect")
		}
	}
	if err := hub.SubscribeAccounts(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("after close: %v", err)
	}
	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
	checkNoEngineGoroutines(t)
}

func TestReconnectDisabledAndExhausted(t *testing.T) {
	s := newTestServer(t)
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, append(fastOptions(s.url), WithoutReconnect())...)
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.nextConn(t).drop()
	events := waitClosed(t, hub.Events())
	last := events[len(events)-1].(*StateEvent)
	if last.State != StateClosed || !errors.Is(last.Err, ErrConnectionLost) {
		t.Fatalf("final event = %+v", last)
	}
	if s.attemptCount() != 1 {
		t.Fatal("reconnect is disabled")
	}
	hub.Close()

	s = newTestServer(t)
	s.set(func(s *testServer) {
		s.reject = func(n int, _ string) int {
			if n > 1 {
				return http.StatusBadGateway
			}
			return 0
		}
	})
	hub = NewUserHub(tokens, append(fastOptions(s.url),
		WithReconnectPolicy(ReconnectPolicy{Delays: []time.Duration{time.Millisecond}, MaxAttempts: 2}))...)
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.nextConn(t).drop()
	events = waitClosed(t, hub.Events())
	attempts := 0
	for _, ev := range events {
		if se, ok := ev.(*StateEvent); ok && se.State == StateReconnecting {
			attempts++
		}
	}
	last = events[len(events)-1].(*StateEvent)
	if attempts != 2 || last.State != StateClosed || !strings.Contains(last.Err.Error(), "gave up after 2") || s.attemptCount() != 3 {
		t.Fatalf("attempts %d (server %d), final %+v", attempts, s.attemptCount(), last)
	}
	hub.Close()
	checkNoEngineGoroutines(t)
}

func TestCloseEndsEverything(t *testing.T) {
	s := newTestServer(t)
	s.set(func(s *testServer) {
		s.invoke = func(c *serverConn, m clientMsg) {
			if m.Target == "Hang" {
				return
			}
			c.complete(m.InvocationID, "")
		}
	})
	hub := NewUserHub(func(context.Context) (string, error) { return "t", nil },
		append(fastOptions(s.url), WithKeepAliveInterval(20*time.Millisecond))...)
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	if err := hub.SubscribeAccounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending := make(chan error, 1)
	go func() {
		_, err := hub.Invoke(context.Background(), "Hang")
		pending <- err
	}()
	c.waitMsg(t, isInvocation("Hang"))

	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-pending; !errors.Is(err, ErrClosed) {
		t.Fatalf("pending invocation = %v, want ErrClosed", err)
	}
	if m := c.waitMsg(t, func(m clientMsg) bool { return m.Type == 7 }); m.Raw != `{"type":7}` {
		t.Fatalf("close message = %q", m.Raw)
	}
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the socket was not closed")
	}
	events := waitClosed(t, hub.Events())
	last := events[len(events)-1].(*StateEvent)
	if last.State != StateClosed || last.Err != nil {
		t.Fatalf("final event = %+v", last)
	}
	if err := hub.Close(); err != nil {
		t.Fatal("Close must be idempotent")
	}
	if err := hub.SubscribeAccounts(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
	if err := hub.Connect(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Connect after Close: %v", err)
	}
	checkNoEngineGoroutines(t)
	if s.attemptCount() != 1 {
		t.Fatal("Close must stop reconnecting")
	}
}

func TestCloseDuringReconnectAndBeforeConnect(t *testing.T) {
	s := newTestServer(t)
	s.set(func(s *testServer) {
		s.reject = func(n int, _ string) int {
			if n > 1 {
				return http.StatusServiceUnavailable
			}
			return 0
		}
	})
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, append(fastOptions(s.url),
		WithReconnectPolicy(ReconnectPolicy{Delays: []time.Duration{time.Hour}, MaxAttempts: UnlimitedAttempts}))...)
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.nextConn(t).drop()
	waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnecting))
	start := time.Now()
	hub.Close() // interrupts the one-hour delay
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Close took %v", time.Since(start))
	}
	waitClosed(t, hub.Events())
	checkNoEngineGoroutines(t)

	never := NewMarketHub(tokens, fastOptions(s.url)...)
	if err := never.Close(); err != nil {
		t.Fatal(err)
	}
	if events := waitClosed(t, never.Events()); len(events) != 1 || events[0].(*StateEvent).State != StateClosed {
		t.Fatalf("events = %v", events)
	}
}

func TestBackpressureNeverBlocks(t *testing.T) {
	for _, policy := range []OverflowPolicy{DropOldest, DropNewest} {
		t.Run(fmt.Sprint(policy), func(t *testing.T) {
			s := newTestServer(t)
			hub := NewUserHub(func(context.Context) (string, error) { return "t", nil },
				append(fastOptions(s.url), WithEventBuffer(2), WithOverflowPolicy(policy))...)
			defer hub.Close()
			if err := hub.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			c := s.nextConn(t)
			var records []string
			for i := 1; i <= 10; i++ {
				records = append(records, fmt.Sprintf(`{"type":1,"target":"GatewayUserAccount","arguments":[{"id":%d}]}`, i))
			}
			c.send(t, records...)
			// Nobody reads events, yet invocations still complete: the read
			// loop is not blocked.
			if err := hub.SubscribeAccounts(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := hub.Dropped(); got != 9 {
				t.Fatalf("Dropped = %d, want 9", got)
			}
			var ids []int32
			for i := 0; i < 2; i++ {
				switch ev := nextEvent(t, hub.Events()).(type) {
				case *AccountEvent:
					ids = append(ids, ev.Account.ID)
				case *StateEvent:
					ids = append(ids, 0)
				}
			}
			want := []int32{9, 10}
			if policy == DropNewest {
				want = []int32{0, 1} // connected event and the first account
			}
			if fmt.Sprint(ids) != fmt.Sprint(want) {
				t.Fatalf("buffered = %v, want %v", ids, want)
			}
		})
	}
}

func TestStateEventsAreNeverDropped(t *testing.T) {
	s := newTestServer(t)
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, append(fastOptions(s.url), WithEventBuffer(1), WithOverflowPolicy(DropNewest), WithoutReconnect())...)
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	c.send(t, `{"type":1,"target":"GatewayUserAccount","arguments":[{"id":1}]}`)
	c.drop()
	events := waitClosed(t, hub.Events())
	if last, ok := events[len(events)-1].(*StateEvent); !ok || last.State != StateClosed {
		t.Fatalf("events = %v; the closed event must be delivered", events)
	}
}

func TestHubForClient(t *testing.T) {
	s := newTestServer(t)
	client, err := topstepx.NewClient(topstepx.WithToken("client-token"), topstepx.WithUserHubURL(s.url), topstepx.WithMarketHubURL(s.url))
	if err != nil {
		t.Fatal(err)
	}
	for _, connect := range []func() (func() error, error){
		func() (func() error, error) {
			h := NewUserHubForClient(client)
			return h.Close, h.Connect(context.Background())
		},
		func() (func() error, error) {
			h := NewMarketHubForClient(client)
			return h.Close, h.Connect(context.Background())
		},
	} {
		closeHub, err := connect()
		if err != nil {
			t.Fatal(err)
		}
		if c := s.nextConn(t); c.token != "client-token" {
			t.Fatalf("token = %q", c.token)
		}
		closeHub()
	}
	checkNoEngineGoroutines(t)
}

func TestConcurrentUse(t *testing.T) {
	s := newTestServer(t)
	hub := NewMarketHub(func(context.Context) (string, error) { return "t", nil }, fastOptions(s.url)...)
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := s.nextConn(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("C%d", i%5)
			if err := hub.SubscribeContractQuotes(context.Background(), id); err != nil {
				t.Error(err)
			}
			_ = hub.Dropped()
		}(i)
	}
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := 0; i < 50; i++ {
			_ = c.ws.Write(context.Background(), websocket.MessageText,
				[]byte(`{"type":1,"target":"GatewayQuote","arguments":["C1",{"symbol":"S","lastPrice":1}]}`+"\x1e"))
		}
	}()
	wg.Wait()
	<-sent
	hub.e.mu.Lock()
	n := len(hub.e.subs)
	hub.e.mu.Unlock()
	if n != 5 {
		t.Fatalf("registry has %d entries, want 5 (deduplicated)", n)
	}
	hub.Close()
	waitClosed(t, hub.Events())
	checkNoEngineGoroutines(t)
}

func TestPayloadModelsRoundTrip(t *testing.T) {
	in := `{"symbol":"S","symbolName":"/ES","lastPrice":6000.25,"bestBid":6000,"bestAsk":6000.5,"change":-1.25,"changePercent":-0.02,"open":6001,"high":6002,"low":5999,"volume":1200,"lastUpdated":"2025-07-01T14:00:00Z","timestamp":"2025-07-01T14:00:00.5Z"}`
	var q GatewayQuote
	if err := json.Unmarshal([]byte(in), &q); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Fatalf("round trip\n got %s\nwant %s", out, in)
	}
	for _, v := range []any{GatewayUserAccount{}, GatewayUserPosition{}, GatewayUserOrder{}, GatewayUserTrade{}, GatewayDepth{}, GatewayTrade{}} {
		if _, err := json.Marshal(v); err != nil {
			t.Fatalf("%T: %v", v, err)
		}
	}
	if DomType(12).String() != "Unrecognized(12)" || TradeLogType(1).String() != "Sell" || StateReconnected.String() != "reconnected" || ConnectionState(0).String() != "ConnectionState(0)" {
		t.Fatal("names")
	}
}
