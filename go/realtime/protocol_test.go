package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecordSplitter(t *testing.T) {
	var r recordSplitter
	recs, err := r.feed([]byte("{\"a\":1}\x1e{\"b\":2}\x1e{\"c\""))
	if err != nil || len(recs) != 2 || string(recs[0]) != `{"a":1}` || string(recs[1]) != `{"b":2}` {
		t.Fatalf("feed = %q, %v", recs, err)
	}
	first := recs[0]
	recs, _ = r.feed([]byte(":3}\x1e\x1e  \x1e"))
	if len(recs) != 1 || string(recs[0]) != `{"c":3}` {
		t.Fatalf("buffered record = %q", recs)
	}
	if string(first) != `{"a":1}` {
		t.Fatalf("earlier records must stay valid, got %q", first)
	}
	if recs, _ := r.feed(nil); len(recs) != 0 {
		t.Fatal("empty frame")
	}
	limited := recordSplitter{limit: 4}
	if _, err := limited.feed([]byte("123456")); err == nil {
		t.Fatal("an oversized incomplete record must fail")
	}
}

func TestHubURL(t *testing.T) {
	cases := map[string]string{
		"https://rtc.topstepx.com/hubs/user":      "wss://rtc.topstepx.com/hubs/user?access_token=a+b%2Fc",
		"http://localhost:5000/hubs/market?x=1":   "ws://localhost:5000/hubs/market?access_token=a+b%2Fc&x=1",
		"wss://example.com/hub":                   "wss://example.com/hub?access_token=a+b%2Fc",
		"ws://example.com/hub?access_token=stale": "ws://example.com/hub?access_token=a+b%2Fc",
	}
	for in, want := range cases {
		got, err := hubURL(in, "a b/c")
		if err != nil || got != want {
			t.Errorf("hubURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"ftp://example.com", "https://", "::"} {
		if _, err := hubURL(bad, "secret"); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("hubURL(%q) error = %v", bad, err)
		}
	}
}

func TestDialErrorsNeverContainTheToken(t *testing.T) {
	// A refused connection makes net/http report the full URL, including
	// the access_token query parameter.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	const token = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln+/="
	hub := NewUserHub(func(context.Context) (string, error) { return token, nil },
		WithURL("http://"+addr+"/hubs/user"), WithHandshakeTimeout(2*time.Second))
	defer hub.Close()
	err = hub.Connect(context.Background())
	var dialErr *DialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("err = %v, want *DialError", err)
	}
	msg := fmt.Sprintf("%v %+v %#v", err, err, err)
	for _, s := range []string{token, "eyJhbGciOiJIUzI1NiJ9", "c2ln"} {
		if strings.Contains(msg, s) {
			t.Fatalf("dial error leaks the token: %s", msg)
		}
	}
	// net/http puts the full URL in the error; it must arrive redacted.
	if !strings.Contains(err.Error(), "access_token=[REDACTED]") {
		t.Fatalf("dial error should show the redacted URL: %v", err)
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("only context errors may be unwrapped")
	}

	ctxErr := sanitizeDialError(fmt.Errorf("dial: %w", context.DeadlineExceeded), &http.Response{StatusCode: 502}, "tok")
	if !errors.Is(ctxErr, context.DeadlineExceeded) || ctxErr.(*DialError).StatusCode != 502 {
		t.Fatalf("sanitized = %#v", ctxErr)
	}
	plain := sanitizeDialError(errors.New(`Get "wss://h/x?access_token=abc%2Bdef&y=1": refused`), nil, "abc+def")
	if strings.Contains(plain.Error(), "abc") {
		t.Fatalf("sanitized = %v", plain)
	}
}

func TestEncodeInvocation(t *testing.T) {
	msg, err := encodeInvocation("7", "SubscribeOrders", []any{int32(12)})
	if err != nil || string(msg) != "{\"type\":1,\"invocationId\":\"7\",\"target\":\"SubscribeOrders\",\"arguments\":[12]}\x1e" {
		t.Fatalf("encodeInvocation = %q, %v", msg, err)
	}
	msg, _ = encodeInvocation("8", "SubscribeAccounts", nil)
	if !strings.Contains(string(msg), `"arguments":[]`) {
		t.Fatalf("empty arguments must be [], got %s", msg)
	}
	if _, err := encodeInvocation("9", "Bad", []any{make(chan int)}); err == nil {
		t.Fatal("unencodable arguments must fail")
	}
}

func TestReconnectPolicyDelays(t *testing.T) {
	p := DefaultReconnectPolicy()
	want := []time.Duration{0, time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := p.delay(i + 1); got != w {
			t.Errorf("delay(%d) = %v, want %v", i+1, got, w)
		}
	}
	if !p.enabled() || p.MaxAttempts != UnlimitedAttempts {
		t.Fatal("the default policy reconnects forever")
	}
	if (ReconnectPolicy{MaxAttempts: 0}).enabled() {
		t.Fatal("zero attempts disables reconnect")
	}
	if got := (ReconnectPolicy{MaxAttempts: 1}).delay(7); got != 30*time.Second {
		t.Fatalf("empty Delays use the defaults, got %v", got)
	}
	cfg := newConfig("u", []Option{nil, WithEventBuffer(0), WithKeepAliveInterval(-1), WithServerTimeout(0),
		WithHandshakeTimeout(0), WithInvocationTimeout(0), WithReadLimit(-5), WithURL("")})
	if cfg.eventBuffer != 1 || cfg.keepAlive != DefaultKeepAliveInterval || cfg.serverTimeout != DefaultServerTimeout ||
		cfg.handshakeTimeout != DefaultHandshakeTimeout || cfg.invocationTimeout != DefaultInvocationTimeout ||
		cfg.readLimit != DefaultReadLimit || cfg.url != "u" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestDecodeUserEventDirect(t *testing.T) {
	raw := func(s string) []json.RawMessage { return []json.RawMessage{json.RawMessage(s)} }
	events := decodeUserEvent(EventGatewayUserAccount, raw(`[{"id":1},{"action":"3","data":{"id":2}},{"action":null,"data":{"id":3}}]`))
	if len(events) != 3 {
		t.Fatalf("events = %v", events)
	}
	if a := events[1].(*AccountEvent); a.Account.ID != 2 || a.Action == nil || *a.Action != 3 {
		t.Fatalf("envelope with a string action = %+v", a)
	}
	if a := events[2].(*AccountEvent); a.Action != nil || a.Account.ID != 3 {
		t.Fatalf("null action = %+v", a)
	}
	for _, bad := range []string{`{"action":"x","data":{}}`, `{"data":5}`, `[1]`, `null`, `5`} {
		evs := decodeUserEvent(EventGatewayUserPosition, raw(bad))
		if len(evs) != 1 {
			t.Fatalf("%s: events = %v", bad, evs)
		}
		if u, ok := evs[0].(*UnknownEvent); !ok || u.Err == nil || u.Target != EventGatewayUserPosition {
			t.Errorf("%s: got %#v, want an unknown event with an error", bad, evs[0])
		}
	}
	// One bad element does not hide the good ones.
	evs := decodeUserEvent(EventGatewayUserTrade, raw(`[{"id":1,"orderId":2},{"id":"bad"}]`))
	if len(evs) != 2 {
		t.Fatalf("events = %v", evs)
	}
	if _, ok := evs[0].(*UserTradeEvent); !ok {
		t.Fatalf("first = %#v", evs[0])
	}
	if _, ok := evs[1].(*UnknownEvent); !ok {
		t.Fatalf("second = %#v", evs[1])
	}
}

func TestDecodeMarketEventDirect(t *testing.T) {
	args := func(s ...string) []json.RawMessage {
		out := make([]json.RawMessage, len(s))
		for i, x := range s {
			out[i] = json.RawMessage(x)
		}
		return out
	}
	cases := map[string][]json.RawMessage{
		"one argument":        args(`{"symbol":"S"}`),
		"numeric contract":    args(`5`, `{"symbol":"S"}`),
		"scalar payload":      args(`"C"`, `"x"`),
		"bad volume":          args(`"C"`, `{"volume":"lots"}`),
		"fractional volume":   args(`"C"`, `{"volume":2.5}`),
		"bad trade timestamp": args(`"C"`, `[{"timestamp":"yesterday"}]`),
	}
	targets := map[string]string{"bad volume": EventGatewayQuote, "fractional volume": EventGatewayDepth, "bad trade timestamp": EventGatewayTrade}
	for name, a := range cases {
		target := targets[name]
		if target == "" {
			target = EventGatewayQuote
		}
		evs := decodeMarketEvent(target, a)
		if u, ok := evs[0].(*UnknownEvent); len(evs) != 1 || !ok || u.Err == nil {
			t.Errorf("%s: events = %#v", name, evs)
		}
	}
	quotes := decodeMarketEvent(EventGatewayQuote, args(`"C"`, `[{"symbol":"A"},{"symbol":"B"}]`))
	if len(quotes) != 2 || quotes[1].(*QuoteEvent).Quote.Symbol != "B" {
		t.Fatalf("quote list = %#v", quotes)
	}
	if evs := decodeMarketEvent("Other", args(`1`)); evs[0].(*UnknownEvent).Err != nil {
		t.Fatal("unknown targets carry no error")
	}
}
