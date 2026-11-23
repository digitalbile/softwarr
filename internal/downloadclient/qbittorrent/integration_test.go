//go:build integration

package qbittorrent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/stretchr/testify/require"
)

// Run manually against a real qBittorrent:
// QBT_URL=http://localhost:8080 QBT_USER=admin QBT_PASS=secret go test -tags integration ./internal/downloadclient/qbittorrent/
func TestIntegrationAddAndGet(t *testing.T) {
	raw, user, pass := os.Getenv("QBT_URL"), os.Getenv("QBT_USER"), os.Getenv("QBT_PASS")
	if raw == "" {
		t.Skip("QBT_URL not set")
	}
	u, err := url.Parse(raw)
	require.NoError(t, err)
	port, _ := strconv.Atoi(u.Port())
	settings, _ := json.Marshal(map[string]any{"host": u.Hostname(), "port": port, "useTls": u.Scheme == "https", "urlBase": u.Path, "username": user, "password": pass})
	c, err := downloadclient.New(downloadclient.Config{Type: "qbittorrent", Settings: settings}, &http.Client{Timeout: 15 * time.Second})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, c.Test(ctx))
	b, hash := sampleTorrent(t)
	id, err := c.Add(ctx, downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	require.Equal(t, hash, id)
	it, err := c.Get(ctx, id)
	require.NoError(t, err)
	require.NotEqual(t, downloadclient.StateMissing, it.State)
}
