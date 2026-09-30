package topstepx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// HTTP behavior against a local mock server (contract section 12.3).

func TestLazyLoginAndBearerHeader(t *testing.T) {
	m := newMockServer(t)
	token := makeJWT(time.Now().Add(24*time.Hour), "lazy")
	m.handleLogin(token)
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithCredentials("user", "key"))

	if m.total() != 0 {
		t.Fatal("NewClient must not perform I/O")
	}
	for i := 0; i < 2; i++ {
		accounts, err := c.Accounts.Search(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		if len(accounts) != 1 || accounts[0].Balance.String() != "50000.25" {
			t.Fatalf("accounts = %+v", accounts)
		}
	}
	if got := m.count("/api/Auth/loginKey"); got != 1 {
		t.Fatalf("logins = %d, want 1 (lazy, once)", got)
	}
	login := m.last("/api/Auth/loginKey")
	assertEquivalentJSON(t, []byte(`{"userName":"user","apiKey":"key"}`), login.Body, "login body")
	if login.Header.Get("Authorization") != "" {
		t.Error("login must not send a bearer token")
	}
	for _, r := range m.all("/api/Account/search") {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers = %v", r.Header)
		}
		if r.Header.Get("User-Agent") != "topstepx-go/"+Version {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		assertEquivalentJSON(t, []byte(`{"onlyActiveAccounts":true}`), r.Body, "search body")
	}
	if got, err := c.Token(context.Background()); err != nil || got != token {
		t.Fatalf("Token() = %q, %v", got, err)
	}
}

func TestNotAuthenticatedFailsBeforeIO(t *testing.T) {
	m := newMockServer(t)
	c := m.client(t)
	_, err := c.Accounts.Search(context.Background(), true)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("err = %v, want ErrNotAuthenticated", err)
	}
	if _, err := c.Token(context.Background()); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("Token err = %v", err)
	}
	if m.total() != 0 {
		t.Fatalf("server saw %d requests", m.total())
	}
}

func TestInvalidCredentialsIsAPIError(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Auth/loginKey", okJSON(`{"success":false,"errorCode":3,"errorMessage":null,"hasBusinessFailure":true,"token":null}`))
	c := m.client(t, WithCredentials("user", "wrong"))

	_, err := c.Accounts.Search(context.Background(), false)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Operation != OpAuthLoginKey || apiErr.Code != 3 || apiErr.CodeName != "InvalidCredentials" || apiErr.Message != nil {
		t.Fatalf("APIError = %+v", apiErr)
	}
	if resp, ok := apiErr.Response.(*LoginResponse); !ok || resp.Success {
		t.Fatalf("Response = %#v", apiErr.Response)
	}
	if c.sess.current() != "" {
		t.Fatal("a failed login must store nothing")
	}
	if m.count("/api/Account/search") != 0 {
		t.Fatal("the request must not be sent after a failed login")
	}
	// An explicit LoginKey reports the same failure.
	if _, err := c.Auth.LoginKey(context.Background(), "user", "wrong"); !errors.As(err, &apiErr) || apiErr.Code != 3 {
		t.Fatalf("LoginKey err = %v", err)
	}
}

func TestBusinessFailureRules(t *testing.T) {
	m := newMockServer(t)
	// errorCode != 0 is a failure even when success is true; hasBusinessFailure is absent.
	m.handle("/api/Contract/searchById", okJSON(`{"success":true,"errorCode":1,"errorMessage":"no such contract"}`))
	// Unknown codes are named Unrecognized(n).
	m.handle("/api/Order/cancel", okJSON(`{"success":false,"errorCode":77}`))
	c := m.client(t, WithToken("tok"))

	_, err := c.Contracts.SearchByID(context.Background(), "X")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.CodeName != "ContractNotFound" || *apiErr.Message != "no such contract" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Contract_SearchContractById") || !strings.Contains(err.Error(), "/api/Contract/searchById") {
		t.Fatalf("error should name the operation: %v", err)
	}
	err = c.Orders.Cancel(context.Background(), 1, 2)
	if !errors.As(err, &apiErr) || apiErr.Code != 77 || apiErr.CodeName != "Unrecognized(77)" || apiErr.OrderID != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectedOrderKeepsOrderID(t *testing.T) {
	m := newMockServer(t)
	rejected := `{"success":false,"errorCode":2,"errorMessage":"Trail Distance not set.","hasBusinessFailure":true,"orderId":457}`
	m.handle("/api/Order/place", okJSON(rejected))
	c := m.client(t, WithToken("tok"))

	id, err := c.Orders.Place(context.Background(), NewTrailingStopOrder(123, "CON.F.US.EP.U25", OrderSideAsk, 1, decimal.RequireFromString("5998.5")))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || id != 0 {
		t.Fatalf("Place = %d, %v; want *APIError", id, err)
	}
	if apiErr.OrderID == nil || *apiErr.OrderID != 457 || apiErr.Code != 2 || apiErr.CodeName != "OrderRejected" ||
		apiErr.Message == nil || *apiErr.Message != "Trail Distance not set." {
		t.Fatalf("APIError = %+v", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "orderId 457") {
		t.Fatalf("Error() = %q", apiErr.Error())
	}

	// The escape hatch returns the envelope without converting the failure.
	var resp PlaceOrderResponse
	if err := c.Do(context.Background(), OpOrderPlaceOrder, NewMarketOrder(123, "C", OrderSideBid, 1), &resp); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.Success || !resp.IsBusinessFailure() || resp.OrderID == nil || *resp.OrderID != 457 || resp.ErrorCode != PlaceOrderErrorCodeOrderRejected {
		t.Fatalf("Do response = %+v", resp)
	}
	var raw json.RawMessage
	if err := c.Do(context.Background(), OpOrderPlaceOrder, NewMarketOrder(123, "C", OrderSideBid, 1), &raw); err != nil || !strings.Contains(string(raw), "457") {
		t.Fatalf("Do raw = %s, %v", raw, err)
	}
	if err := c.Do(context.Background(), OperationID("Nope"), nil, nil); err == nil {
		t.Fatal("Do with an unknown operation succeeded")
	}
	if err := c.Do(context.Background(), OpOrderPlaceOrder, nil, &resp); err == nil {
		t.Fatal("Do without a request body succeeded")
	}
}

func TestUnauthorizedReloginAndRetryOnce(t *testing.T) {
	m := newMockServer(t)
	first, second := makeJWT(time.Now().Add(24*time.Hour), "first"), makeJWT(time.Now().Add(24*time.Hour), "second")
	m.handleLogin(first, second)
	m.handle("/api/Account/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+first {
			writeJSON(w, http.StatusUnauthorized, "")
			return
		}
		writeJSON(w, http.StatusOK, accountsOK)
	})
	c := m.client(t, WithCredentials("user", "key"), WithMaxRetries(0))

	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := m.count("/api/Auth/loginKey"); got != 2 {
		t.Fatalf("logins = %d, want 2", got)
	}
	reqs := m.all("/api/Account/search")
	if len(reqs) != 2 || reqs[1].Header.Get("Authorization") != "Bearer "+second {
		t.Fatalf("searches = %d, second auth %q", len(reqs), reqs[len(reqs)-1].Header.Get("Authorization"))
	}
	if len(m.recordedSleeps()) != 0 {
		t.Fatal("401 handling must not sleep or use the retry budget")
	}
}

func TestUnauthorizedTwiceIsErrUnauthorized(t *testing.T) {
	m := newMockServer(t)
	m.handleLogin("a", "b", "c")
	m.handle("/api/Account/search", status(http.StatusUnauthorized, `{"message":"expired"}`))
	c := m.client(t, WithCredentials("user", "key"))

	_, err := c.Accounts.Search(context.Background(), true)
	var httpErr *HTTPError
	if !errors.Is(err, ErrUnauthorized) || !errors.As(err, &httpErr) || httpErr.StatusCode != 401 {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if !strings.Contains(httpErr.Body, "expired") {
		t.Fatalf("Body = %q", httpErr.Body)
	}
	if logins, searches := m.count("/api/Auth/loginKey"), m.count("/api/Account/search"); logins != 2 || searches != 2 {
		t.Fatalf("logins = %d, searches = %d; want one re-login and one retry", logins, searches)
	}
}

func TestUnauthorizedWithoutCredentials(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Account/search", status(http.StatusUnauthorized, ""))
	c := m.client(t, WithToken("tok"))
	if _, err := c.Accounts.Search(context.Background(), true); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
	if m.count("/api/Account/search") != 1 {
		t.Fatal("without credentials a 401 is not retried")
	}
}

func TestUnauthorizedFailedReloginKeepsCause(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Auth/loginKey", okJSON(`{"success":false,"errorCode":10,"token":null}`))
	m.handle("/api/Account/search", status(http.StatusUnauthorized, ""))
	c := m.client(t, WithToken("tok"), WithCredentials("user", "key"))
	_, err := c.Accounts.Search(context.Background(), true)
	var apiErr *APIError
	if !errors.Is(err, ErrUnauthorized) || !errors.As(err, &apiErr) || apiErr.CodeName != "ApiKeyAuthenticationDisabled" {
		t.Fatalf("err = %v", err)
	}
}

// If another caller replaced the token while the request was in flight, the
// retry uses that token instead of logging in again.
func TestUnauthorizedRetriesWithReplacedToken(t *testing.T) {
	m := newMockServer(t)
	m.handleLogin("login-token")
	var c *Client
	m.handle("/api/Account/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer stale" {
			c.sess.store("fresh", time.Now()) // another caller refreshed meanwhile
			writeJSON(w, http.StatusUnauthorized, "")
			return
		}
		writeJSON(w, http.StatusOK, accountsOK)
	})
	c = m.client(t, WithToken("stale"), WithCredentials("user", "key"))
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if m.count("/api/Auth/loginKey") != 0 {
		t.Fatal("must not log in when the token was already replaced")
	}
	if got := m.last("/api/Account/search").Header.Get("Authorization"); got != "Bearer fresh" {
		t.Fatalf("retry used %q", got)
	}
}

func TestRateLimited429HonorsRetryAfter(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Account/search", sequence(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "7")
			writeJSON(w, http.StatusTooManyRequests, "")
		},
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(http.TimeFormat))
			writeJSON(w, http.StatusTooManyRequests, "")
		},
		okJSON(accountsOK),
	))
	c := m.client(t, WithToken("tok"))
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	sleeps := m.recordedSleeps()
	if len(sleeps) != 2 || sleeps[0] != 7*time.Second || sleeps[1] < time.Second || sleeps[1] > 3*time.Second {
		t.Fatalf("sleeps = %v, want [7s ~3s] from Retry-After", sleeps)
	}
	if m.count("/api/Account/search") != 3 {
		t.Fatal("expected two retries")
	}
}

func TestRateLimited429RealWait(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Account/search", sequence(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, "")
		},
		okJSON(accountsOK),
	))
	c, err := NewClient(WithBaseURL(m.srv.URL), WithToken("tok"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("retried after %v, want about 1s (Retry-After)", elapsed)
	}
}

func TestRateLimited429ExhaustedAndBackoff(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Order/place", status(http.StatusTooManyRequests, "slow down"))
	c := m.client(t, WithToken("tok"))

	// Writes are retried on 429 too; without Retry-After the backoff is 1s, 2s, 4s.
	_, err := c.Orders.Place(context.Background(), NewMarketOrder(1, "C", OrderSideBid, 1))
	var rlErr *RateLimitError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &rlErr) || rlErr.RetryAfter != nil || rlErr.Body != "slow down" {
		t.Fatalf("err = %v", err)
	}
	if got := m.count("/api/Order/place"); got != 4 {
		t.Fatalf("attempts = %d, want 1 + 3 retries", got)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if sleeps := m.recordedSleeps(); len(sleeps) != 3 || sleeps[0] != want[0] || sleeps[1] != want[1] || sleeps[2] != want[2] {
		t.Fatalf("sleeps = %v, want %v", sleeps, want)
	}

	// Retry-After is exposed when the budget is spent; zero retries disables retrying.
	m.handle("/api/Account/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		writeJSON(w, http.StatusTooManyRequests, "")
	})
	c = m.client(t, WithToken("tok"), WithMaxRetries(0))
	_, err = c.Accounts.Search(context.Background(), true)
	if !errors.As(err, &rlErr) || rlErr.RetryAfter == nil || *rlErr.RetryAfter != 5*time.Second {
		t.Fatalf("err = %v", err)
	}
	if m.count("/api/Account/search") != 1 {
		t.Fatal("max retries 0 must not retry")
	}
}

func TestWritesNotRetriedOn503(t *testing.T) {
	m := newMockServer(t)
	for _, p := range []string{"/api/Order/place", "/api/Order/cancel", "/api/Order/modify", "/api/Position/closeContract", "/api/Position/partialCloseContract"} {
		m.handle(p, status(http.StatusServiceUnavailable, "down"))
	}
	c := m.client(t, WithToken("tok"))
	ctx := context.Background()
	checks := map[string]func() error{
		"/api/Order/place": func() error {
			_, err := c.Orders.Place(ctx, NewMarketOrder(1, "C", OrderSideBid, 1))
			return err
		},
		"/api/Order/cancel": func() error { return c.Orders.Cancel(ctx, 1, 2) },
		"/api/Order/modify": func() error {
			return c.Orders.Modify(ctx, ModifyOrderRequest{AccountID: 1, OrderID: 2, Size: Ptr(int32(3))})
		},
		"/api/Position/closeContract":        func() error { return c.Positions.Close(ctx, 1, "C") },
		"/api/Position/partialCloseContract": func() error { return c.Positions.PartialClose(ctx, 1, "C", 1) },
	}
	for path, call := range checks {
		err := call()
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != 503 {
			t.Errorf("%s: err = %v, want HTTP 503", path, err)
		}
		if got := m.count(path); got != 1 {
			t.Errorf("%s: attempts = %d, want 1 (writes are not retried on 503)", path, got)
		}
	}
}

func TestReadsRetriedOn5xx(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Position/searchOpen", sequence(status(502, ""), status(503, ""), status(504, ""),
		okJSON(`{"success":true,"errorCode":0,"positions":[]}`)))
	c := m.client(t, WithToken("tok"))
	positions, err := c.Positions.SearchOpen(context.Background(), 1)
	if err != nil || len(positions) != 0 {
		t.Fatalf("SearchOpen = %v, %v", positions, err)
	}
	if got := m.count("/api/Position/searchOpen"); got != 4 {
		t.Fatalf("attempts = %d, want 4", got)
	}
	// 500 is not retried.
	m.handle("/api/Trade/search", status(500, "boom"))
	_, err = c.Trades.Search(context.Background(), SearchTradeRequest{AccountID: 1, StartTimestamp: time.Now()})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 500 || m.count("/api/Trade/search") != 1 {
		t.Fatalf("err = %v", err)
	}
}

// hangup closes the connection without a response, a transport error.
func hangup(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func TestTransportErrorsRetriedForReadsOnly(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Contract/available", sequence(hangup, okJSON(`{"success":true,"errorCode":0,"contracts":[]}`)))
	m.handle("/api/Order/place", hangup)
	c := m.client(t, WithToken("tok"))

	if _, err := c.Contracts.Available(context.Background(), false); err != nil {
		t.Fatalf("Available: %v", err)
	}
	if got := m.count("/api/Contract/available"); got != 2 {
		t.Fatalf("available attempts = %d, want 2", got)
	}
	_, err := c.Orders.Place(context.Background(), NewMarketOrder(1, "C", OrderSideBid, 1))
	var tErr *TransportError
	if !errors.As(err, &tErr) || tErr.Operation != OpOrderPlaceOrder {
		t.Fatalf("err = %v, want *TransportError", err)
	}
	if got := m.count("/api/Order/place"); got != 1 {
		t.Fatalf("place attempts = %d, want 1", got)
	}
}

func TestPerAttemptTimeoutIsTransportError(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Status/ping", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	c := m.client(t, WithTimeout(50*time.Millisecond), WithMaxRetries(1))
	_, err := c.Status.Ping(context.Background())
	var tErr *TransportError
	if !errors.As(err, &tErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a timeout *TransportError", err)
	}
	if got := m.count("/api/Status/ping"); got != 2 {
		t.Fatalf("attempts = %d, want 2 (ping is a read)", got)
	}

	// A canceled context stops immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Status.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestProblemDetails400(t *testing.T) {
	m := newMockServer(t)
	problem := `{"type":"https://tools.ietf.org/html/rfc9110#section-15.5.1","title":"One or more validation errors occurred.","status":400,"traceId":"00-abc-01","errors":{"Filter.AccountId":["The field is required."]}}`
	m.handle("/api/Order/v2/query", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(problem))
	})
	m.handle("/api/Auth/loginKey", status(http.StatusBadRequest, "userName is required"))
	c := m.client(t, WithToken("tok"))

	_, err := c.Orders.Query(context.Background(), SearchOrdersQueryRequest{Filter: GatewayOrderFilter{AccountID: 1}})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 400 || httpErr.Problem == nil {
		t.Fatalf("err = %v, want HTTP 400 with ProblemDetails", err)
	}
	p := httpErr.Problem
	if *p.Title != "One or more validation errors occurred." || *p.Status != 400 || string(p.Extensions["traceId"]) != `"00-abc-01"` || p.Extensions["errors"] == nil {
		t.Fatalf("Problem = %+v", p)
	}
	if httpErr.Body != problem || m.count("/api/Order/v2/query") != 1 {
		t.Fatal("400 keeps the body and is not retried")
	}
	if !strings.Contains(err.Error(), "One or more validation errors occurred.") {
		t.Fatalf("Error() = %q", err.Error())
	}
	// A plain-text 400 has no Problem.
	_, err = c.Auth.LoginKey(context.Background(), "", "")
	if !errors.As(err, &httpErr) || httpErr.Problem != nil || httpErr.Body != "userName is required" || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestPingTextAndJSON(t *testing.T) {
	m := newMockServer(t)
	c := m.client(t) // no token, no credentials: anonymous ping
	for _, reply := range []struct{ contentType, body string }{
		{"text/plain; charset=utf-8", "pong"},
		{"application/json; charset=utf-8", `"pong"`},
		{"text/json", "  \"pong\"\n"},
	} {
		m.handle("/api/Status/ping", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", reply.contentType)
			_, _ = w.Write([]byte(reply.body))
		})
		got, err := c.Status.Ping(context.Background())
		if err != nil || got != "pong" {
			t.Fatalf("Ping(%s %q) = %q, %v", reply.contentType, reply.body, got, err)
		}
	}
	for _, r := range m.all("/api/Status/ping") {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || len(r.Body) != 0 {
			t.Fatalf("anonymous ping request = %+v", r)
		}
	}
	// With a token, ping is authenticated; the escape hatch returns the text.
	c = m.client(t, WithToken("tok"))
	var out string
	if err := c.Do(context.Background(), OpStatusPing, nil, &out); err != nil || out != "pong" {
		t.Fatalf("Do ping = %q, %v", out, err)
	}
	if got := m.last("/api/Status/ping").Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q", got)
	}
	// A failing login does not prevent an anonymous ping.
	m.handle("/api/Auth/loginKey", okJSON(`{"success":false,"errorCode":3}`))
	c = m.client(t, WithCredentials("user", "bad"))
	if got, err := c.Status.Ping(context.Background()); err != nil || got != "pong" {
		t.Fatalf("Ping = %q, %v", got, err)
	}
}

func TestRefreshMarginCallsValidate(t *testing.T) {
	m := newMockServer(t)
	old := makeJWT(time.Now().Add(30*time.Minute), "old") // inside the 1 h margin
	renewed := makeJWT(time.Now().Add(24*time.Hour), "renewed")
	m.handle("/api/Auth/validate", okJSON(`{"success":true,"errorCode":0,"errorMessage":null,"newToken":"`+renewed+`"}`))
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithToken(old))

	for i := 0; i < 3; i++ {
		if _, err := c.Accounts.Search(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.count("/api/Auth/validate"); got != 1 {
		t.Fatalf("validate calls = %d, want 1", got)
	}
	v := m.last("/api/Auth/validate")
	if v.Method != http.MethodPost || len(v.Body) != 0 || v.Header.Get("Authorization") != "Bearer "+old || v.Header.Get("Content-Type") != "" {
		t.Fatalf("validate request = %+v", v)
	}
	for _, r := range m.all("/api/Account/search") {
		if r.Header.Get("Authorization") != "Bearer "+renewed {
			t.Fatalf("search used %q, want the renewed token", r.Header.Get("Authorization"))
		}
	}
}

func TestRefreshMarginValidateFailure(t *testing.T) {
	old := makeJWT(time.Now().Add(10*time.Minute), "old")

	// Without credentials the current, still-valid token stays in use.
	m := newMockServer(t)
	m.handle("/api/Auth/validate", okJSON(`{"success":false,"errorCode":1}`))
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithToken(old))
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := m.last("/api/Account/search").Header.Get("Authorization"); got != "Bearer "+old {
		t.Fatalf("search used %q", got)
	}

	// With credentials a failed validate leads to a login.
	m = newMockServer(t)
	fresh := makeJWT(time.Now().Add(24*time.Hour), "fresh")
	m.handleLogin(fresh)
	m.handle("/api/Auth/validate", status(http.StatusInternalServerError, ""))
	m.handle("/api/Account/search", okJSON(accountsOK))
	c = m.client(t, WithToken(old), WithCredentials("user", "key"))
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if m.count("/api/Auth/validate") != 1 || m.count("/api/Auth/loginKey") != 1 {
		t.Fatalf("validate %d, login %d", m.count("/api/Auth/validate"), m.count("/api/Auth/loginKey"))
	}
	if got := m.last("/api/Account/search").Header.Get("Authorization"); got != "Bearer "+fresh {
		t.Fatalf("search used %q", got)
	}
}

func TestExpiredToken(t *testing.T) {
	expired := makeJWT(time.Now().Add(-time.Minute), "expired")

	// Without credentials the expired token is sent and the server decides.
	m := newMockServer(t)
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithToken(expired))
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if m.count("/api/Auth/validate") != 0 || m.last("/api/Account/search").Header.Get("Authorization") != "Bearer "+expired {
		t.Fatal("an expired token without credentials must be sent as-is")
	}

	// With credentials the client logs in first.
	m = newMockServer(t)
	fresh := makeJWT(time.Now().Add(24*time.Hour), "fresh")
	m.handleLogin(fresh)
	m.handle("/api/Account/search", okJSON(accountsOK))
	c = m.client(t, WithToken(expired), WithCredentials("user", "key"))
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if m.count("/api/Auth/validate") != 0 || m.count("/api/Auth/loginKey") != 1 ||
		m.last("/api/Account/search").Header.Get("Authorization") != "Bearer "+fresh {
		t.Fatal("an expired token with credentials must lead to a login")
	}
}

func TestExplicitValidateStoresNewToken(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Auth/validate", sequence(
		okJSON(`{"success":true,"errorCode":0,"newToken":"renewed"}`),
		okJSON(`{"success":true,"errorCode":0}`),
		okJSON(`{"success":false,"errorCode":3,"errorMessage":"expired"}`),
	))
	c := m.client(t, WithToken("original"))
	if tok, err := c.Auth.Validate(context.Background()); err != nil || tok != "renewed" || c.sess.current() != "renewed" {
		t.Fatalf("Validate = %q, %v", tok, err)
	}
	if tok, err := c.Auth.Validate(context.Background()); err != nil || tok != "renewed" {
		t.Fatalf("Validate without newToken = %q, %v", tok, err)
	}
	var apiErr *APIError
	if _, err := c.Auth.Validate(context.Background()); !errors.As(err, &apiErr) || apiErr.CodeName != "ExpiredToken" {
		t.Fatalf("err = %v", err)
	}
}

func TestLogoutClearsToken(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Auth/logout", okJSON(`{"success":true,"errorCode":0,"errorMessage":null,"hasBusinessFailure":false}`))
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithToken("tok"))

	if err := c.Auth.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := m.last("/api/Auth/logout")
	if r.Method != http.MethodPost || len(r.Body) != 0 || r.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("logout request = %+v", r)
	}
	if c.sess.current() != "" {
		t.Fatal("logout must clear the token")
	}
	if _, err := c.Accounts.Search(context.Background(), true); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("after logout: %v", err)
	}
	if err := c.Auth.Logout(context.Background()); err != nil || m.count("/api/Auth/logout") != 1 {
		t.Fatal("logout without a token is a no-op")
	}

	// A business failure is returned, the token is cleared anyway, and with
	// credentials the next call logs in again.
	m.handle("/api/Auth/logout", okJSON(`{"success":false,"errorCode":1}`))
	m.handleLogin("new")
	c = m.client(t, WithToken("tok"), WithCredentials("user", "key"))
	var apiErr *APIError
	if err := c.Auth.Logout(context.Background()); !errors.As(err, &apiErr) || apiErr.CodeName != "InvalidSession" {
		t.Fatalf("err = %v", err)
	}
	if c.sess.current() != "" {
		t.Fatal("logout must clear the token whatever the result")
	}
	if _, err := c.Accounts.Search(context.Background(), true); err != nil || m.count("/api/Auth/loginKey") != 1 {
		t.Fatalf("search after logout: %v", err)
	}
}

func TestLoginAppStoresToken(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Auth/loginApp", okJSON(`{"success":true,"errorCode":0,"token":"app-token"}`))
	c := m.client(t)
	tok, err := c.Auth.LoginApp(context.Background(), LoginAppRequest{UserName: "u", Password: "p", DeviceID: "d", AppID: "a", VerifyKey: "v"})
	if err != nil || tok != "app-token" || c.sess.current() != "app-token" {
		t.Fatalf("LoginApp = %q, %v", tok, err)
	}
	m.handle("/api/Auth/loginApp", okJSON(`{"success":true,"errorCode":0,"token":null}`))
	var decodeErr *DecodeError
	if _, err := c.Auth.LoginApp(context.Background(), LoginAppRequest{}); !errors.As(err, &decodeErr) {
		t.Fatalf("login without token: %v", err)
	}
}

func TestSingleFlightLogin(t *testing.T) {
	m := newMockServer(t)
	var logins atomic.Int32
	token := makeJWT(time.Now().Add(24*time.Hour), "shared")
	m.handle("/api/Auth/loginKey", func(w http.ResponseWriter, _ *http.Request) {
		logins.Add(1)
		time.Sleep(50 * time.Millisecond)
		writeJSON(w, http.StatusOK, `{"success":true,"errorCode":0,"token":"`+token+`"}`)
	})
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithCredentials("user", "key"))

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Accounts.Search(context.Background(), true)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := logins.Load(); got != 1 {
		t.Fatalf("logins = %d, want 1 (single-flight)", got)
	}
}

func TestSingleFlightValidateAndRelogin(t *testing.T) {
	m := newMockServer(t)
	var validates, logins atomic.Int32
	renewed := makeJWT(time.Now().Add(24*time.Hour), "renewed")
	m.handle("/api/Auth/validate", func(w http.ResponseWriter, _ *http.Request) {
		validates.Add(1)
		time.Sleep(50 * time.Millisecond)
		writeJSON(w, http.StatusOK, `{"success":true,"errorCode":0,"newToken":"`+renewed+`"}`)
	})
	m.handle("/api/Auth/loginKey", func(w http.ResponseWriter, _ *http.Request) {
		logins.Add(1)
		time.Sleep(50 * time.Millisecond)
		writeJSON(w, http.StatusOK, `{"success":true,"errorCode":0,"token":"relogged"}`)
	})
	var searches atomic.Int32
	m.handle("/api/Account/search", func(w http.ResponseWriter, r *http.Request) {
		// Every request with the renewed token is rejected once.
		if r.Header.Get("Authorization") == "Bearer "+renewed {
			searches.Add(1)
			writeJSON(w, http.StatusUnauthorized, "")
			return
		}
		writeJSON(w, http.StatusOK, accountsOK)
	})
	c := m.client(t, WithToken(makeJWT(time.Now().Add(time.Minute), "old")), WithCredentials("user", "key"))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Accounts.Search(context.Background(), true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if validates.Load() != 1 || logins.Load() != 1 {
		t.Fatalf("validates = %d, logins = %d; want 1 each", validates.Load(), logins.Load())
	}
}

// When the caller running a shared login gives up, callers waiting on it
// retry with their own context instead of inheriting the cancellation.
func TestSingleFlightLeaderCancellation(t *testing.T) {
	m := newMockServer(t)
	var logins atomic.Int32
	m.handle("/api/Auth/loginKey", func(w http.ResponseWriter, r *http.Request) {
		if logins.Add(1) == 1 {
			<-r.Context().Done() // the first login hangs until its caller gives up
			return
		}
		writeJSON(w, http.StatusOK, `{"success":true,"errorCode":0,"token":"tok"}`)
	})
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithCredentials("user", "key"))

	leaderCtx, cancel := context.WithCancel(context.Background())
	leader := make(chan error, 1)
	go func() {
		_, err := c.Accounts.Search(leaderCtx, true)
		leader <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for logins.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	follower := make(chan error, 1)
	go func() {
		_, err := c.Accounts.Search(context.Background(), true)
		follower <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the follower join the flight
	cancel()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader err = %v, want context.Canceled", err)
	}
	if err := <-follower; err != nil {
		t.Fatalf("follower err = %v, want success after its own login", err)
	}
	if got := logins.Load(); got != 2 {
		t.Fatalf("logins = %d, want 2", got)
	}
	c.CloseIdleConnections()
}

func TestDecimalsOnTheWire(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Order/place", okJSON(`{"success":true,"errorCode":0,"orderId":9007199254740993}`))
	m.handle("/api/Order/modify", okJSON(`{"success":true,"errorCode":0}`))
	m.handle("/api/Order/searchOpen", okJSON(`{"success":true,"errorCode":0,"orders":[{"id":1,"accountId":2,"contractId":"C","symbolId":"S","creationTimestamp":"2025-07-01T14:00:00.1234567","updateTimestamp":null,"status":99,"type":1,"side":0,"size":1,"limitPrice":6000.25,"fillVolume":0}]}`))
	c := m.client(t, WithToken("tok"))
	ctx := context.Background()

	req := NewLimitOrder(123, "CON.F.US.EP.U25", OrderSideBid, 1, decimal.RequireFromString("6000.25")).
		WithCustomTag("tag-1").WithStopLossBracket(8, OrderTypeStop).WithTakeProfitBracket(16, OrderTypeLimit)
	id, err := c.Orders.Place(ctx, req)
	if err != nil || id != 9007199254740993 {
		t.Fatalf("Place = %d, %v", id, err)
	}
	body := string(m.last("/api/Order/place").Body)
	want := `{"accountId":123,"contractId":"CON.F.US.EP.U25","type":1,"side":0,"size":1,"limitPrice":6000.25,"customTag":"tag-1","stopLossBracket":{"ticks":8,"type":4},"takeProfitBracket":{"ticks":16,"type":1}}`
	if body != want {
		t.Fatalf("place body\n got %s\nwant %s", body, want)
	}

	err = c.Orders.Modify(ctx, ModifyOrderRequest{AccountID: 1, OrderID: 2, TrailPrice: Ptr(decimal.RequireFromString("5998.50"))})
	if err != nil {
		t.Fatal(err)
	}
	if body := string(m.last("/api/Order/modify").Body); body != `{"accountId":1,"orderId":2,"trailPrice":5998.5}` {
		t.Fatalf("modify body %s", body)
	}

	orders, err := c.Orders.SearchOpen(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	o := orders[0]
	if o.LimitPrice.String() != "6000.25" || o.UpdateTimestamp != nil || o.Status.String() != "Unrecognized(99)" ||
		!o.CreationTimestamp.Equal(time.Date(2025, 7, 1, 14, 0, 0, 123456700, time.UTC)) {
		t.Fatalf("order = %+v", o)
	}
}

func TestTimestampsOnTheWire(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/History/retrieveBars", okJSON(`{"success":true,"errorCode":0,"bars":[{"t":"2025-07-01T16:00:00+02:00","o":6000,"h":6002.5,"l":5999,"c":6001,"v":125,"d":"2025-07-01","k":3}]}`))
	c := m.client(t, WithToken("tok"))
	start := time.Date(2025, 7, 1, 2, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	bars, err := c.History.RetrieveBars(context.Background(), RetrieveBarRequest{
		ContractID: "C", StartTime: start, EndTime: start.Add(24 * time.Hour), Unit: AggregateBarUnitMinute, UnitNumber: 5, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(m.last("/api/History/retrieveBars").Body)
	if !strings.Contains(body, `"startTime":"2025-07-01T00:00:00Z"`) || !strings.Contains(body, `"endTime":"2025-07-02T00:00:00Z"`) {
		t.Fatalf("timestamps must be sent as RFC 3339 UTC with Z: %s", body)
	}
	b := bars[0]
	if !b.Time.Equal(time.Date(2025, 7, 1, 14, 0, 0, 0, time.UTC)) || b.Time.Location() != time.UTC ||
		b.High.String() != "6002.5" || b.Volume != 125 || *b.D != "2025-07-01" || *b.K != 3 {
		t.Fatalf("bar = %+v", b)
	}
}

func TestResponseParsingIsTolerant(t *testing.T) {
	m := newMockServer(t)
	// text/plain content type, unknown members, absent hasBusinessFailure and a missing array.
	m.handle("/api/Contract/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"success\":true,\"errorCode\":0,\"brandNew\":{\"x\":1}}"))
	})
	c := m.client(t, WithToken("tok"))
	contracts, err := c.Contracts.Search(context.Background(), "ES", false)
	if err != nil || len(contracts) != 0 {
		t.Fatalf("Search = %v, %v", contracts, err)
	}
	var resp SearchContractResponse
	if err := c.Do(context.Background(), OpContractSearchContracts, SearchContractRequest{SearchText: "ES"}, &resp); err != nil || resp.HasBusinessFailure || resp.Contracts != nil {
		t.Fatalf("Do = %+v, %v", resp, err)
	}
}

func TestDecodeErrorCarriesBody(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Account/search", okJSON("<html>gateway</html>"))
	m.handle("/api/Auth/loginKey", okJSON(`{"success":"yes","token":"secret-token-value"}`))
	c := m.client(t, WithToken("tok"))
	_, err := c.Accounts.Search(context.Background(), true)
	var decodeErr *DecodeError
	if !errors.As(err, &decodeErr) || decodeErr.Body != "<html>gateway</html>" || decodeErr.StatusCode != 200 {
		t.Fatalf("err = %v", err)
	}
	if m.count("/api/Account/search") != 1 {
		t.Fatal("decode errors are not retried")
	}
	// Bodies of token-bearing responses are kept without the token.
	_, err = c.Auth.LoginKey(context.Background(), "user", "key")
	if !errors.As(err, &decodeErr) || strings.Contains(decodeErr.Body, "secret-token-value") || !strings.Contains(decodeErr.Body, `"token":"[REDACTED]"`) {
		t.Fatalf("err = %v, body %q", err, decodeErr.Body)
	}
	if strings.Contains(err.Error(), "secret-token-value") {
		t.Fatal("error message leaks the token")
	}
}

func TestMissingPayloadIsDecodeError(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Order/place", okJSON(`{"success":true,"errorCode":0}`))
	m.handle("/api/Order/searchById", okJSON(`{"success":true,"errorCode":0}`))
	m.handle("/api/Contract/searchById", okJSON(`{"success":true,"errorCode":0}`))
	c := m.client(t, WithToken("tok"))
	var decodeErr *DecodeError
	if _, err := c.Orders.Place(context.Background(), NewMarketOrder(1, "C", OrderSideBid, 1)); !errors.As(err, &decodeErr) {
		t.Fatalf("Place: %v", err)
	}
	if _, err := c.Orders.SearchByID(context.Background(), 1, 2); !errors.As(err, &decodeErr) {
		t.Fatalf("SearchByID: %v", err)
	}
	if _, err := c.Contracts.SearchByID(context.Background(), "C"); !errors.As(err, &decodeErr) {
		t.Fatalf("Contracts.SearchByID: %v", err)
	}
}

func TestClientSideRateLimiting(t *testing.T) {
	m := newMockServer(t)
	m.handle("/api/Account/search", okJSON(accountsOK))
	m.handle("/api/History/retrieveBars", okJSON(`{"success":true,"errorCode":0,"bars":[]}`))
	window := 300 * time.Millisecond
	c := m.client(t, WithToken("tok"), WithRateLimits(RateLimits{
		History: RateLimit{Requests: 1, Window: window},
		General: RateLimit{Requests: 2, Window: window},
	}))
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Accounts.Search(ctx, true); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < window-20*time.Millisecond {
		t.Fatalf("third request after %v; the sliding window must delay it by about %v", elapsed, window)
	}
	// The history bucket is separate from the general one.
	start = time.Now()
	if _, err := c.History.RetrieveBars(ctx, RetrieveBarRequest{Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > window/2 {
		t.Fatalf("history request waited %v on the general bucket", elapsed)
	}
	// A waiting caller honors its context.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := c.History.RetrieveBars(ctx, RetrieveBarRequest{Limit: 1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded while waiting for a slot", err)
	}

	// Disabled limiting never waits.
	c = m.client(t, WithToken("tok"), WithRateLimits(RateLimits{History: RateLimit{1, time.Hour}, General: RateLimit{1, time.Hour}}), WithoutRateLimiting())
	for i := 0; i < 5; i++ {
		if _, err := c.Accounts.Search(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRetriesAndLoginsTakeRateLimitSlots(t *testing.T) {
	m := newMockServer(t)
	m.handleLogin("tok")
	m.handle("/api/Account/search", sequence(status(503, ""), okJSON(accountsOK)))
	c := m.client(t, WithCredentials("user", "key"), WithRateLimits(RateLimits{
		History: RateLimit{Requests: 1, Window: time.Hour},
		General: RateLimit{Requests: 2, Window: time.Hour},
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// login + first attempt fill the window; the retry must wait for a slot.
	if _, err := c.Accounts.Search(ctx, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the retry to block on the limiter", err)
	}
	if m.count("/api/Auth/loginKey") != 1 || m.count("/api/Account/search") != 1 {
		t.Fatal("unexpected request counts")
	}
}

func TestNewClientOptionErrors(t *testing.T) {
	bad := map[string]Option{
		"empty credentials": WithCredentials("", "key"),
		"empty token":       WithToken(""),
		"bad base URL":      WithBaseURL("ftp://example.com"),
		"no host":           WithBaseURL("https://"),
		"ws base URL":       WithBaseURL("wss://example.com"),
		"bad hub URL":       WithUserHubURL(":/nope"),
		"bad market hub":    WithMarketHubURL("mailto:x"),
		"nil HTTP client":   WithHTTPClient(nil),
		"negative timeout":  WithTimeout(-1),
		"negative retries":  WithMaxRetries(-1),
		"bad backoff":       WithRetryBackoff(2*time.Second, time.Second),
		"bad rate limits":   WithRateLimits(RateLimits{General: RateLimit{Requests: 0, Window: time.Second}, History: RateLimit{1, time.Second}}),
		"negative margin":   WithRefreshMargin(-time.Second),
		"empty user agent":  WithUserAgent(""),
	}
	for name, opt := range bad {
		if _, err := NewClient(opt); err == nil {
			t.Errorf("%s: NewClient succeeded", name)
		}
	}
	c, err := NewClient(nil, WithLogger(nil), WithUserAgent("custom/1"), WithBaseURL("http://localhost:1/"),
		WithUserHubURL("https://hub.example/hubs/user"), WithMarketHubURL("wss://hub.example/hubs/market"),
		WithHTTPClient(&http.Client{}), WithRefreshMargin(0))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "http://localhost:1" || c.UserHubURL() != "https://hub.example/hubs/user" ||
		c.MarketHubURL() != "wss://hub.example/hubs/market" || c.Logger() == nil || c.userAgent != "custom/1" {
		t.Fatalf("client = %+v", c)
	}
	d, _ := NewClient()
	if d.BaseURL() != DefaultBaseURL || d.UserHubURL() != DefaultUserHubURL || d.MarketHubURL() != DefaultMarketHubURL ||
		d.timeout != 30*time.Second || d.maxRetries != 3 || d.backoff != time.Second || d.maxBackoff != 30*time.Second ||
		d.refreshMargin != time.Hour || !d.validation || d.limiters[RateBucketGeneral] == nil {
		t.Fatal("unexpected defaults")
	}
}

func TestDefaultHTTPClientConnectTimeout(t *testing.T) {
	hc := newDefaultHTTPClient()
	tr, ok := hc.Transport.(*http.Transport)
	if !ok || tr.TLSHandshakeTimeout != DefaultConnectTimeout || tr.DialContext == nil || hc.Timeout != 0 {
		t.Fatalf("transport = %+v", hc.Transport)
	}
}
