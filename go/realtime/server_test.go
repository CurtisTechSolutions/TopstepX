package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testServer is a local SignalR hub that speaks the JSON hub protocol.
type testServer struct {
	t   *testing.T
	srv *httptest.Server
	url string // http URL of the hub; the client converts it to ws

	mu sync.Mutex
	// handshake returns the handshake reply frame for the n-th connection
	// (1-based); nil means "{}\x1e".
	handshake func(n int) string
	// reject returns an HTTP status that refuses the n-th upgrade (offering
	// token); 0 accepts.
	reject func(n int, token string) int
	// invoke answers a client invocation; nil completes it successfully.
	invoke func(c *serverConn, m clientMsg)
	// pingEvery makes the server send keepalive pings; 0 disables them.
	pingEvery time.Duration
	attempts  int
	conns     []*serverConn
	accepted  chan *serverConn
}

// serverConn is the server side of one client connection.
type serverConn struct {
	n     int
	ws    *websocket.Conn
	token string // access_token query value
	auth  string // Authorization header
	query string // raw query
	msgs  chan clientMsg
	done  chan struct{}
}

// clientMsg is a message received from the client.
type clientMsg struct {
	Type         int               `json:"type"`
	InvocationID string            `json:"invocationId"`
	Target       string            `json:"target"`
	Arguments    []json.RawMessage `json:"arguments"`
	Protocol     string            `json:"protocol"`
	Raw          string            `json:"-"`
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	s := &testServer{t: t, accepted: make(chan *serverConn, 16)}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	s.url = s.srv.URL + "/hubs/user"
	t.Cleanup(func() {
		s.mu.Lock()
		conns := append([]*serverConn(nil), s.conns...)
		s.mu.Unlock()
		for _, c := range conns {
			_ = c.ws.CloseNow()
		}
		s.srv.Close()
	})
	return s
}

func (s *testServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.attempts++
	n := s.attempts
	reject, handshake, pingEvery := s.reject, s.handshake, s.pingEvery
	s.mu.Unlock()
	if reject != nil {
		if code := reject(n, r.URL.Query().Get("access_token")); code != 0 {
			http.Error(w, "rejected", code)
			return
		}
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c := &serverConn{
		n: n, ws: ws, token: r.URL.Query().Get("access_token"), auth: r.Header.Get("Authorization"),
		query: r.URL.RawQuery, msgs: make(chan clientMsg, 4096), done: make(chan struct{}),
	}
	defer close(c.done)
	ctx := context.Background()

	// Handshake.
	_, data, err := ws.Read(ctx)
	if err != nil {
		return
	}
	if string(data) != "{\"protocol\":\"json\",\"version\":1}\x1e" {
		s.t.Errorf("unexpected handshake request %q", data)
		_ = ws.CloseNow()
		return
	}
	reply := "{}\x1e"
	if handshake != nil {
		reply = handshake(n)
	}
	if reply == "" { // never answer; wait for the client to give up
		_, _, _ = ws.Read(ctx)
		return
	}
	if err := ws.Write(ctx, websocket.MessageText, []byte(reply)); err != nil {
		return
	}
	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.mu.Unlock()
	if strings.HasPrefix(reply, "{}") {
		s.accepted <- c
	}

	if pingEvery > 0 {
		go func() {
			ticker := time.NewTicker(pingEvery)
			defer ticker.Stop()
			for {
				select {
				case <-c.done:
					return
				case <-ticker.C:
					if ws.Write(ctx, websocket.MessageText, []byte("{\"type\":6}\x1e")) != nil {
						return
					}
				}
			}
		}()
	}

	var buf []byte
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		buf = append(buf, data...)
		for {
			i := bytes.IndexByte(buf, 0x1e)
			if i < 0 {
				break
			}
			rec := string(buf[:i])
			buf = buf[i+1:]
			var m clientMsg
			if err := json.Unmarshal([]byte(rec), &m); err != nil {
				s.t.Errorf("client sent invalid JSON %q", rec)
				continue
			}
			m.Raw = rec
			select {
			case c.msgs <- m:
			default:
				s.t.Errorf("test server message buffer overflow")
			}
			if m.Type == 1 {
				s.mu.Lock()
				invoke := s.invoke
				s.mu.Unlock()
				if invoke != nil {
					invoke(c, m)
				} else {
					c.complete(m.InvocationID, "")
				}
			}
		}
	}
}

func (s *testServer) set(f func(s *testServer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// nextConn waits for the next accepted connection.
func (s *testServer) nextConn(t *testing.T) *serverConn {
	t.Helper()
	select {
	case c := <-s.accepted:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no connection accepted")
		return nil
	}
}

func (s *testServer) attemptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

// sendFrame writes one frame holding the given text verbatim.
func (c *serverConn) sendFrame(t *testing.T, frame string) {
	t.Helper()
	if err := c.ws.Write(context.Background(), websocket.MessageText, []byte(frame)); err != nil {
		t.Fatalf("server write: %v", err)
	}
}

// send writes the records, each terminated by the record separator, in one
// frame.
func (c *serverConn) send(t *testing.T, records ...string) {
	t.Helper()
	c.sendFrame(t, strings.Join(records, "\x1e")+"\x1e")
}

// complete answers an invocation; an empty result means null.
func (c *serverConn) complete(id, result string) {
	msg := `{"type":3,"invocationId":"` + id + `"}` + "\x1e"
	if result != "" {
		msg = `{"type":3,"invocationId":"` + id + `","result":` + result + "}\x1e"
	}
	_ = c.ws.Write(context.Background(), websocket.MessageText, []byte(msg))
}

func (c *serverConn) fail(id, message string) {
	msg := `{"type":3,"invocationId":"` + id + `","error":"` + message + "\"}\x1e"
	_ = c.ws.Write(context.Background(), websocket.MessageText, []byte(msg))
}

// drop closes the connection abruptly.
func (c *serverConn) drop() { _ = c.ws.CloseNow() }

// waitMsg returns the next client message matching pred.
func (c *serverConn) waitMsg(t *testing.T, pred func(clientMsg) bool) clientMsg {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-c.msgs:
			if pred(m) {
				return m
			}
		case <-deadline:
			t.Fatal("expected client message not received")
			return clientMsg{}
		}
	}
}

func isInvocation(target string) func(clientMsg) bool {
	return func(m clientMsg) bool { return m.Type == 1 && m.Target == target }
}

// fastOptions keeps timings short for tests.
func fastOptions(url string) []Option {
	return []Option{
		WithURL(url),
		WithReconnectPolicy(ReconnectPolicy{Delays: []time.Duration{0, 10 * time.Millisecond}, MaxAttempts: UnlimitedAttempts}),
		WithHandshakeTimeout(2 * time.Second),
		WithInvocationTimeout(2 * time.Second),
	}
}

// tokenSequence returns a provider yielding token-1, token-2, ...
func tokenSequence() (TokenProvider, func() int) {
	var mu sync.Mutex
	n := 0
	return func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			n++
			return "token-" + strconv.Itoa(n), nil
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return n
		}
}

// nextEvent returns the next event or fails after a timeout.
func nextEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("event channel closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event received")
		return nil
	}
}

// waitEvent skips events until one of type T satisfies pred.
func waitEvent[T Event](t *testing.T, events <-chan Event, pred func(T) bool) T {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event channel closed while waiting")
			}
			if typed, ok := ev.(T); ok && (pred == nil || pred(typed)) {
				return typed
			}
		case <-deadline:
			var zero T
			t.Fatalf("no %T event received", zero)
			return zero
		}
	}
}

func isState(s ConnectionState) func(*StateEvent) bool {
	return func(ev *StateEvent) bool { return ev.State == s }
}

// waitClosed drains events until the channel closes and returns them.
func waitClosed(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var all []Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return all
			}
			all = append(all, ev)
		case <-deadline:
			t.Fatal("event channel not closed")
			return all
		}
	}
}

// checkNoEngineGoroutines fails if any hub goroutine is still running.
func checkNoEngineGoroutines(t *testing.T) {
	t.Helper()
	markers := []string{"realtime.(*engine).readLoop", "realtime.(*engine).keepAlive", "realtime.(*engine).run", "realtime.(*engine).reconnect"}
	deadline := time.Now().Add(3 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		stacks := string(buf[:runtime.Stack(buf, true)])
		leaked := false
		for _, m := range markers {
			if strings.Contains(stacks, m) {
				leaked = true
			}
		}
		if !leaked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("hub goroutines still running after Close:\n%s", stacks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
