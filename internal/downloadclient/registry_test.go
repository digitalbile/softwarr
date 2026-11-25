package downloadclient_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	_ "github.com/digitalbile/softwarr/internal/downloadclient/qbittorrent"
	"github.com/digitalbile/softwarr/internal/testutil"
)

func TestPickClientByPriority(t *testing.T) {
	_, q := testutil.NewDB(t)
	ctx := context.Background()
	mk := func(name, protocol string, enabled int64, priority int64) dbq.DownloadClient {
		c, err := q.CreateDownloadClient(ctx, dbq.CreateDownloadClientParams{Name: name, Type: "qbittorrent", Protocol: protocol, Enabled: enabled, Priority: priority, Settings: `{"host":"h","port":1}`})
		require.NoError(t, err)
		return c
	}
	_, ok, err := downloadclient.PickClient(ctx, q, "torrent")
	require.NoError(t, err)
	assert.False(t, ok)

	mk("disabled-best", "torrent", 0, 0)
	mk("usenet", "usenet", 1, 0)
	mk("low", "torrent", 1, 5)
	a := mk("a", "torrent", 1, 2)
	mk("b", "torrent", 1, 2)

	cfg, ok, err := downloadclient.PickClient(ctx, q, "torrent")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, a.ID, cfg.ID) // tie on priority 2 goes to the lower id
	assert.Equal(t, "a", cfg.Name)
	assert.Equal(t, 2, cfg.Priority)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "qbittorrent", cfg.Type)
	assert.JSONEq(t, `{"host":"h","port":1}`, string(cfg.Settings))
}
