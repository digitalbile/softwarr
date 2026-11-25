package main

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/config"
	"github.com/digitalbile/softwarr/internal/db"
	"github.com/digitalbile/softwarr/internal/db/dbq"
)

func TestResetPasswordCmd(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(filepath.Join(dir, "softwarr.db"))
	require.NoError(t, err)
	require.NoError(t, db.Migrate(ctx, d))
	_, err = auth.NewUsers(d, dbq.New(d)).Setup(ctx, "alice", "password1")
	require.NoError(t, err)
	require.NoError(t, d.Close())

	var out bytes.Buffer
	require.NoError(t, resetPasswordCmd(ctx, config.Config{DataDir: dir}, &out))
	assert.Regexp(t, `^The owner password has been reset\.\nUsername: alice\nTemporary password: [A-Za-z0-9]{20}\nYou'll be asked to choose a new password after signing in\.\n$`, out.String())
}

func TestHTTPServerLimitsHeaderTime(t *testing.T) {
	srv := newHTTPServer(7979, http.NotFoundHandler())
	assert.Equal(t, ":7979", srv.Addr)
	assert.Equal(t, 10*time.Second, srv.ReadHeaderTimeout, "slow clients can't hold connections open forever")
}
