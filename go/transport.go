package topstepx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxResponseBytes caps how much of a response body is read.
const maxResponseBytes = 64 << 20

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// execOptions adjust a single exec call.
type execOptions struct {
	// token, when set, is sent as-is: ensure-token is skipped and an HTTP 401
	// is returned without re-login.
	token string
}

// response is a fully read HTTP response.
type response struct {
	status int
	header http.Header
	body   []byte
}

// Do performs any operation and decodes the complete response envelope into
// resp, a pointer such as *PlaceOrderResponse (or any value accepted by
// json.Unmarshal, such as *json.RawMessage). For Status_Ping, pass a *string.
//
// Unlike the high-level methods, Do does not turn a business failure into an
// error: inspect the envelope's Success and ErrorCode, for example to read
// the orderId of a rejected order. HTTP, transport, decode and validation
// errors are still returned, and authentication, rate limiting and retries
// work as for the high-level methods.
//
// req is the request body (a value or pointer of the operation's request
// type) and is ignored for operations without one (Auth_Validate,
// Auth_Logout and Status_Ping). Do never changes the stored session: a token
// returned by a login or validate operation is not stored and Auth_Logout
// does not clear it. Use Client.Auth for session management.
func (c *Client) Do(ctx context.Context, op OperationID, req, resp any) error {
	return c.exec(ctx, op, req, resp, execOptions{})
}

// call performs op and converts a business failure into an *APIError.
func call[R any, P interface {
	*R
	enveloped
}](ctx context.Context, c *Client, op OperationID, req any) (*R, error) {
	resp := new(R)
	if err := c.exec(ctx, op, req, resp, execOptions{}); err != nil {
		return nil, err
	}
	if err := businessError(op, P(resp)); err != nil {
		return nil, err
	}
	return resp, nil
}

// businessError returns an *APIError when r is a business failure.
func businessError(op OperationID, r enveloped) error {
	e := r.envelope()
	if !isFailure(e.success, e.code) {
		return nil
	}
	apiErr := &APIError{Operation: op, Code: int(e.code), CodeName: e.name, Message: e.message, Response: r}
	if placed, ok := r.(*PlaceOrderResponse); ok {
		apiErr.OrderID = placed.OrderID
	}
	return apiErr
}

// exec performs op: validation, authentication, rate limiting, the HTTP
// attempt, retries and HTTP 401 handling, then decoding into out.
func (c *Client) exec(ctx context.Context, op OperationID, in, out any, xo execOptions) error {
	info, ok := op.Info()
	if !ok {
		return fmt.Errorf("topstepx: unknown operation %q", op)
	}
	var body []byte
	if info.HasBody {
		if in == nil {
			return fmt.Errorf("topstepx: %s requires a request body", op)
		}
		if c.validation {
			if v, ok := in.(validator); ok {
				if err := v.Validate(); err != nil {
					var ve *ValidationError
					if errors.As(err, &ve) {
						ve.Operation = op
					}
					return err
				}
			}
		}
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("topstepx: %s: encode request: %w", op, err)
		}
		body = b
	}

	retries := 0
	reauthenticated := false
	for {
		token := ""
		switch {
		case info.Auth == AuthNone:
		case xo.token != "":
			token = xo.token
		case info.Auth == AuthBearer:
			t, err := c.ensureToken(ctx)
			if err != nil {
				return err
			}
			token = t
		case info.Auth == AuthBearerIfAvailable:
			t, err := c.ensureToken(ctx)
			if err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				c.logger.Debug("topstepx: no token available; sending anonymously", "operation", op, "reason", err)
			}
			token = t
		}

		if lim := c.limiters[info.RateBucket]; lim != nil {
			if err := lim.acquire(ctx); err != nil {
				return err
			}
		}

		res, err := c.attempt(ctx, info, body, token)
		if err != nil {
			var decodeErr *DecodeError
			if errors.As(err, &decodeErr) {
				return err
			}
			if ctx.Err() == nil && info.RetryClass == RetryRead && retries < c.maxRetries {
				delay := backoffDelay(c.backoff, c.maxBackoff, retries)
				retries++
				c.logger.Warn("topstepx: transport error; retrying", "operation", op, "retry", retries, "delay", delay, "error", err)
				if err := c.sleep(ctx, delay); err != nil {
					return err
				}
				continue
			}
			return &TransportError{Operation: op, Err: err}
		}

		switch status := res.status; {
		case status >= 200 && status < 300:
			return c.decode(info, res, out)

		case status == http.StatusUnauthorized:
			if token != "" && xo.token == "" && !reauthenticated {
				retry, rerr := c.reauthenticate(ctx, token)
				if rerr != nil {
					herr := c.httpError(info, res)
					herr.Err = rerr
					return herr
				}
				if retry {
					reauthenticated = true
					continue
				}
			}
			return c.httpError(info, res)

		case status == http.StatusTooManyRequests:
			retryAfter, hasRetryAfter := parseRetryAfter(res.header.Get("Retry-After"), c.now())
			if retries < c.maxRetries {
				delay := backoffDelay(c.backoff, c.maxBackoff, retries)
				if hasRetryAfter {
					delay = retryAfter
				}
				retries++
				c.logger.Warn("topstepx: HTTP 429; retrying", "operation", op, "retry", retries, "delay", delay)
				if err := c.sleep(ctx, delay); err != nil {
					return err
				}
				continue
			}
			rlErr := &RateLimitError{Operation: op, Body: redactBody(op, res.body)}
			if hasRetryAfter {
				rlErr.RetryAfter = &retryAfter
			}
			return rlErr

		case status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
			if info.RetryClass == RetryRead && retries < c.maxRetries {
				delay := backoffDelay(c.backoff, c.maxBackoff, retries)
				retries++
				c.logger.Warn("topstepx: server unavailable; retrying", "operation", op, "status", status, "retry", retries, "delay", delay)
				if err := c.sleep(ctx, delay); err != nil {
					return err
				}
				continue
			}
			return c.httpError(info, res)

		default:
			return c.httpError(info, res)
		}
	}
}

// attempt sends one HTTP request and reads the whole response.
func (c *Client) attempt(ctx context.Context, info OperationInfo, body []byte, token string) (*response, error) {
	actx, cancel := ctx, context.CancelFunc(func() {})
	if c.timeout > 0 {
		actx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(actx, info.Method, c.baseURL+info.Path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Debug("topstepx: request failed", "operation", info.ID, "error", err)
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	c.logger.Debug("topstepx: request", "operation", info.ID, "status", resp.StatusCode, "duration", time.Since(start))
	if len(data) > maxResponseBytes {
		return nil, &DecodeError{Operation: info.ID, StatusCode: resp.StatusCode,
			Err: fmt.Errorf("response body exceeds %d bytes", maxResponseBytes)}
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// decode parses a 2xx body into out whatever its Content-Type.
func (c *Client) decode(info OperationInfo, res *response, out any) error {
	if out == nil {
		return nil
	}
	body := bytes.TrimPrefix(res.body, utf8BOM)
	if info.ID == OpStatusPing {
		if s, ok := out.(*string); ok {
			*s = pingText(body)
			return nil
		}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &DecodeError{Operation: info.ID, StatusCode: res.status, Body: redactBody(info.ID, body), Err: err}
	}
	return nil
}

// pingText returns the ping payload: a JSON string is unquoted, anything else
// is returned as trimmed text.
func pingText(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '"' {
		var s string
		if err := json.Unmarshal(body, &s); err == nil {
			return s
		}
	}
	return string(body)
}

func (c *Client) httpError(info OperationInfo, res *response) *HTTPError {
	body := redactBody(info.ID, res.body)
	return &HTTPError{
		Operation:  info.ID,
		StatusCode: res.status,
		Header:     res.header,
		Body:       body,
		Problem:    parseProblem([]byte(body)),
	}
}
