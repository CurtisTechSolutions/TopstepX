package topstepx

import "context"

// AuthService manages the session token. Obtain it from Client.Auth.
//
// Most programs never call it directly: with WithCredentials the client logs
// in lazily, renews the token through Validate inside the refresh margin and
// logs in again after expiry or HTTP 401.
type AuthService struct{ c *Client }

// LoginKey logs in with a platform username and API key (Auth_LoginKey) and
// stores the returned token, which it also returns. On a business failure,
// such as invalid credentials (errorCode 3, InvalidCredentials, delivered
// with HTTP 200), it returns an *APIError and stores nothing.
//
// LoginKey does not configure the credentials for automatic re-login; use
// WithCredentials for that.
func (s *AuthService) LoginKey(ctx context.Context, userName, apiKey string) (string, error) {
	return s.c.loginKey(ctx, userName, apiKey)
}

// LoginApp performs application login (Auth_LoginApp) and stores the returned
// token.
//
// Deprecated: application login appears only in the gateway's Swagger schema;
// the current authentication guide does not support it and says API-key
// login is the only supported way to create a session. Use LoginKey.
func (s *AuthService) LoginApp(ctx context.Context, req LoginAppRequest) (string, error) {
	resp, err := call[LoginResponse](ctx, s.c, OpAuthLoginApp, req)
	if err != nil {
		return "", err
	}
	return s.c.storeLoginToken(OpAuthLoginApp, resp)
}

// Validate renews the session (Auth_Validate): it sends the current token,
// stores the returned newToken and returns it. When the response carries no
// newToken, the current token stays in use and is returned.
func (s *AuthService) Validate(ctx context.Context) (string, error) {
	var resp ValidateResponse
	if err := s.c.exec(ctx, OpAuthValidate, nil, &resp, execOptions{}); err != nil {
		return "", err
	}
	if err := businessError(OpAuthValidate, &resp); err != nil {
		return "", err
	}
	if resp.NewToken != nil && *resp.NewToken != "" {
		s.c.sess.store(*resp.NewToken, s.c.now())
		return *resp.NewToken, nil
	}
	return s.c.sess.current(), nil
}

// Logout ends the session (Auth_Logout) and then clears the stored token,
// whatever the result; a business failure is still returned. It sends the
// stored token as-is, without renewing it first, and does nothing when no
// token is stored. A later authenticated call logs in again when credentials
// are configured.
func (s *AuthService) Logout(ctx context.Context) error {
	token := s.c.sess.current()
	if token == "" {
		return nil
	}
	var resp LogoutResponse
	err := s.c.exec(ctx, OpAuthLogout, nil, &resp, execOptions{token: token})
	s.c.sess.clear()
	if err != nil {
		return err
	}
	return businessError(OpAuthLogout, &resp)
}
