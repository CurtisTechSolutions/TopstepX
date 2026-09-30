package topstepx

import "context"

// StatusService checks API responsiveness. Obtain it from Client.Status.
type StatusService struct{ c *Client }

// Ping calls Status_Ping (listed only in Swagger) and returns its text,
// normally "pong", whether the gateway answers with text/plain or a JSON
// string. A bearer token is attached when one is held or obtainable;
// otherwise the ping is sent anonymously.
func (s *StatusService) Ping(ctx context.Context) (string, error) {
	var out string
	if err := s.c.exec(ctx, OpStatusPing, nil, &out, execOptions{}); err != nil {
		return "", err
	}
	return out, nil
}
