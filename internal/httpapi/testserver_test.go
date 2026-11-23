package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/library"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/requests"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/require"
)

const testPassword = "correct horse"

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// client is an HTTP client with its own cookie jar. It sends the CSRF header
// on every request, like the web app does.
type client struct {
	ts   *testServer
	http *http.Client
}

type testServer struct {
	*client  // the default client
	t        *testing.T
	srv      *httptest.Server
	DB       *sql.DB
	Q        *dbq.Queries
	Deps     Deps
	Clock    *fakeClock
	shutdown chan struct{}
	owner    *auth.User
	user     *auth.User
}

func newTestServer(t *testing.T) *testServer { return newTestServerBase(t, "") }

func newTestServerBase(t *testing.T, urlBase string) *testServer {
	return newTestServerWith(t, urlBase, nil)
}

// newTestServerWith lets a test adjust Deps before the server starts.
func newTestServerWith(t *testing.T, urlBase string, mutate func(*Deps)) *testServer {
	t.Helper()
	d, q := testutil.NewDB(t)
	// Start at the real time: the cookie jar judges cookie expiry by the wall clock.
	clock := &fakeClock{t: time.Now()}
	st := settings.NewStore(q)
	shutdown := make(chan struct{})
	deps := Deps{
		DB: d, Q: q,
		Settings:     st,
		Users:        auth.NewUsers(d, q),
		Sessions:     auth.NewSessions(q, clock.Now),
		OIDC:         auth.NewOIDC(st, q, d, clock.Now),
		Done:         shutdown,
		Broker:       events.NewBroker(),
		PingInterval: 20 * time.Millisecond,
		URLBase:      urlBase,
		Version:      "test",
		Now:          clock.Now,
	}
	deps.Notify = notify.New(q, deps.Broker, st, http.DefaultClient, clock.Now)
	deps.Requests = requests.NewService(d, q, st, deps.Notify, deps.Broker, clock.Now)
	deps.Library = library.NewService(q, st, deps.Notify, deps.Broker, clock.Now)
	if mutate != nil {
		mutate(&deps)
	}
	srv := httptest.NewServer(New(deps))
	t.Cleanup(srv.Close)
	ts := &testServer{t: t, srv: srv, DB: d, Q: q, Deps: deps, Clock: clock, shutdown: shutdown}
	ts.client = ts.newClient()
	return ts
}

// newClient returns a client with an empty cookie jar.
func (ts *testServer) newClient() *client {
	jar, err := cookiejar.New(nil)
	require.NoError(ts.t, err)
	return &client{ts: ts, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

func (c *client) request(method, path string, body any, headers map[string]string) *http.Response {
	t := c.ts.t
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, c.ts.srv.URL+path, rdr)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// do sends a request with the CSRF header set.
func (c *client) do(method, path string, body any) *http.Response {
	c.ts.t.Helper()
	return c.request(method, path, body, map[string]string{"X-Softwarr-CSRF": "1"})
}

// login signs in through the HTTP endpoint and returns the user.
func (c *client) login(username, password string) auth.User {
	t := c.ts.t
	t.Helper()
	resp := c.do("POST", "/api/v1/auth/login", map[string]string{"username": username, "password": password})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return decode[auth.User](t, resp)
}

// loginAsOwner creates the owner on first use, then signs this client in.
func (c *client) loginAsOwner() auth.User {
	t := c.ts.t
	t.Helper()
	if c.ts.owner == nil {
		u, err := c.ts.Deps.Users.Setup(context.Background(), "owner", testPassword)
		require.NoError(t, err)
		c.ts.owner = &u
	}
	return c.login("owner", testPassword)
}

// loginAsUser creates a regular user on first use, then signs this client in.
func (c *client) loginAsUser() auth.User {
	t := c.ts.t
	t.Helper()
	if c.ts.user == nil {
		u, err := c.ts.Deps.Users.CreateLocal(context.Background(), "user", "", testPassword, false)
		require.NoError(t, err)
		c.ts.user = &u
	}
	return c.login("user", testPassword)
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
	return v
}

func decodeString[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(s), &v))
	return v
}

func errorBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	return decode[map[string]string](t, resp)["error"]
}

// newHandlerServer serves an arbitrary handler, for testing middleware on
// routes the real API doesn't have yet.
func newHandlerServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

// doOn sends a GET to srv using c's cookie jar.
func doOn(t *testing.T, srv *httptest.Server, c *client, path string) *http.Response {
	t.Helper()
	resp, err := c.http.Get(srv.URL + path)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
