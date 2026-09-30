package topstepx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// recordedRequest is a request seen by the mock server.
type recordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// mockServer is a local gateway double. Handlers are registered per path;
// every request is recorded.
type mockServer struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	requests []recordedRequest
	sleeps   []time.Duration
}

func newMockServer(t *testing.T) *mockServer {
	t.Helper()
	m := &mockServer{t: t, handlers: map[string]http.HandlerFunc{}}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.requests = append(m.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		h := m.handlers[r.URL.Path]
		m.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockServer) handle(path string, h http.HandlerFunc) {
	m.mu.Lock()
	m.handlers[path] = h
	m.mu.Unlock()
}

// handleLogin answers Auth_LoginKey with the given tokens in turn (the last
// one repeats).
func (m *mockServer) handleLogin(tokens ...string) {
	var mu sync.Mutex
	n := 0
	m.handle("/api/Auth/loginKey", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tok := tokens[min(n, len(tokens)-1)]
		n++
		mu.Unlock()
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"success":true,"errorCode":0,"errorMessage":null,"token":%q}`, tok))
	})
}

// client returns a client for the server whose retry sleeps are recorded
// instead of waited.
func (m *mockServer) client(t *testing.T, opts ...Option) *Client {
	t.Helper()
	c, err := NewClient(append([]Option{WithBaseURL(m.srv.URL)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(ctx context.Context, d time.Duration) error {
		m.mu.Lock()
		m.sleeps = append(m.sleeps, d)
		m.mu.Unlock()
		return ctx.Err()
	}
	return c
}

func (m *mockServer) total() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func (m *mockServer) count(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.requests {
		if r.Path == path {
			n++
		}
	}
	return n
}

func (m *mockServer) all(path string) []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []recordedRequest
	for _, r := range m.requests {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func (m *mockServer) last(path string) recordedRequest {
	all := m.all(path)
	if len(all) == 0 {
		m.t.Fatalf("no request to %s", path)
	}
	return all[len(all)-1]
}

func (m *mockServer) recordedSleeps() []time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Duration(nil), m.sleeps...)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func okJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, body) }
}

// sequence answers with the handlers in turn; the last one repeats.
func sequence(handlers ...http.HandlerFunc) http.HandlerFunc {
	var mu sync.Mutex
	n := 0
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		h := handlers[min(n, len(handlers)-1)]
		n++
		mu.Unlock()
		h(w, r)
	}
}

func status(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, code, body) }
}

// makeJWT returns an unsigned JWT with the given exp claim.
func makeJWT(exp time.Time, subject string) string {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"sub": subject, "exp": exp.Unix()})
	return header + "." + enc.EncodeToString(claims) + ".c2lnbmF0dXJl"
}

const accountsOK = `{"success":true,"errorCode":0,"errorMessage":null,"accounts":[{"id":1,"name":"A","balance":50000.25,"canTrade":true,"isVisible":true,"simulated":true}]}`
