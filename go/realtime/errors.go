package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
)

// Sentinel errors, for use with errors.Is.
var (
	// ErrNotConnected is returned by invocations while the hub is not
	// connected, including while it reconnects. They fail fast rather than
	// wait.
	ErrNotConnected = errors.New("realtime: not connected")

	// ErrConnectionLost is returned by invocations pending when the
	// connection dropped, and wraps the cause reported in StateReconnecting
	// events.
	ErrConnectionLost = errors.New("realtime: connection lost")

	// ErrClosed is returned after Close, and by invocations that Close
	// interrupted.
	ErrClosed = errors.New("realtime: hub closed")

	// ErrAlreadyConnected is returned by Connect on a hub that is connected
	// or connecting.
	ErrAlreadyConnected = errors.New("realtime: already connected")

	// ErrInvocationTimeout is returned when no completion arrives within the
	// invocation timeout (30 s by default).
	ErrInvocationTimeout = errors.New("realtime: invocation timed out")

	// ErrServerTimeout is the connection-loss cause when nothing arrives from
	// the server within the server timeout (30 s by default).
	ErrServerTimeout = errors.New("realtime: server timeout: no message received")

	// ErrUnauthorized is matched by a *DialError whose WebSocket upgrade was
	// rejected with HTTP 401. It is the same value as topstepx.ErrUnauthorized,
	// so either name matches.
	ErrUnauthorized = topstepx.ErrUnauthorized
)

// HubError is an invocation the server completed with an error.
type HubError struct {
	// Method is the invoked hub method.
	Method string
	// Message is the server's error text.
	Message string
}

// Error describes the failure.
func (e *HubError) Error() string {
	return fmt.Sprintf("realtime: %s failed: %s", e.Method, e.Message)
}

// HandshakeError is a SignalR handshake that the server rejected or that did
// not complete.
type HandshakeError struct {
	// Message is the server's error text or the reason the handshake failed.
	Message string
}

// Error describes the failure.
func (e *HandshakeError) Error() string {
	return "realtime: handshake failed: " + e.Message
}

// ServerCloseError is the cause reported when the server sends a SignalR
// close message. The hub reconnects only when AllowReconnect is true.
type ServerCloseError struct {
	// Message is the server's error text, empty when none was sent.
	Message string
	// AllowReconnect is the server's allowReconnect flag.
	AllowReconnect bool
}

// Error describes the close.
func (e *ServerCloseError) Error() string {
	msg := "realtime: server closed the connection"
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.AllowReconnect {
		msg += " (reconnect allowed)"
	}
	return msg
}

// DialError is a failed WebSocket connection attempt. Its message never
// contains the access token. An upgrade rejected with HTTP 401 matches
// ErrUnauthorized; the rejected token has then been reported through
// WithTokenRejected.
type DialError struct {
	// StatusCode is the HTTP status of a rejected upgrade request, or 0.
	StatusCode int
	msg        string
	cause      error // only context errors, so no secrets are reachable
}

// Error describes the failure.
func (e *DialError) Error() string { return e.msg }

// Is reports whether target is ErrUnauthorized and the upgrade was rejected
// with HTTP 401.
func (e *DialError) Is(target error) bool {
	return target == ErrUnauthorized && e.StatusCode == http.StatusUnauthorized
}

// Unwrap returns context.Canceled or context.DeadlineExceeded when the dial
// ended because of its context, and nil otherwise.
func (e *DialError) Unwrap() error { return e.cause }

var accessTokenParam = regexp.MustCompile(`(?i)(access_token=)[^&\s"']*`)

// sanitizeDialError rewrites a dial error so that the token, which is part of
// the URL, cannot leak through the message or the unwrap chain.
func sanitizeDialError(err error, resp *http.Response, token string) error {
	msg := err.Error()
	for _, s := range []string{token, url.QueryEscape(token), url.PathEscape(token)} {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "[REDACTED]")
		}
	}
	msg = accessTokenParam.ReplaceAllString(msg, "${1}[REDACTED]")
	de := &DialError{msg: "realtime: connect: " + msg}
	if resp != nil {
		de.StatusCode = resp.StatusCode
		if resp.StatusCode == http.StatusUnauthorized {
			de.msg = "realtime: connect: token rejected (HTTP 401): " + msg
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		de.cause = context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		de.cause = context.DeadlineExceeded
	}
	return de
}

// lostError wraps cause so that it matches ErrConnectionLost.
func lostError(cause error) error {
	if cause == nil {
		return ErrConnectionLost
	}
	if errors.Is(cause, ErrConnectionLost) {
		return cause
	}
	return fmt.Errorf("%w: %w", ErrConnectionLost, cause)
}
