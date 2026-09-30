package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
)

// Rejected tokens (contract section 11): an upgrade rejected with HTTP 401
// reports the token to the token source, so the next attempt does not reuse
// it.

// restServer is a local REST gateway whose loginKey issues token-1, token-2, ...
type restServer struct {
	srv    *httptest.Server
	logins atomic.Int32
}

func newRESTServer(t *testing.T) *restServer {
	t.Helper()
	r := &restServer{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/Auth/loginKey" {
			http.NotFound(w, req)
			return
		}
		n := r.logins.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success":true,"errorCode":0,"errorMessage":null,"token":"token-%d"}`, n)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func newRESTClient(t *testing.T, rest *restServer, hubURL string) *topstepx.Client {
	t.Helper()
	client, err := topstepx.NewClient(topstepx.WithBaseURL(rest.srv.URL), topstepx.WithCredentials("user", "key"),
		topstepx.WithUserHubURL(hubURL), topstepx.WithMarketHubURL(hubURL))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertUnauthorizedDialError(t *testing.T, err error, token string) {
	t.Helper()
	var dialErr *DialError
	if !errors.As(err, &dialErr) || dialErr.StatusCode != http.StatusUnauthorized ||
		!errors.Is(err, ErrUnauthorized) || !errors.Is(err, topstepx.ErrUnauthorized) {
		t.Fatalf("err = %v, want a *DialError matching ErrUnauthorized", err)
	}
	if !strings.Contains(err.Error(), "token rejected (HTTP 401)") {
		t.Fatalf("err = %q, want the 401 surfaced", err)
	}
	if msg := fmt.Sprintf("%v %+v %#v", err, err, err); strings.Contains(msg, token) {
		t.Fatalf("error leaks the token: %s", msg)
	}
}

func TestRejectedTokenOnReconnect(t *testing.T) {
	rest := newRESTServer(t)
	hubSrv := newTestServer(t)
	client := newRESTClient(t, rest, hubSrv.url)
	var revoked atomic.Bool
	hubSrv.set(func(s *testServer) {
		s.reject = func(_ int, token string) int {
			if revoked.Load() && token == "token-1" {
				return http.StatusUnauthorized
			}
			return 0
		}
	})
	hub := NewUserHubForClient(client, fastOptions(hubSrv.url)...)
	defer hub.Close()
	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	first := hubSrv.nextConn(t)
	if first.token != "token-1" {
		t.Fatalf("first connection used %q", first.token)
	}
	if err := hub.SubscribeOrders(ctx, 7); err != nil {
		t.Fatal(err)
	}
	waitEvent[*StateEvent](t, hub.Events(), isState(StateConnected))

	// The server revokes token-1 and drops the connection. The first
	// reconnect attempt still offers the cached token-1 and is rejected.
	revoked.Store(true)
	first.drop()
	waitEvent[*StateEvent](t, hub.Events(), func(ev *StateEvent) bool { return ev.State == StateReconnecting && ev.Attempt == 1 })
	second := waitEvent[*StateEvent](t, hub.Events(), func(ev *StateEvent) bool { return ev.State == StateReconnecting && ev.Attempt == 2 })
	assertUnauthorizedDialError(t, second.Err, "token-1")

	// The rejected token was discarded, so the next attempt logged in again.
	waitEvent[*StateEvent](t, hub.Events(), isState(StateReconnected))
	conn := hubSrv.nextConn(t)
	if conn.token != "token-2" || conn.auth != "Bearer token-2" {
		t.Fatalf("reconnect used %q / %q, want the fresh token-2", conn.token, conn.auth)
	}
	conn.waitMsg(t, isInvocation("SubscribeOrders"))
	if got := rest.logins.Load(); got != 2 {
		t.Fatalf("loginKey calls = %d, want 2", got)
	}
	if got := hubSrv.attemptCount(); got != 3 {
		t.Fatalf("upgrade attempts = %d, want 3 (accepted, rejected, accepted)", got)
	}
	if tok, err := client.Token(ctx); err != nil || tok != "token-2" {
		t.Fatalf("client token = %q, %v", tok, err)
	}
}

func TestRejectedTokenOnInitialConnect(t *testing.T) {
	rest := newRESTServer(t)
	hubSrv := newTestServer(t)
	client := newRESTClient(t, rest, hubSrv.url)
	hubSrv.set(func(s *testServer) {
		s.reject = func(n int, _ string) int {
			if n == 1 {
				return http.StatusUnauthorized
			}
			return 0
		}
	})
	hub := NewMarketHubForClient(client, fastOptions(hubSrv.url)...)
	defer hub.Close()
	ctx := context.Background()

	err := hub.Connect(ctx)
	assertUnauthorizedDialError(t, err, "token-1")
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	conn := hubSrv.nextConn(t)
	if conn.token != "token-2" {
		t.Fatalf("second Connect used %q, want the fresh token-2", conn.token)
	}
	if got := rest.logins.Load(); got != 2 {
		t.Fatalf("loginKey calls = %d, want 2", got)
	}
	if err := hub.SubscribeContractQuotes(ctx, "C"); err != nil {
		t.Fatal(err)
	}
}

// A plain TokenProvider keeps working; WithTokenRejected hears about 401s
// only.
func TestRejectedTokenHookWithPlainProvider(t *testing.T) {
	hubSrv := newTestServer(t)
	hubSrv.set(func(s *testServer) {
		s.reject = func(n int, _ string) int {
			switch n {
			case 1:
				return http.StatusServiceUnavailable
			case 2:
				return http.StatusUnauthorized
			}
			return 0
		}
	})
	var mu sync.Mutex
	var rejected []string
	tokens, _ := tokenSequence()
	hub := NewUserHub(tokens, append(fastOptions(hubSrv.url), WithTokenRejected(func(token string) {
		mu.Lock()
		rejected = append(rejected, token)
		mu.Unlock()
	}))...)
	defer hub.Close()
	ctx := context.Background()
	reported := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), rejected...)
	}

	err := hub.Connect(ctx)
	var dialErr *DialError
	if !errors.As(err, &dialErr) || dialErr.StatusCode != http.StatusServiceUnavailable || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("first Connect = %v, want a 503 *DialError", err)
	}
	if got := reported(); len(got) != 0 {
		t.Fatalf("a 503 must not report the token, got %v", got)
	}
	assertUnauthorizedDialError(t, hub.Connect(ctx), "token-2")
	if got := reported(); len(got) != 1 || got[0] != "token-2" {
		t.Fatalf("reported = %v, want [token-2]", got)
	}
	if err := hub.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if conn := hubSrv.nextConn(t); conn.token != "token-3" {
		t.Fatalf("third Connect used %q", conn.token)
	}
}

// Without the hook (a plain provider), a 401 is still reported distinctly.
func TestRejectedTokenWithoutHook(t *testing.T) {
	hubSrv := newTestServer(t)
	hubSrv.set(func(s *testServer) { s.reject = func(int, string) int { return http.StatusUnauthorized } })
	hub := NewUserHub(func(context.Context) (string, error) { return "static-token", nil }, fastOptions(hubSrv.url)...)
	defer hub.Close()
	assertUnauthorizedDialError(t, hub.Connect(context.Background()), "static-token")
}
