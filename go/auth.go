package topstepx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// credentials are the platform username and API key. They are immutable once
// the client is built and are never printed: the struct lives behind a
// pointer in unexported fields of types with redacting formatters.
type credentials struct {
	userName string
	apiKey   string
}

// String redacts the credentials.
func (c *credentials) String() string { return "credentials{" + redacted + "}" }

// Format redacts the credentials for every verb.
func (c *credentials) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

// session holds the token and coordinates refreshes. All fields except creds
// are guarded by mu.
type session struct {
	creds *credentials

	mu     sync.Mutex
	token  string
	expiry time.Time
	flight *flight // the refresh or login in progress, if any
	// renewBlockedUntil is the end of the renewal cooldown: after a failed
	// renewal that kept the current token, validate is not called again
	// before this time.
	renewBlockedUntil time.Time
}

// String redacts the session.
func (s *session) String() string { return "session{" + redacted + "}" }

// Format redacts the session for every verb.
func (s *session) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, s.String()) }

// flight is one in-progress refresh or login shared by concurrent callers.
type flight struct {
	done chan struct{}
	err  error // written before done is closed
}

// store saves a token, deriving its expiry from the JWT exp claim or assuming
// DefaultTokenLifetime from now.
func (s *session) store(token string, now time.Time) {
	exp, ok := jwtExpiry(token)
	if !ok {
		exp = now.Add(DefaultTokenLifetime)
	}
	s.mu.Lock()
	s.token, s.expiry = token, exp
	s.renewBlockedUntil = time.Time{} // a new token clears the cooldown
	s.mu.Unlock()
}

func (s *session) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

func (s *session) clear() {
	s.mu.Lock()
	s.token, s.expiry, s.renewBlockedUntil = "", time.Time{}, time.Time{}
	s.mu.Unlock()
}

// invalidate discards token if it is still the stored token and reports
// whether it did.
func (s *session) invalidate(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || s.token != token {
		return false
	}
	s.token, s.expiry, s.renewBlockedUntil = "", time.Time{}, time.Time{}
	return true
}

// blockRenewal starts the renewal cooldown for token, unless another token
// was stored meanwhile.
func (s *session) blockRenewal(token string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == token {
		s.renewBlockedUntil = until
	}
}

// runFlightLocked runs fn as the session's single in-flight refresh, or, when
// one is already running, waits for it and returns its result with
// ran == false. The caller must hold s.mu; it is released.
func (s *session) runFlightLocked(ctx context.Context, fn func() error) (ran bool, err error) {
	if f := s.flight; f != nil {
		s.mu.Unlock()
		select {
		case <-f.done:
			return false, f.err
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	f := &flight{done: make(chan struct{})}
	s.flight = f
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.flight = nil
		s.mu.Unlock()
		f.err = err
		close(f.done)
	}()
	return true, fn()
}

// jwtExpiry reads the exp claim of a JWT without verifying its signature.
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	seg := strings.TrimRight(parts[1], "=")
	payload, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		if payload, err = base64.RawStdEncoding.DecodeString(seg); err != nil {
			return time.Time{}, false
		}
	}
	var claims struct {
		Exp json.Number `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == "" {
		return time.Time{}, false
	}
	secs, err := strconv.ParseFloat(string(claims.Exp), 64)
	if err != nil || math.IsNaN(secs) || math.IsInf(secs, 0) || secs <= 0 || secs > 1e11 {
		return time.Time{}, false
	}
	whole, frac := math.Modf(secs)
	return time.Unix(int64(whole), int64(frac*1e9)).UTC(), true
}

type tokenAction int

const (
	actUse      tokenAction = iota // use the stored token
	actFail                        // not authenticated
	actLogin                       // log in with credentials
	actValidate                    // renew through Auth_Validate
)

// decideLocked implements the ensure_token rules of contract section 5.
func (s *session) decideLocked(now time.Time, margin time.Duration) tokenAction {
	switch {
	case s.token == "":
		if s.creds != nil {
			return actLogin
		}
		return actFail
	case !now.Before(s.expiry):
		if s.creds != nil {
			return actLogin
		}
		// Expired and nothing to log in with: let the server decide, which
		// tolerates clock skew.
		return actUse
	case !now.Before(s.expiry.Add(-margin)):
		if now.Before(s.renewBlockedUntil) {
			return actUse // renewal cooldown after a failed renewal
		}
		return actValidate
	default:
		return actUse
	}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// ensureToken returns a usable token, logging in or renewing it first when
// needed. Concurrent callers share one refresh.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	s := c.sess
	for {
		s.mu.Lock()
		act := s.decideLocked(c.now(), c.refreshMargin)
		switch act {
		case actUse:
			token := s.token
			s.mu.Unlock()
			return token, nil
		case actFail:
			s.mu.Unlock()
			return "", ErrNotAuthenticated
		}
		ran, err := s.runFlightLocked(ctx, func() error { return c.refresh(ctx, act) })
		if err != nil {
			if !ran && isContextError(err) && ctx.Err() == nil {
				continue // the caller that ran the refresh gave up; try again
			}
			return "", err
		}
		// Use whatever token the refresh left, even one still inside the
		// refresh margin, rather than starting another refresh.
		if token := s.current(); token != "" {
			return token, nil
		}
		if ran {
			return "", ErrNotAuthenticated
		}
	}
}

// refresh performs a login or a validate-based renewal.
func (c *Client) refresh(ctx context.Context, act tokenAction) error {
	if act == actLogin {
		return c.loginWithCredentials(ctx)
	}
	s := c.sess
	s.mu.Lock()
	token, expiry := s.token, s.expiry
	s.mu.Unlock()

	newToken, err := c.validateToken(ctx, token)
	if err == nil && newToken != "" {
		s.store(newToken, c.now())
		return nil
	}
	if err == nil {
		err = errors.New("validate returned no newToken")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.logger.Warn("topstepx: token renewal through validate failed", "error", err)
	if s.creds == nil {
		// Keep using the current, still-valid token, and do not call
		// validate again before the cooldown ends.
		s.blockRenewal(token, c.now().Add(RenewalCooldown))
		return nil
	}
	if lerr := c.loginWithCredentials(ctx); lerr != nil {
		if ctx.Err() == nil && c.now().Before(expiry) {
			c.logger.Warn("topstepx: re-login failed; keeping the current token", "retryIn", RenewalCooldown, "error", lerr)
			s.blockRenewal(token, c.now().Add(RenewalCooldown))
			return nil
		}
		return lerr
	}
	return nil
}

// reauthenticate handles HTTP 401 for a request sent with used. It reports
// whether the request should be retried: with a token another caller stored
// in the meantime, or after logging in again.
func (c *Client) reauthenticate(ctx context.Context, used string) (bool, error) {
	s := c.sess
	for {
		s.mu.Lock()
		if s.token != "" && s.token != used {
			s.mu.Unlock()
			return true, nil
		}
		if s.creds == nil {
			s.mu.Unlock()
			return false, nil
		}
		ran, err := s.runFlightLocked(ctx, func() error {
			if current := s.current(); current != "" && current != used {
				return nil
			}
			c.logger.Info("topstepx: HTTP 401; logging in again")
			return c.loginWithCredentials(ctx)
		})
		if err != nil {
			if !ran && isContextError(err) && ctx.Err() == nil {
				continue
			}
			return false, err
		}
		if ran {
			return true, nil
		}
		// Another caller's refresh finished; re-check whether it replaced
		// the rejected token.
	}
}

func (c *Client) loginWithCredentials(ctx context.Context) error {
	cr := c.sess.creds
	if cr == nil {
		return ErrNotAuthenticated
	}
	c.logger.Debug("topstepx: logging in with API key")
	_, err := c.loginKey(ctx, cr.userName, cr.apiKey)
	return err
}

// loginKey performs Auth_LoginKey and stores the token on success.
func (c *Client) loginKey(ctx context.Context, userName, apiKey string) (string, error) {
	resp, err := call[LoginResponse](ctx, c, OpAuthLoginKey, LoginAPIKeyRequest{UserName: userName, APIKey: apiKey})
	if err != nil {
		return "", err
	}
	return c.storeLoginToken(OpAuthLoginKey, resp)
}

func (c *Client) storeLoginToken(op OperationID, resp *LoginResponse) (string, error) {
	if resp.Token == nil || *resp.Token == "" {
		return "", &DecodeError{Operation: op, StatusCode: 200, Err: errors.New("successful login response has no token")}
	}
	c.sess.store(*resp.Token, c.now())
	return *resp.Token, nil
}

// validateToken calls Auth_Validate with token as-is (no ensure-token and no
// re-login on 401) and returns newToken.
func (c *Client) validateToken(ctx context.Context, token string) (string, error) {
	var resp ValidateResponse
	if err := c.exec(ctx, OpAuthValidate, nil, &resp, execOptions{token: token}); err != nil {
		return "", err
	}
	if err := businessError(OpAuthValidate, &resp); err != nil {
		return "", err
	}
	if resp.NewToken == nil {
		return "", nil
	}
	return *resp.NewToken, nil
}
