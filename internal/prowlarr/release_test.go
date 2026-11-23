package prowlarr

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func linkedRelease() Release {
	return Release{
		GUID: "g", Title: "t", Protocol: "torrent", InfoHash: "abc",
		DownloadURL: "http://prowlarr.local/1/download?apikey=secret",
		MagnetURL:   "magnet:?xt=urn:btih:abc&tr=http://tracker/passkey",
	}
}

func TestReleaseJSONOmitsDownloadLinks(t *testing.T) {
	b, err := json.Marshal(linkedRelease())
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.NotContains(t, m, "downloadUrl")
	assert.NotContains(t, m, "magnetUrl")
	assert.NotContains(t, string(b), "secret")
	assert.NotContains(t, string(b), "passkey")
	assert.Equal(t, "t", m["title"])
	assert.Equal(t, "abc", m["infoHash"])
}

func TestStoredReleaseKeepsDownloadLinks(t *testing.T) {
	r := linkedRelease()
	b, err := MarshalStored(r)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"downloadUrl"`)
	got, err := UnmarshalStored(b)
	require.NoError(t, err)
	assert.Equal(t, r, got)

	// Rows written before this change used the same keys.
	got, err = UnmarshalStored([]byte(`{"title":"old","protocol":"torrent","downloadUrl":"http://x/d","magnetUrl":"magnet:?xt=urn:btih:ff"}`))
	require.NoError(t, err)
	assert.Equal(t, "http://x/d", got.DownloadURL)
	assert.Equal(t, "magnet:?xt=urn:btih:ff", got.MagnetURL)
}
