package httpapi

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEEndpointRequiresLogin(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSSEEndpointStreams(t *testing.T) {
	ts := newTestServer(t)
	u := ts.loginAsUser()

	resp := ts.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	require.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	// The broker only delivers to subscribers, and the handler has already
	// flushed headers, so the subscription exists once Do returned.
	ts.Deps.Broker.Publish(events.Message{Type: events.TypeNotification, Data: map[string]string{"hello": "world"}, UserIDs: []int64{u.ID}})

	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for len(lines) < 2 && sc.Scan() {
		if sc.Text() == "" || strings.HasPrefix(sc.Text(), ":") {
			continue
		}
		lines = append(lines, sc.Text())
	}
	require.Equal(t, []string{"event: notification", `data: {"hello":"world"}`}, lines)
}

func TestSSEEndpointSendsPingAndUnsubscribes(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsUser()
	resp := ts.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	sc := bufio.NewScanner(resp.Body)
	got := make(chan string, 1)
	go func() {
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), ": ping") {
				got <- sc.Text()
				return
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("no ping")
	}
	resp.Body.Close()
	require.Eventually(t, func() bool { return ts.Deps.Broker.SubscriberCount() == 0 }, 3*time.Second, 10*time.Millisecond)
}

func TestSSEEndpointStopsOnShutdown(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsUser()
	resp := ts.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	close(ts.shutdown)
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, resp.Body) // returns at EOF once the handler exits
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream still open after shutdown signal")
	}
}

// streamEvents reads event names from an SSE body until it ends. The channel
// closes at EOF.
func streamEvents(body io.Reader) <-chan string {
	out := make(chan string, 16)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				out <- name
			}
		}
	}()
	return out
}

func waitClosed(t *testing.T, evs <-chan string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-evs:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("stream still open")
		}
	}
}

func newAdminClient(t *testing.T, ts *testServer, name string) (*client, auth.User) {
	t.Helper()
	u, err := ts.Deps.Users.CreateLocal(context.Background(), name, "", testPassword, true)
	require.NoError(t, err)
	c := ts.newClient()
	c.login(name, testPassword)
	return c, u
}

func TestSSEStreamEndsWhenAdminIsDemoted(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	bc, bob := newAdminClient(t, ts, "bob")
	resp := bc.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	evs := streamEvents(resp.Body)

	resp = ts.do("PATCH", "/api/v1/users/"+itoa(bob.ID), map[string]any{"isAdmin": false})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	waitClosed(t, evs)

	// The browser reconnects and now only gets what a regular user gets.
	resp = bc.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	evs = streamEvents(resp.Body)
	ts.Deps.Broker.Publish(events.Message{Type: events.TypeHealth, Data: "admins only", Admins: true})
	ts.Deps.Broker.Publish(events.Message{Type: events.TypeNotification, Data: "for bob", UserIDs: []int64{bob.ID}})
	select {
	case name := <-evs:
		assert.Equal(t, events.TypeNotification, name)
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
}

func TestSSEStreamEndsWhenUserIsDisabledOrDeleted(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	for _, act := range []func(id int64) *http.Response{
		func(id int64) *http.Response {
			return ts.do("PATCH", "/api/v1/users/"+itoa(id), map[string]any{"status": "disabled"})
		},
		func(id int64) *http.Response { return ts.do("DELETE", "/api/v1/users/"+itoa(id), nil) },
	} {
		c, u := newAdminClient(t, ts, "bob"+itoa(time.Now().UnixNano()))
		resp := c.do("GET", "/api/v1/events", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		evs := streamEvents(resp.Body)
		require.Less(t, act(u.ID).StatusCode, 300)
		waitClosed(t, evs)
	}
}

func TestSSEStreamEndsWhenPasswordChanges(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	other := ts.newClient()
	other.login("owner", testPassword)
	resp := other.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	evs := streamEvents(resp.Body)

	resp = ts.do("POST", "/api/v1/auth/password", map[string]string{"current": testPassword, "new": "a brand new one"})
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	waitClosed(t, evs)
	assert.Equal(t, http.StatusUnauthorized, other.do("GET", "/api/v1/events", nil).StatusCode)
}

func TestSSEStreamRechecksSessionOnPing(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	for name, change := range map[string]func(id int64){
		"demoted in the database": func(id int64) {
			_, err := ts.DB.Exec(`UPDATE users SET is_admin = 0 WHERE id = ?`, id)
			require.NoError(t, err)
		},
		"session removed": func(id int64) {
			_, err := ts.DB.Exec(`DELETE FROM sessions WHERE user_id = ?`, id)
			require.NoError(t, err)
		},
		"disabled": func(id int64) {
			_, err := ts.DB.Exec(`UPDATE users SET status = 'disabled' WHERE id = ?`, id)
			require.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, u := newAdminClient(t, ts, "u"+itoa(time.Now().UnixNano()))
			resp := c.do("GET", "/api/v1/events", nil)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			evs := streamEvents(resp.Body)
			change(u.ID)
			waitClosed(t, evs)
		})
	}
}

func TestPasswordChangeDisconnectsAfterNewSession(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.loginAsOwner()
	ch, cancel := ts.Deps.Broker.Subscribe(owner.ID, true)
	defer cancel()
	// When the stream is told to reconnect, the new session must already
	// exist, or the browser's reconnect fails and the stream stays off.
	sessions := make(chan int, 1)
	go func() {
		for range ch {
		}
		var n int
		_ = ts.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, owner.ID).Scan(&n)
		sessions <- n
	}()

	resp := ts.do("POST", "/api/v1/auth/password", map[string]string{"current": testPassword, "new": "a brand new one"})
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	select {
	case n := <-sessions:
		assert.Equal(t, 1, n)
	case <-time.After(3 * time.Second):
		t.Fatal("stream not disconnected")
	}
	assert.Equal(t, http.StatusOK, ts.do("GET", "/api/v1/auth/me", nil).StatusCode)
}

func TestSSEStreamEndsWhenDemotedWhileSubscribing(t *testing.T) {
	// Only the check at subscribe can end it within the test.
	ts := newTestServerWith(t, "", func(d *Deps) { d.PingInterval = time.Hour })
	ts.loginAsOwner()
	bc, bob := newAdminClient(t, ts, "bob")
	t.Cleanup(func() { testHookAfterSubscribe = nil })
	testHookAfterSubscribe = func() {
		_, err := ts.DB.Exec(`UPDATE users SET is_admin = 0 WHERE id = ?`, bob.ID)
		require.NoError(t, err)
	}
	resp := bc.do("GET", "/api/v1/events", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	waitClosed(t, streamEvents(resp.Body))
}
