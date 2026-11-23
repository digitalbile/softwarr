package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/config"
	"github.com/digitalbile/softwarr/internal/db"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
