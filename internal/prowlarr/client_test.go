package prowlarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

func TestSearchParsesReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixture(t, "search.json"))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "k", srv.Client()).Search(context.Background(), "editor", nil, nil)
	require.NoError(t, err)
	require.Len(t, got, 3)

	a := got[0]
	assert.Equal(t, "Example.Editor.2.4.1.Linux.x64", a.Title)
	assert.Equal(t, "torrent", a.Protocol)
	assert.Equal(t, 1, a.IndexerID)
	assert.Equal(t, int64(123456789), a.Size)
	assert.Equal(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), a.PublishDate.UTC())
	require.NotNil(t, a.Seeders)
	assert.Equal(t, 42, *a.Seeders)
	assert.Equal(t, []int{4000, 4010, 4020}, a.Categories)
	assert.Equal(t, "0123456789ABCDEF0123456789ABCDEF01234567", a.InfoHash)

	b := got[1]
	assert.Equal(t, "fedcba9876543210fedcba9876543210fedcba98", b.InfoHash)
	assert.Contains(t, b.MagnetURL, "magnet:?xt=urn:btih:")
	assert.Empty(t, b.DownloadURL)

	c := got[2]
	assert.Equal(t, "usenet", c.Protocol)
	assert.Nil(t, c.Seeders)
	require.NotNil(t, c.Grabs)
	assert.Equal(t, 120, *c.Grabs)
	assert.Empty(t, c.InfoHash)
}

func TestSearchSendsAPIKeyAndCategories(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	// A URL base path with a trailing slash must join cleanly.
	c := New(srv.URL+"/prowlarr/", "secret", srv.Client())
	_, err := c.Search(context.Background(), "my editor", []int{4000, 4010}, []int{1, 2})
	require.NoError(t, err)

	require.NotNil(t, gotReq)
	assert.Equal(t, "/prowlarr/api/v1/search", gotReq.URL.Path)
	assert.Equal(t, "secret", gotReq.Header.Get("X-Api-Key"))
	q := gotReq.URL.Query()
	assert.Equal(t, "my editor", q.Get("query"))
	assert.Equal(t, "search", q.Get("type"))
	assert.Equal(t, []string{"4000", "4010"}, q["categories"])
	assert.Equal(t, []string{"1", "2"}, q["indexerIds"])
	assert.Contains(t, gotReq.URL.RawQuery, "categories=4000&categories=4010")
}

func TestReleaseKeyPrecedence(t *testing.T) {
	assert.Equal(t, "ih:abc123", Release{InfoHash: "ABC123", GUID: "g", Title: "t"}.Key())
	assert.Equal(t, "guid:g", Release{GUID: "g", Title: "t"}.Key())
	assert.Equal(t, "title:t", Release{Title: "t"}.Key())
}

func TestSearchErrorIncludesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "k", srv.Client()).Search(context.Background(), "x", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "boom")
}

func TestIndexersAndPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Write([]byte(`[{"id":1,"name":"A","protocol":"torrent","enable":true},{"id":2,"name":"B","protocol":"usenet","enable":false}]`))
		case "/api/v1/system/status":
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	got, err := c.Indexers(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []Indexer{{1, "A", "torrent", true}, {2, "B", "usenet", false}}, got)
	require.NoError(t, c.Ping(context.Background()))
}

func TestReleaseInfoHashFromMagnet(t *testing.T) {
	r := wireRelease{MagnetURL: "magnet:?dn=x&xt=urn:btih:ABCDEF0123456789ABCDEF0123456789ABCDEF01"}.release()
	assert.Equal(t, "abcdef0123456789abcdef0123456789abcdef01", r.InfoHash)
	r = wireRelease{MagnetURL: "magnet:?xt=urn:btih:short"}.release()
	assert.Empty(t, r.InfoHash)
}
