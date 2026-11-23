package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenPathWithURIDelimiters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data #1?")
	require.NoError(t, os.Mkdir(dir, 0o755))
	path := filepath.Join(dir, "soft%20warr.db")
	d, err := Open(path)
	require.NoError(t, err)
	defer d.Close()
	_, err = d.Exec("CREATE TABLE t (x INTEGER)")
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.NoError(t, err, "the database lives at the exact path given")
}

// Transactions take the write lock when they begin, so a read-then-write
// transaction can't fail with SQLITE_BUSY_SNAPSHOT under WAL.
func TestOpenBeginsImmediateTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "softwarr.db")
	d, err := Open(path)
	require.NoError(t, err)
	defer d.Close()
	_, err = d.Exec("CREATE TABLE t (x INTEGER)")
	require.NoError(t, err)

	tx, err := d.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()

	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(0)")
	require.NoError(t, err)
	defer other.Close()
	_, err = other.Exec("BEGIN IMMEDIATE")
	if err == nil {
		_, _ = other.Exec("ROLLBACK")
	}
	assert.Error(t, err, "another writer is locked out while the transaction is open")
}
