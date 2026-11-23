package library

import (
	"context"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/importer"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateItem(t *testing.T) {
	d, q := testutil.NewDB(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rel := prowlarr.Release{GUID: "g", Title: "Thing.v1", Indexer: "idx", Protocol: "torrent", InfoHash: "ABC"}
	res := importer.Result{
		Folder: "/library/Thing.v1", Mode: importer.ModeHardlink, TotalSize: 8,
		Files: []downloadclient.File{{Path: "setup.dmg", Size: 6}, {Path: "docs/readme.txt", Size: 2}},
	}

	id, err := CreateItem(context.Background(), q, 7, "Thing", rel, res, now)
	require.NoError(t, err)

	var reqID, size, imported int64
	var title, name, key, folder, protocol, indexer, mode, status string
	require.NoError(t, d.QueryRow(`SELECT request_id, display_title, release_name, release_key, folder, total_size,
		protocol, indexer, import_mode, status, imported_at FROM library_items WHERE id = ?`, id).
		Scan(&reqID, &title, &name, &key, &folder, &size, &protocol, &indexer, &mode, &status, &imported))
	assert.Equal(t, int64(7), reqID)
	assert.Equal(t, "Thing", title)
	assert.Equal(t, "Thing.v1", name)
	assert.Equal(t, "ih:abc", key)
	assert.Equal(t, "/library/Thing.v1", folder)
	assert.Equal(t, int64(8), size)
	assert.Equal(t, "torrent", protocol)
	assert.Equal(t, "idx", indexer)
	assert.Equal(t, "hardlink", mode)
	assert.Equal(t, "available", status)
	assert.Equal(t, now.Unix(), imported)

	rows, err := d.Query("SELECT rel_path, size FROM library_files WHERE item_id = ? ORDER BY id", id)
	require.NoError(t, err)
	defer rows.Close()
	var got []downloadclient.File
	for rows.Next() {
		var f downloadclient.File
		require.NoError(t, rows.Scan(&f.Path, &f.Size))
		got = append(got, f)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, res.Files, got)
}
