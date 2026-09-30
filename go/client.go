package topstepx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CurtisTechSolutions/TopstepX/go/internal/logx"
)

// Client is a ProjectX Gateway API client. It is safe for concurrent use.
//
// Endpoint groups are exposed as fields, for example client.Orders.Place.
// Client.Do is the low-level escape hatch for any operation.
type Client struct {
	// Auth manages the session: login, validate and logout.
	Auth *AuthService
	// Accounts searches trading accounts.
	Accounts *AccountsService
	// Contracts searches contracts.
	Contracts *ContractsService
	// History retrieves historical bars.
	History *HistoryService
	// Orders searches, places, cancels and modifies orders.
	Orders *OrdersService
	// Positions searches and closes positions.
	Positions *PositionsService
	// Trades searches trades.
	Trades *TradesService
	// Status checks API responsiveness.
	Status *StatusService

	baseURL       string // without trailing slash
	userHubURL    string
	marketHubURL  string
	httpClient    *http.Client
	timeout       time.Duration
	maxRetries    int
	backoff       time.Duration
	maxBackoff    time.Duration
	validation    bool
	refreshMargin time.Duration
	userAgent     string
	logger        *slog.Logger
	limiters      [2]*slidingWindow // indexed by RateBucket; nil when disabled
	sess          *session

	// Test hooks.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// Option configures a Client. Options are applied in order by NewClient.
type Option func(*settings) error

type settings struct {
	baseURL       string
	userHubURL    string
	marketHubURL  string
	httpClient    *http.Client
	timeout       time.Duration
	maxRetries    int
	backoff       time.Duration
	maxBackoff    time.Duration
	rateLimits    RateLimits
	rateLimiting  bool
	validation    bool
	refreshMargin time.Duration
	userAgent     string
	logger        *slog.Logger
	creds         *credentials
	token         string
}

// WithCredentials configures the platform username and API key used to log in
// (lazily, on the first authenticated call) and to log in again when the
// token expires or the gateway answers HTTP 401. The API key is never printed
// or logged.
func WithCredentials(userName, apiKey string) Option {
	return func(s *settings) error {
		if userName == "" || apiKey == "" {
			return errors.New("topstepx: WithCredentials requires a username and an API key")
		}
		s.creds = &credentials{userName: userName, apiKey: apiKey}
		return nil
	}
}

// WithToken configures a pre-existing session token, used instead of or in
// addition to credentials. Its expiry is read from the JWT exp claim, or
// assumed to be 24 hours away.
func WithToken(token string) Option {
	return func(s *settings) error {
		if token == "" {
			return errors.New("topstepx: WithToken requires a non-empty token")
		}
		s.token = token
		return nil
	}
}

func checkURL(option, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("topstepx: %s: %w", option, err)
	}
	switch u.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return fmt.Errorf("topstepx: %s: unsupported URL scheme %q", option, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("topstepx: %s: URL %q has no host", option, raw)
	}
	return nil
}

// WithBaseURL sets the REST base URL (default DefaultBaseURL). Other firms
// run the same gateway on other hosts.
func WithBaseURL(baseURL string) Option {
	return func(s *settings) error {
		if err := checkURL("WithBaseURL", baseURL); err != nil {
			return err
		}
		if strings.HasPrefix(baseURL, "ws") {
			return fmt.Errorf("topstepx: WithBaseURL: %q is not an HTTP URL", baseURL)
		}
		s.baseURL = baseURL
		return nil
	}
}

// WithUserHubURL sets the SignalR user hub URL reported by UserHubURL
// (default DefaultUserHubURL).
func WithUserHubURL(hubURL string) Option {
	return func(s *settings) error {
		if err := checkURL("WithUserHubURL", hubURL); err != nil {
			return err
		}
		s.userHubURL = hubURL
		return nil
	}
}

// WithMarketHubURL sets the SignalR market hub URL reported by MarketHubURL
// (default DefaultMarketHubURL).
func WithMarketHubURL(hubURL string) Option {
	return func(s *settings) error {
		if err := checkURL("WithMarketHubURL", hubURL); err != nil {
			return err
		}
		s.marketHubURL = hubURL
		return nil
	}
}

// WithHTTPClient sets the HTTP client. The default client uses a 10 second
// connect and TLS-handshake timeout.
func WithHTTPClient(c *http.Client) Option {
	return func(s *settings) error {
		if c == nil {
			return errors.New("topstepx: WithHTTPClient requires a non-nil client")
		}
		s.httpClient = c
		return nil
	}
}

// WithTimeout bounds each HTTP attempt (default DefaultTimeout). Zero
// disables the per-attempt timeout; the context still applies.
func WithTimeout(d time.Duration) Option {
	return func(s *settings) error {
		if d < 0 {
			return errors.New("topstepx: WithTimeout requires a non-negative duration")
		}
		s.timeout = d
		return nil
	}
}

// WithMaxRetries sets the retry budget (default DefaultMaxRetries). Zero
// disables retries. HTTP 401 re-login does not consume the budget.
func WithMaxRetries(n int) Option {
	return func(s *settings) error {
		if n < 0 {
			return errors.New("topstepx: WithMaxRetries requires a non-negative count")
		}
		s.maxRetries = n
		return nil
	}
}

// WithRetryBackoff sets the first retry delay and its cap (defaults
// DefaultRetryBackoff and DefaultMaxRetryBackoff). The delay doubles per
// retry and has no jitter. A Retry-After header on HTTP 429 takes precedence.
func WithRetryBackoff(initial, maxDelay time.Duration) Option {
	return func(s *settings) error {
		if initial < 0 || maxDelay < initial {
			return errors.New("topstepx: WithRetryBackoff requires 0 <= initial <= max")
		}
		s.backoff, s.maxBackoff = initial, maxDelay
		return nil
	}
}

// WithRateLimits replaces the client-side rate limits (default
// DefaultRateLimits) and enables limiting.
func WithRateLimits(limits RateLimits) Option {
	return func(s *settings) error {
		if err := limits.History.validate("history"); err != nil {
			return err
		}
		if err := limits.General.validate("general"); err != nil {
			return err
		}
		s.rateLimits = limits
		s.rateLimiting = true
		return nil
	}
}

// WithoutRateLimiting disables client-side rate limiting.
func WithoutRateLimiting() Option {
	return func(s *settings) error {
		s.rateLimiting = false
		return nil
	}
}

// WithoutValidation disables client-side request validation; the server
// remains the final authority.
func WithoutValidation() Option {
	return func(s *settings) error {
		s.validation = false
		return nil
	}
}

// WithRefreshMargin sets how long before expiry the token is renewed through
// Auth_Validate (default DefaultRefreshMargin). Zero renews only on expiry.
func WithRefreshMargin(d time.Duration) Option {
	return func(s *settings) error {
		if d < 0 {
			return errors.New("topstepx: WithRefreshMargin requires a non-negative duration")
		}
		s.refreshMargin = d
		return nil
	}
}

// WithUserAgent sets the User-Agent header (default DefaultUserAgent).
func WithUserAgent(userAgent string) Option {
	return func(s *settings) error {
		if userAgent == "" {
			return errors.New("topstepx: WithUserAgent requires a non-empty value")
		}
		s.userAgent = userAgent
		return nil
	}
}

// WithLogger sets the logger for request, retry and session diagnostics. The
// default discards everything. Secrets are never logged.
func WithLogger(l *slog.Logger) Option {
	return func(s *settings) error {
		s.logger = logx.OrDiscard(l)
		return nil
	}
}

// newDefaultHTTPClient returns an HTTP client with a 10 second connect and
// TLS-handshake timeout. Request timeouts are applied per attempt instead of
// through http.Client.Timeout.
func newDefaultHTTPClient() *http.Client {
	var transport *http.Transport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = t.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.DialContext = (&net.Dialer{Timeout: DefaultConnectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = DefaultConnectTimeout
	return &http.Client{Transport: transport}
}

// NewClient returns a client configured by opts. Without WithCredentials or
// WithToken, authenticated calls fail with ErrNotAuthenticated until
// Auth.LoginKey succeeds.
func NewClient(opts ...Option) (*Client, error) {
	s := settings{
		baseURL:       DefaultBaseURL,
		userHubURL:    DefaultUserHubURL,
		marketHubURL:  DefaultMarketHubURL,
		timeout:       DefaultTimeout,
		maxRetries:    DefaultMaxRetries,
		backoff:       DefaultRetryBackoff,
		maxBackoff:    DefaultMaxRetryBackoff,
		rateLimits:    DefaultRateLimits(),
		rateLimiting:  true,
		validation:    true,
		refreshMargin: DefaultRefreshMargin,
		userAgent:     DefaultUserAgent,
		logger:        logx.Discard(),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&s); err != nil {
			return nil, err
		}
	}
	if s.httpClient == nil {
		s.httpClient = newDefaultHTTPClient()
	}
	c := &Client{
		baseURL:       strings.TrimRight(s.baseURL, "/"),
		userHubURL:    s.userHubURL,
		marketHubURL:  s.marketHubURL,
		httpClient:    s.httpClient,
		timeout:       s.timeout,
		maxRetries:    s.maxRetries,
		backoff:       s.backoff,
		maxBackoff:    s.maxBackoff,
		validation:    s.validation,
		refreshMargin: s.refreshMargin,
		userAgent:     s.userAgent,
		logger:        s.logger,
		now:           time.Now,
		sleep:         sleepContext,
	}
	c.sess = &session{creds: s.creds}
	if s.token != "" {
		c.sess.store(s.token, c.now())
	}
	if s.rateLimiting {
		c.limiters[RateBucketGeneral] = newSlidingWindow(s.rateLimits.General, func() time.Time { return c.now() })
		c.limiters[RateBucketHistory] = newSlidingWindow(s.rateLimits.History, func() time.Time { return c.now() })
	}
	c.Auth = &AuthService{c: c}
	c.Accounts = &AccountsService{c: c}
	c.Contracts = &ContractsService{c: c}
	c.History = &HistoryService{c: c}
	c.Orders = &OrdersService{c: c}
	c.Positions = &PositionsService{c: c}
	c.Trades = &TradesService{c: c}
	c.Status = &StatusService{c: c}
	return c, nil
}

// Token returns a usable session token, logging in or renewing it first when
// needed (the contract's ensure_token). Its signature matches the realtime
// package's token provider, so client.Token can be passed to the hubs.
func (c *Client) Token(ctx context.Context) (string, error) {
	return c.ensureToken(ctx)
}

// InvalidateToken discards token when it is still the stored token, for
// example because the gateway rejected it. The next authenticated call, or
// Token, then logs in again when credentials are configured and fails with
// ErrNotAuthenticated otherwise. A token that was already replaced is
// ignored, so a stale report never discards a newer token.
//
// The realtime hubs call it when a WebSocket upgrade is rejected with HTTP
// 401 (see realtime.WithTokenRejected), so that the next connection attempt
// does not reuse the rejected token until it expires.
func (c *Client) InvalidateToken(token string) {
	if c.sess.invalidate(token) {
		c.logger.Info("topstepx: discarded a rejected token")
	}
}

// CloseIdleConnections closes idle keep-alive connections of the client's
// HTTP client. The Client stays usable.
func (c *Client) CloseIdleConnections() { c.httpClient.CloseIdleConnections() }

// BaseURL returns the REST base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// UserHubURL returns the configured SignalR user hub URL.
func (c *Client) UserHubURL() string { return c.userHubURL }

// MarketHubURL returns the configured SignalR market hub URL.
func (c *Client) MarketHubURL() string { return c.marketHubURL }

// Logger returns the client's logger.
func (c *Client) Logger() *slog.Logger { return c.logger }

// String describes the client without secrets.
func (c Client) String() string {
	creds := "none"
	if c.sess != nil && c.sess.creds != nil {
		creds = "configured"
	}
	return fmt.Sprintf("topstepx.Client{BaseURL: %q, Credentials: %s, Token: %s}", c.baseURL, creds, redacted)
}

// GoString describes the client without secrets.
func (c Client) GoString() string { return c.String() }

// Format implements fmt.Formatter so that no verb prints a secret, whether
// the client is formatted by value or by pointer.
func (c Client) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

// LogValue implements slog.LogValuer without secrets.
func (c Client) LogValue() slog.Value {
	return slog.GroupValue(slog.String("baseURL", c.baseURL), slog.Bool("credentials", c.sess != nil && c.sess.creds != nil))
}
