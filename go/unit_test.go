package topstepx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestJWTExpiry(t *testing.T) {
	exp := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	if got, ok := jwtExpiry(makeJWT(exp, "x")); !ok || !got.Equal(exp) {
		t.Fatalf("jwtExpiry = %v, %v; want %v", got, ok, exp)
	}
	enc := base64.RawURLEncoding.EncodeToString
	cases := map[string]bool{
		"not-a-jwt":                              false,
		"a.b":                                    false,
		"a.%%%.c":                                false,
		"h." + enc([]byte(`{"sub":"x"}`)) + ".s": false, // no exp
		"h." + enc([]byte(`{"exp":"soon"}`)) + ".s":       false,
		"h." + enc([]byte(`not json`)) + ".s":             false,
		"h." + enc([]byte(`{"exp":1751378400.5}`)) + ".s": true,
		// Padded and standard base64 are tolerated.
		"h." + base64.URLEncoding.EncodeToString([]byte(`{"exp":1751378400}`)) + ".s": true,
	}
	for tok, want := range cases {
		if _, ok := jwtExpiry(tok); ok != want {
			t.Errorf("jwtExpiry(%q) ok = %v, want %v", tok, ok, want)
		}
	}
	// Tokens without a decodable exp are assumed to last 24 hours.
	s := &session{}
	now := time.Now()
	s.store("opaque", now)
	if !s.expiry.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("expiry = %v", s.expiry)
	}
}

func TestDecideToken(t *testing.T) {
	now := time.Now()
	creds := &credentials{userName: "u", apiKey: "k"}
	cases := []struct {
		name   string
		token  string
		expiry time.Time
		creds  *credentials
		want   tokenAction
	}{
		{"no token, no creds", "", time.Time{}, nil, actFail},
		{"no token, creds", "", time.Time{}, creds, actLogin},
		{"expired, no creds", "t", now.Add(-time.Second), nil, actUse},
		{"expired now, creds", "t", now, creds, actLogin},
		{"inside margin", "t", now.Add(59 * time.Minute), nil, actValidate},
		{"at margin", "t", now.Add(time.Hour), creds, actValidate},
		{"outside margin", "t", now.Add(61 * time.Minute), creds, actUse},
	}
	for _, c := range cases {
		s := &session{creds: c.creds, token: c.token, expiry: c.expiry}
		if got := s.decideLocked(now, time.Hour); got != c.want {
			t.Errorf("%s: action %d, want %d", c.name, got, c.want)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"3", 3 * time.Second, true},
		{" 0 ", 0, true},
		{"1.5", 1500 * time.Millisecond, true},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second, true},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{"Tue, 29 Sep 2026 12:00:05 GMT", 5 * time.Second, true},
		{"", 0, false},
		{"-1", 0, false},
		{"soon", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRetryAfter(c.in, now)
		if got != c.want || ok != c.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if d, ok := parseRetryAfter("99999999999999999", now); !ok || d <= 0 {
		t.Errorf("huge Retry-After = %v, %v", d, ok)
	}
}

func TestBackoffDelay(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30, 30}
	for n, w := range want {
		if got := backoffDelay(time.Second, 30*time.Second, n); got != w*time.Second {
			t.Errorf("backoffDelay(%d) = %v, want %v", n, got, w*time.Second)
		}
	}
	if got := backoffDelay(time.Second, 30*time.Second, 200); got != 30*time.Second {
		t.Errorf("backoffDelay(200) = %v", got)
	}
	if got := backoffDelay(0, 0, 3); got != 0 {
		t.Errorf("zero backoff = %v", got)
	}
}

func TestSlidingWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	w := newSlidingWindow(RateLimit{Requests: 3, Window: 10 * time.Second}, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if wait := w.tryAcquire(); wait != 0 {
			t.Fatalf("slot %d: wait %v", i, wait)
		}
		now = now.Add(time.Second)
	}
	// Window full: wait until the oldest entry (t=1000) expires at t=1010.
	if wait := w.tryAcquire(); wait != 7*time.Second {
		t.Fatalf("wait = %v, want 7s", wait)
	}
	now = time.Unix(1010, 0)
	if wait := w.tryAcquire(); wait != 0 {
		t.Fatalf("after expiry: wait %v", wait)
	}
	// Entries at 1001, 1002 and 1010 remain: the next slot frees at 1011.
	if wait := w.tryAcquire(); wait != time.Second {
		t.Fatalf("wait = %v, want 1s (sliding, not fixed, window)", wait)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire on a full window with a canceled context: %v", err)
	}
	if err := DefaultRateLimits().General.validate("general"); err != nil {
		t.Fatal(err)
	}
	if d := DefaultRateLimits(); d.History.Requests != 50 || d.History.Window != 30*time.Second || d.General.Requests != 200 || d.General.Window != time.Minute {
		t.Fatalf("DefaultRateLimits = %+v", d)
	}
}

func TestEnumsAndOperationsStrings(t *testing.T) {
	if OrderTypeTrailingStop.String() != "TrailingStop" || OrderStatus(42).String() != "Unrecognized(42)" {
		t.Fatal("enum String")
	}
	var o Order
	if err := json.Unmarshal([]byte(`{"type":12,"side":7,"status":-3}`), &o); err != nil {
		t.Fatal(err)
	}
	if o.Type.IsKnown() || o.Side.String() != "Unrecognized(7)" || o.Status.String() != "Unrecognized(-3)" {
		t.Fatalf("unknown enum values must be tolerated: %+v", o)
	}
	for _, s := range []fmt.Stringer{AuthNone, AuthBearer, AuthBearerIfAvailable, AuthMode(9), RateBucketGeneral,
		RateBucketHistory, RateBucket(9), RetryRead, RetryWrite, RetryClass(9), OpStatusPing} {
		if s.String() == "" {
			t.Errorf("%#v has an empty name", s)
		}
	}
	if _, ok := OperationID("nope").Info(); ok {
		t.Fatal("unknown operation found")
	}
	ops := Operations()
	ops[0].Path = "/mutated"
	if info, _ := ops[1].ID.Info(); Operations()[0].Path == "/mutated" || info.ID != ops[1].ID {
		t.Fatal("Operations must return a copy")
	}
}

func TestErrorMessagesAndMatching(t *testing.T) {
	msg := "boom"
	apiErr := &APIError{Operation: OpOrderPlaceOrder, Code: 2, CodeName: "OrderRejected", Message: &msg, OrderID: Ptr(int64(9))}
	if got := apiErr.Error(); got != "topstepx: Order_PlaceOrder (POST /api/Order/place) failed: errorCode 2 (OrderRejected): boom [orderId 9]" {
		t.Fatalf("APIError = %q", got)
	}
	httpErr := &HTTPError{Operation: OpStatusPing, StatusCode: 401, Body: strings.Repeat("x", 300)}
	if !errors.Is(httpErr, ErrUnauthorized) || !strings.HasSuffix(httpErr.Error(), "...") {
		t.Fatalf("HTTPError = %q", httpErr.Error())
	}
	if errors.Is(&HTTPError{StatusCode: 403}, ErrUnauthorized) {
		t.Fatal("403 is not ErrUnauthorized")
	}
	d := 2 * time.Second
	if rl := (&RateLimitError{Operation: OpHistoryGetBars, RetryAfter: &d}); !errors.Is(rl, ErrRateLimited) || !strings.Contains(rl.Error(), "2s") {
		t.Fatalf("RateLimitError = %q", rl.Error())
	}
	ve := &ValidationError{Field: "size", Reason: "too small"}
	if ve.Error() != "topstepx: invalid request: size: too small" {
		t.Fatalf("ValidationError = %q", ve.Error())
	}
	cause := errors.New("eof")
	if de := (&DecodeError{Operation: OpAccountSearchAccounts, StatusCode: 200, Body: "{", Err: cause}); !errors.Is(de, cause) || strings.Contains(de.Error(), "{") {
		t.Fatalf("DecodeError = %q", de.Error())
	}
	if te := (&TransportError{Operation: OpAccountSearchAccounts, Err: context.DeadlineExceeded}); !errors.Is(te, context.DeadlineExceeded) {
		t.Fatal("TransportError must unwrap")
	}
	if got := describeOp("Custom"); got != "Custom" {
		t.Fatalf("describeOp = %q", got)
	}
}

func TestProblemDetailsRoundTrip(t *testing.T) {
	in := `{"title":"Bad","status":400,"traceId":"t-1","errors":{"a":["b"]},"nothing":null}`
	var p ProblemDetails
	if err := json.Unmarshal([]byte(in), &p); err != nil {
		t.Fatal(err)
	}
	if *p.Title != "Bad" || *p.Status != 400 || len(p.Extensions) != 3 {
		t.Fatalf("ProblemDetails = %+v", p)
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	assertEquivalentJSON(t, []byte(in), out, "problem")
	empty, _ := json.Marshal(ProblemDetails{Extensions: map[string]json.RawMessage{"x": json.RawMessage(`1`)}})
	if string(empty) != `{"x":1}` {
		t.Fatalf("extensions only = %s", empty)
	}
	if parseProblem([]byte(`{"unrelated":true}`)) != nil || parseProblem([]byte(`[1]`)) != nil || parseProblem(nil) != nil {
		t.Fatal("non-problem bodies must not parse as ProblemDetails")
	}
}

// Secrets must never appear in fmt output, slog output or error messages.
func TestSecretsAreNeverPrinted(t *testing.T) {
	const apiKey, token, password, verifyKey = "sEcReT-aPi-KeY", "tOkEn-SeCrEt", "pAsSwOrD-SeCrEt", "vErIfY-SeCrEt"
	c, err := NewClient(WithCredentials("user", apiKey), WithToken(token))
	if err != nil {
		t.Fatal(err)
	}
	values := []any{
		c, *c, c.sess, c.sess.creds, c.Auth,
		LoginAPIKeyRequest{UserName: "user", APIKey: apiKey},
		&LoginAPIKeyRequest{UserName: "user", APIKey: apiKey},
		LoginAppRequest{UserName: "user", Password: password, VerifyKey: verifyKey},
		LoginResponse{Success: true, Token: Ptr(token)},
		&ValidateResponse{Success: true, NewToken: Ptr(token)},
		&APIError{Operation: OpAuthLoginKey, Response: &LoginResponse{Token: Ptr(token)}},
	}
	secrets := []string{apiKey, token, password, verifyKey}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	text := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, v := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			fmt.Fprintf(&buf, verb+"\n", v)
		}
		logger.Info("value", "v", v)
		text.Info("value", "v", v)
	}
	for _, s := range secrets {
		if strings.Contains(buf.String(), s) {
			t.Fatalf("secret %q printed:\n%s", s, buf.String())
		}
	}
	if !strings.Contains(buf.String(), "[REDACTED]") {
		t.Fatal("expected redaction markers")
	}
}

// A full session with debug logging must not log secrets either.
func TestSecretsNotLogged(t *testing.T) {
	const apiKey = "log-sEcReT-aPi-KeY"
	token1, token2 := makeJWT(time.Now().Add(24*time.Hour), "one"), makeJWT(time.Now().Add(24*time.Hour), "two")
	m := newMockServer(t)
	m.handleLogin(token1, token2)
	m.handle("/api/Account/search", sequence(status(401, ""), status(503, ""), okJSON(accountsOK)))
	var buf bytes.Buffer
	c := m.client(t, WithCredentials("user", apiKey), WithLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	if _, err := c.Accounts.Search(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected debug logs")
	}
	for _, s := range []string{apiKey, token1, token2} {
		if strings.Contains(buf.String(), s) {
			t.Fatalf("secret logged:\n%s", buf.String())
		}
	}
}

func TestInvalidateToken(t *testing.T) {
	m := newMockServer(t)
	m.handleLogin("fresh")
	m.handle("/api/Account/search", okJSON(accountsOK))
	c := m.client(t, WithToken("stale"), WithCredentials("user", "key"))
	ctx := context.Background()

	// Only the stored token is discarded; a stale report is ignored.
	c.InvalidateToken("other")
	c.InvalidateToken("")
	if got := c.sess.current(); got != "stale" {
		t.Fatalf("token = %q, want it untouched", got)
	}
	c.InvalidateToken("stale")
	if got := c.sess.current(); got != "" {
		t.Fatalf("token = %q, want it discarded", got)
	}
	if tok, err := c.Token(ctx); err != nil || tok != "fresh" || m.count("/api/Auth/loginKey") != 1 {
		t.Fatalf("Token = %q, %v; want a new login", tok, err)
	}
	c.InvalidateToken("stale") // already replaced
	if got := c.sess.current(); got != "fresh" {
		t.Fatalf("a stale report discarded the new token: %q", got)
	}

	// Without credentials the next call fails before any I/O.
	c = m.client(t, WithToken("only"))
	c.InvalidateToken("only")
	if _, err := c.Token(ctx); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("Token = %v, want ErrNotAuthenticated", err)
	}
	if _, err := c.Accounts.Search(ctx, true); !errors.Is(err, ErrNotAuthenticated) || m.count("/api/Account/search") != 0 {
		t.Fatalf("Search = %v", err)
	}
}
