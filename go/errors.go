package topstepx

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Sentinel errors, for use with errors.Is.
var (
	// ErrUnauthorized matches the *HTTPError of an HTTP 401 response that
	// survived the single re-login attempt.
	ErrUnauthorized = errors.New("topstepx: unauthorized")

	// ErrNotAuthenticated is returned, before any network I/O, when an
	// authenticated operation runs with neither a token nor credentials.
	ErrNotAuthenticated = errors.New("topstepx: not authenticated: no token and no credentials configured")

	// ErrRateLimited matches every *RateLimitError.
	ErrRateLimited = errors.New("topstepx: rate limited")
)

// APIError is a business failure: a response with success == false or a
// non-zero errorCode. The gateway reports these with HTTP 200, including
// invalid login credentials (LoginErrorCodeInvalidCredentials).
type APIError struct {
	// Operation is the failed operation.
	Operation OperationID
	// Code is the raw errorCode.
	Code int
	// CodeName is the errorCode's name in the operation's error-code enum, or
	// "Unrecognized(<n>)" for a value the enum lacks.
	CodeName string
	// Message is the errorMessage, which the gateway may leave null.
	Message *string
	// OrderID is the orderId returned by Order_PlaceOrder, if any. A returned
	// identifier does not mean the order was accepted.
	OrderID *int64
	// Response is the complete decoded envelope, such as *PlaceOrderResponse.
	Response any
}

// Error describes the failure.
func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "topstepx: %s failed: errorCode %d (%s)", describeOp(e.Operation), e.Code, e.CodeName)
	if e.Message != nil && *e.Message != "" {
		fmt.Fprintf(&b, ": %s", *e.Message)
	}
	if e.OrderID != nil {
		fmt.Fprintf(&b, " [orderId %d]", *e.OrderID)
	}
	return b.String()
}

// HTTPError is a non-2xx HTTP response other than an exhausted 429 (see
// RateLimitError). An HTTP 401 response matches ErrUnauthorized.
type HTTPError struct {
	// Operation is the failed operation.
	Operation OperationID
	// StatusCode is the HTTP status code.
	StatusCode int
	// Header holds the response headers.
	Header http.Header
	// Body is the response body text.
	Body string
	// Problem is the parsed RFC 7807 body, including extension members, when
	// the body is ProblemDetails JSON (Order_SearchOrdersQuery documents this
	// for HTTP 400).
	Problem *ProblemDetails
	// Err is the error that prevented recovery, such as a failed re-login
	// after HTTP 401. It is nil otherwise.
	Err error
}

// Error describes the failure, including a short prefix of the body.
func (e *HTTPError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "topstepx: %s: HTTP %d %s", describeOp(e.Operation), e.StatusCode, http.StatusText(e.StatusCode))
	switch {
	case e.Problem != nil && e.Problem.summary() != "":
		fmt.Fprintf(&b, ": %s", e.Problem.summary())
	case strings.TrimSpace(e.Body) != "":
		fmt.Fprintf(&b, ": %s", snippet(e.Body))
	}
	if e.Err != nil {
		fmt.Fprintf(&b, " (%v)", e.Err)
	}
	return b.String()
}

// Is reports whether target is ErrUnauthorized and the status is 401.
func (e *HTTPError) Is(target error) bool {
	return target == ErrUnauthorized && e.StatusCode == http.StatusUnauthorized
}

// Unwrap returns Err.
func (e *HTTPError) Unwrap() error { return e.Err }

// RateLimitError is an HTTP 429 response that persisted after the retry
// budget was spent. It matches ErrRateLimited.
type RateLimitError struct {
	// Operation is the throttled operation.
	Operation OperationID
	// RetryAfter is the server's Retry-After value, nil when absent or
	// unparsable. HTTP-date values are converted to a duration.
	RetryAfter *time.Duration
	// Body is the response body text.
	Body string
}

// Error describes the failure.
func (e *RateLimitError) Error() string {
	msg := fmt.Sprintf("topstepx: %s: rate limited (HTTP 429)", describeOp(e.Operation))
	if e.RetryAfter != nil {
		msg += fmt.Sprintf(", retry after %s", *e.RetryAfter)
	}
	return msg
}

// Is reports whether target is ErrRateLimited.
func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// ValidationError is a client-side validation failure (contract section 9).
// It is returned before any network I/O, login included.
type ValidationError struct {
	// Operation is the rejected operation; it is empty when the error comes
	// from calling a request's Validate method directly.
	Operation OperationID
	// Field is the JSON path of the offending field, such as "trailPrice" or
	// "stopLossBracket.type".
	Field string
	// Reason explains the rule that failed.
	Reason string
}

// Error describes the failure.
func (e *ValidationError) Error() string {
	if e.Operation != "" {
		return fmt.Sprintf("topstepx: %s: invalid request: %s: %s", e.Operation, e.Field, e.Reason)
	}
	return fmt.Sprintf("topstepx: invalid request: %s: %s", e.Field, e.Reason)
}

// DecodeError is a response body that could not be decoded, or a successful
// response that lacks the payload the operation returns.
type DecodeError struct {
	// Operation is the operation whose response failed to decode.
	Operation OperationID
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// Body is the response body text. Token values in responses of
	// token-bearing operations are replaced with "[REDACTED]".
	Body string
	// Err is the underlying decode error.
	Err error
}

// Error describes the failure without echoing the body.
func (e *DecodeError) Error() string {
	return fmt.Sprintf("topstepx: %s: cannot decode response (HTTP %d, %d bytes): %v",
		describeOp(e.Operation), e.StatusCode, len(e.Body), e.Err)
}

// Unwrap returns Err.
func (e *DecodeError) Unwrap() error { return e.Err }

// TransportError is a failure below HTTP: DNS, connection, TLS, timeout or a
// body that could not be read. Err keeps the cause, so errors.Is works with
// context.DeadlineExceeded and context.Canceled.
type TransportError struct {
	// Operation is the operation that failed.
	Operation OperationID
	// Err is the underlying error.
	Err error
}

// Error describes the failure.
func (e *TransportError) Error() string {
	return fmt.Sprintf("topstepx: %s: transport error: %v", describeOp(e.Operation), e.Err)
}

// Unwrap returns Err.
func (e *TransportError) Unwrap() error { return e.Err }

func describeOp(id OperationID) string {
	if info, ok := id.Info(); ok {
		return fmt.Sprintf("%s (%s %s)", id, info.Method, info.Path)
	}
	return string(id)
}

func snippet(s string) string {
	s = strings.TrimSpace(s)
	const limit = 256
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

// tokenMember matches token-carrying JSON members so that bodies of
// token-bearing responses can be kept in errors without leaking secrets.
var tokenMember = regexp.MustCompile(`(?i)("(?:token|newToken|apiKey|password|verifyKey)"\s*:\s*)"(?:[^"\\]|\\.)*"`)

// redactBody returns body with secret JSON members replaced when op carries
// tokens or credentials.
func redactBody(op OperationID, body []byte) string {
	switch op {
	case OpAuthLoginKey, OpAuthLoginApp, OpAuthValidate:
		return tokenMember.ReplaceAllString(string(body), `$1"[REDACTED]"`)
	}
	return string(body)
}
