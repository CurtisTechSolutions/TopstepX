package realtime

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/CurtisTechSolutions/TopstepX/go/internal/logx"
)

// Defaults from the cross-SDK contract (section 11).
const (
	// DefaultKeepAliveInterval is how long the client may stay silent before
	// it sends a ping.
	DefaultKeepAliveInterval = 15 * time.Second
	// DefaultServerTimeout is how long the server may stay silent before the
	// connection counts as lost.
	DefaultServerTimeout = 30 * time.Second
	// DefaultHandshakeTimeout bounds the WebSocket dial and SignalR handshake.
	DefaultHandshakeTimeout = 15 * time.Second
	// DefaultInvocationTimeout bounds each hub method invocation.
	DefaultInvocationTimeout = 30 * time.Second
	// DefaultEventBuffer is the capacity of the Events channel.
	DefaultEventBuffer = 1024
	// DefaultReadLimit is the largest accepted WebSocket message, in bytes.
	DefaultReadLimit = 16 << 20
	// UnlimitedAttempts is the ReconnectPolicy.MaxAttempts value for
	// unlimited reconnect attempts.
	UnlimitedAttempts = -1
)

// writeTimeout bounds each WebSocket write; closeTimeout bounds the close
// message sent by Close.
const (
	writeTimeout = 10 * time.Second
	closeTimeout = 2 * time.Second
)

// TokenProvider returns the access token for a connection attempt. It is
// called before every attempt, so each reconnect uses a fresh token. Pass
// (*topstepx.Client).Token.
type TokenProvider func(ctx context.Context) (string, error)

// ReconnectPolicy controls automatic reconnection after a lost connection or
// a server close that allows reconnecting.
type ReconnectPolicy struct {
	// Delays are the waits before successive attempts; the last one repeats
	// for later attempts. Empty means the default delays.
	Delays []time.Duration
	// MaxAttempts caps the attempts per outage. UnlimitedAttempts (any
	// negative value) removes the cap; zero disables reconnection.
	MaxAttempts int
}

// DefaultReconnectPolicy returns the contract policy: delays of 0, 1, 2, 5
// and 10 seconds, then every 30 seconds, with unlimited attempts.
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{
		Delays:      []time.Duration{0, time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second},
		MaxAttempts: UnlimitedAttempts,
	}
}

func (p ReconnectPolicy) enabled() bool { return p.MaxAttempts != 0 }

// delay returns the wait before the 1-based attempt.
func (p ReconnectPolicy) delay(attempt int) time.Duration {
	delays := p.Delays
	if len(delays) == 0 {
		delays = DefaultReconnectPolicy().Delays
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

// OverflowPolicy decides which event is lost when the consumer falls behind
// and the Events buffer is full. Delivery never blocks the connection.
type OverflowPolicy int

// OverflowPolicy values.
const (
	// DropOldest evicts the oldest buffered event to make room (default).
	DropOldest OverflowPolicy = iota
	// DropNewest discards the incoming data event. State events are always
	// enqueued, evicting the oldest buffered event if needed.
	DropNewest
)

// Option configures a hub.
type Option func(*config)

type config struct {
	url               string
	httpClient        *http.Client
	logger            *slog.Logger
	eventBuffer       int
	overflow          OverflowPolicy
	reconnect         ReconnectPolicy
	keepAlive         time.Duration
	serverTimeout     time.Duration
	handshakeTimeout  time.Duration
	invocationTimeout time.Duration
	readLimit         int64
	tokenRejected     func(token string)
}

func newConfig(defaultURL string, opts []Option) config {
	cfg := config{
		url:               defaultURL,
		logger:            logx.Discard(),
		eventBuffer:       DefaultEventBuffer,
		overflow:          DropOldest,
		reconnect:         DefaultReconnectPolicy(),
		keepAlive:         DefaultKeepAliveInterval,
		serverTimeout:     DefaultServerTimeout,
		handshakeTimeout:  DefaultHandshakeTimeout,
		invocationTimeout: DefaultInvocationTimeout,
		readLimit:         DefaultReadLimit,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// WithURL sets the hub URL. An https or http URL is converted to wss or ws.
func WithURL(hubURL string) Option {
	return func(c *config) {
		if hubURL != "" {
			c.url = hubURL
		}
	}
}

// WithHTTPClient sets the HTTP client used for the WebSocket upgrade.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) { c.httpClient = hc }
}

// WithLogger sets the logger. The default discards everything. Tokens are
// never logged.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = logx.OrDiscard(l) }
}

// WithEventBuffer sets the capacity of the Events channel (default
// DefaultEventBuffer, minimum 1).
func WithEventBuffer(n int) Option {
	return func(c *config) {
		if n < 1 {
			n = 1
		}
		c.eventBuffer = n
	}
}

// WithOverflowPolicy sets which event is lost when the Events buffer is full
// (default DropOldest). Dropped events are counted by Dropped.
func WithOverflowPolicy(p OverflowPolicy) Option {
	return func(c *config) { c.overflow = p }
}

// WithReconnectPolicy sets the reconnect policy (default
// DefaultReconnectPolicy).
func WithReconnectPolicy(p ReconnectPolicy) Option {
	return func(c *config) {
		p.Delays = append([]time.Duration(nil), p.Delays...)
		c.reconnect = p
	}
}

// WithoutReconnect disables automatic reconnection.
func WithoutReconnect() Option {
	return func(c *config) { c.reconnect.MaxAttempts = 0 }
}

func positive(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

// WithKeepAliveInterval sets how long the client may stay silent before it
// sends a ping (default DefaultKeepAliveInterval).
func WithKeepAliveInterval(d time.Duration) Option {
	return func(c *config) { c.keepAlive = positive(d, DefaultKeepAliveInterval) }
}

// WithServerTimeout sets how long the server may stay silent before the
// connection counts as lost (default DefaultServerTimeout).
func WithServerTimeout(d time.Duration) Option {
	return func(c *config) { c.serverTimeout = positive(d, DefaultServerTimeout) }
}

// WithHandshakeTimeout bounds the WebSocket dial and SignalR handshake
// (default DefaultHandshakeTimeout).
func WithHandshakeTimeout(d time.Duration) Option {
	return func(c *config) { c.handshakeTimeout = positive(d, DefaultHandshakeTimeout) }
}

// WithInvocationTimeout bounds each hub method invocation (default
// DefaultInvocationTimeout).
func WithInvocationTimeout(d time.Duration) Option {
	return func(c *config) { c.invocationTimeout = positive(d, DefaultInvocationTimeout) }
}

// WithTokenRejected sets a function that receives the token of a
// connection attempt whose WebSocket upgrade the server rejected with HTTP
// 401. It is called before the next attempt, for the initial Connect and for
// every reconnect, so that the token source can discard the token instead of
// handing it out again until it expires. Pass
// (*topstepx.Client).InvalidateToken; NewUserHubForClient and
// NewMarketHubForClient do so automatically. The function must not block.
func WithTokenRejected(f func(token string)) Option {
	return func(c *config) { c.tokenRejected = f }
}

// WithReadLimit sets the largest accepted WebSocket message in bytes
// (default DefaultReadLimit).
func WithReadLimit(n int64) Option {
	return func(c *config) {
		if n > 0 {
			c.readLimit = n
		}
	}
}
