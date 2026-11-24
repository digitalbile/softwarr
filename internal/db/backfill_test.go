package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedRequestErrorsMoveToDetail(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	defer d.Close()
	goose.SetBaseFS(migrations)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpToContext(ctx, d, "migrations", 3))

	_, err = d.Exec(`INSERT INTO users (username, created_at) VALUES ('sam', 1)`)
	require.NoError(t, err)
	rows := []struct{ status, errMsg, detail string }{
		{"failed", "Importing from /downloads/App failed: denied", ""},
		{"failed", "", ""},
		{"failed", "The download couldn't be started. An admin has the details.", "404 from indexer"},
		{"downloading", "something", ""},
	}
	for i, r := range rows {
		var detail any
		if r.detail != "" {
			detail = r.detail
		}
		_, err = d.Exec(`INSERT INTO requests (requester_id, status, release, release_key, error, error_detail, created_at, updated_at)
			VALUES (1, ?, '{}', ?, ?, ?, 1, 1)`, r.status, string(rune('a'+i)), r.errMsg, detail)
		require.NoError(t, err)
	}

	require.NoError(t, Migrate(ctx, d))

	type got struct {
		errMsg string
		detail *string
	}
	read := func(id int) got {
		var g got
		require.NoError(t, d.QueryRow(`SELECT error, error_detail FROM requests WHERE id = ?`, id).Scan(&g.errMsg, &g.detail))
		return g
	}
	g := read(1)
	assert.Equal(t, "This request failed. An admin has the details.", g.errMsg)
	require.NotNil(t, g.detail)
	assert.Equal(t, "Importing from /downloads/App failed: denied", *g.detail)
	assert.Equal(t, got{errMsg: ""}, read(2), "no error, nothing to move")
	g = read(3)
	assert.Equal(t, "The download couldn't be started. An admin has the details.", g.errMsg, "already split")
	assert.Equal(t, "404 from indexer", *g.detail)
	assert.Equal(t, got{errMsg: "something"}, read(4), "only failed requests")
}
