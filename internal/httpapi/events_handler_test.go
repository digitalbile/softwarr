package httpapi

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/events"
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
