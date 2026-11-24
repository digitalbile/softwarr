package db_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/digitalbile/softwarr/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoolInt(t *testing.T) {
	assert.Equal(t, int64(1), db.BoolInt(true))
	assert.Equal(t, int64(0), db.BoolInt(false))
}

func TestIsUniqueViolation(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	defer d.Close()
	ctx := context.Background()
	_, err = d.ExecContext(ctx, `CREATE TABLE t (v TEXT UNIQUE)`)
	require.NoError(t, err)
	_, err = d.ExecContext(ctx, `INSERT INTO t (v) VALUES ('a')`)
	require.NoError(t, err)
	_, err = d.ExecContext(ctx, `INSERT INTO t (v) VALUES ('a')`)
	assert.True(t, db.IsUniqueViolation(err))
	assert.False(t, db.IsUniqueViolation(nil))
	assert.False(t, db.IsUniqueViolation(errors.New("disk I/O error")))
}
