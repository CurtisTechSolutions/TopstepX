package realtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
)

// SignalR JSON hub protocol v1 framing and messages.

// recordSeparator terminates every SignalR JSON message.
const recordSeparator = 0x1E

// Message types.
const (
	msgInvocation = 1
	msgCompletion = 3
	msgPing       = 6
	msgClose      = 7
	// Stream items (2), stream invocations (4), cancel invocations (5) and
	// unknown types are ignored.
)

var (
	handshakeRequest = []byte("{\"protocol\":\"json\",\"version\":1}\x1e")
	pingMessage      = []byte("{\"type\":6}\x1e")
	closeMessage     = []byte("{\"type\":7}\x1e")
)

// hubMessage is any message received from the server.
type hubMessage struct {
	Type           int               `json:"type"`
	InvocationID   *string           `json:"invocationId"`
	Target         string            `json:"target"`
	Arguments      []json.RawMessage `json:"arguments"`
	Result         json.RawMessage   `json:"result"`
	Error          *string           `json:"error"`
	AllowReconnect bool              `json:"allowReconnect"`
}

// invocationMessage is a client-to-server invocation.
type invocationMessage struct {
	Type         int    `json:"type"`
	InvocationID string `json:"invocationId"`
	Target       string `json:"target"`
	Arguments    []any  `json:"arguments"`
}

func encodeInvocation(id, target string, args []any) ([]byte, error) {
	if args == nil {
		args = []any{}
	}
	b, err := json.Marshal(invocationMessage{Type: msgInvocation, InvocationID: id, Target: target, Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("realtime: encode %s arguments: %w", target, err)
	}
	return append(b, recordSeparator), nil
}

// recordSplitter splits frames into records at the record separator and
// buffers an incomplete trailing record until the next frame.
type recordSplitter struct {
	buf   []byte
	limit int64
}

// feed appends a frame and returns the complete, non-empty records. The
// returned slices stay valid: later frames are appended past them.
func (r *recordSplitter) feed(frame []byte) ([][]byte, error) {
	r.buf = append(r.buf, frame...)
	var records [][]byte
	for {
		i := bytes.IndexByte(r.buf, recordSeparator)
		if i < 0 {
			break
		}
		if rec := bytes.TrimSpace(r.buf[:i]); len(rec) > 0 {
			records = append(records, rec)
		}
		r.buf = r.buf[i+1:]
	}
	if r.limit > 0 && int64(len(r.buf)) > r.limit {
		return records, fmt.Errorf("realtime: incomplete message exceeds %d bytes", r.limit)
	}
	return records, nil
}

// hubURL converts an http(s) hub URL to ws(s) and appends the URL-encoded
// access token. Errors never contain the token.
func hubURL(base, token string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("realtime: invalid hub URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("realtime: unsupported hub URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("realtime: hub URL %q has no host", base)
	}
	q := u.Query()
	q.Set("access_token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
