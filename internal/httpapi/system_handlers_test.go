package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/health"
	"github.com/digitalbile/softwarr/internal/logbuf"
)

func TestSystemEndpointsAdminOnly(t *testing.T) {
	ts := newTestServer(t)
	for _, p := range []string{"health", "logs", "status"} {
		assert.Equal(t, http.StatusUnauthorized, ts.do("GET", "/api/v1/system/"+p, nil).StatusCode)
	}
	ts.loginAsUser()
	for _, p := range []string{"health", "logs", "status"} {
		assert.Equal(t, http.StatusForbidden, ts.do("GET", "/api/v1/system/"+p, nil).StatusCode)
	}
}

func TestSystemStatus(t *testing.T) {
	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ts := newTestServerWith(t, "/base", func(d *Deps, q *dbq.Queries) { d.DataDir = "/config"; d.StartedAt = started })
	_, err := ts.Deps.Users.Setup(context.Background(), "owner", testPassword)
	require.NoError(t, err)
	login := ts.do("POST", "/base/api/v1/auth/login", map[string]string{"username": "owner", "password": testPassword})
	require.Equal(t, http.StatusOK, login.StatusCode)
	resp := ts.do("GET", "/base/api/v1/system/status", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, map[string]string{
		"version": "test", "goVersion": runtime.Version(), "dataDir": "/config",
		"urlBase": "/base", "startedAt": "2026-01-02T03:04:05Z",
	}, decode[map[string]string](t, resp))
}

func TestSystemLogsNewestFirstCapped(t *testing.T) {
	lb := logbuf.New(1000, nil)
	l := slog.New(lb)
	for i := 0; i < 600; i++ {
		l.Info(fmt.Sprintf("m%d", i))
	}
	ts := newTestServerWith(t, "", func(d *Deps, q *dbq.Queries) { d.Logs = lb })
	ts.loginAsOwner()
	resp := ts.do("GET", "/api/v1/system/logs", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	es := decode[[]logbuf.Entry](t, resp)
	require.Len(t, es, 500)
	assert.Equal(t, "m599", es[0].Message)
	assert.Equal(t, "m100", es[499].Message)
}

func TestSystemHealth(t *testing.T) {
	ts := newTestServerWith(t, "", func(d *Deps, q *dbq.Queries) {
		d.Health = health.NewChecker(q, d.Settings, nil, nil, nil, nil)
	})
	ts.loginAsOwner()
	resp := ts.do("GET", "/api/v1/system/health", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	cs := decode[[]health.Check](t, resp)
	require.NotEmpty(t, cs)
	assert.Equal(t, "prowlarr", cs[len(cs)-1].ID)
}
