package db_test

import (
	"testing"

	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMigrateCreatesSchema(t *testing.T) {
	d, _ := testutil.NewDB(t)
	want := []string{"users", "user_identities", "sessions", "oidc_states", "settings",
		"download_clients", "path_mappings", "requests", "library_items", "library_files",
		"notifications", "webhooks"}
	for _, name := range want {
		var got string
		err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&got)
		require.NoError(t, err, name)
	}
}

func TestOnlyOneOwner(t *testing.T) {
	d, _ := testutil.NewDB(t)
	_, err := d.Exec(`INSERT INTO users (username, is_owner, created_at) VALUES ('a', 1, 0)`)
	require.NoError(t, err)
	_, err = d.Exec(`INSERT INTO users (username, is_owner, created_at) VALUES ('b', 1, 0)`)
	require.Error(t, err)
	_, err = d.Exec(`INSERT INTO users (username, is_owner, created_at) VALUES ('c', 0, 0)`)
	require.NoError(t, err)
}

func TestOpenRequestKeyUnique(t *testing.T) {
	d, _ := testutil.NewDB(t)
	_, err := d.Exec(`INSERT INTO users (username, created_at) VALUES ('u', 0)`)
	require.NoError(t, err)
	ins := func(status string) error {
		_, err := d.Exec(`INSERT INTO requests (requester_id, status, release, release_key, created_at, updated_at)
			VALUES (1, ?, '{}', 'k', 0, 0)`, status)
		return err
	}
	require.NoError(t, ins("denied"))
	require.NoError(t, ins("pending"))
	require.Error(t, ins("pending"))
	require.Error(t, ins("downloading"))
	require.NoError(t, ins("denied"))
}
