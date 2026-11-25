package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/releasetoken"
	"github.com/digitalbile/softwarr/internal/settings"
)

const (
	keyA   = "ih:0123456789abcdef0123456789abcdef01234567"
	keyB   = "ih:fedcba9876543210fedcba9876543210fedcba98"
	keyUse = "guid:nzb-guid-3003"
)

type fakeProwlarr struct {
	*httptest.Server
	mu    sync.Mutex
	query map[string][]string
}

func newFakeProwlarr(t *testing.T) *fakeProwlarr {
	t.Helper()
	body, err := os.ReadFile("../prowlarr/testdata/search.json")
	require.NoError(t, err)
	f := &fakeProwlarr{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			f.mu.Lock()
			f.query = r.URL.Query()
			f.mu.Unlock()
			w.Write(body)
		case "/api/v1/indexer":
			w.Write([]byte(`[{"id":1,"name":"A","protocol":"torrent","enable":true}]`))
		case "/api/v1/system/status":
			if r.Header.Get("X-Api-Key") != "good" {
				http.Error(w, "nope", http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func configureProwlarr(t *testing.T, ts *testServer, url string) {
	t.Helper()
	st, err := ts.Deps.Settings.Get(context.Background())
	require.NoError(t, err)
	st.Prowlarr = settings.Prowlarr{URL: url, APIKey: "good", Categories: []int{4000, 4010}}
	require.NoError(t, ts.Deps.Settings.Save(context.Background(), st))
}

type searchRow struct {
	Key           string `json:"key"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	LibraryItemID *int64 `json:"libraryItemId"`
}

func TestSearchStatusBadges(t *testing.T) {
	ts := newTestServer(t)
	fp := newFakeProwlarr(t)
	configureProwlarr(t, ts, fp.URL)
	owner := ts.loginAsOwner()

	res, err := ts.DB.Exec(`INSERT INTO library_items (display_title, release_name, release_key, folder, imported_at, status)
		VALUES ('Editor', 'Example.Editor', ?, 'f', 1, 'available')`, keyA)
	require.NoError(t, err)
	itemID, _ := res.LastInsertId()
	// An expired item with the same key as B must not count as available.
	_, err = ts.DB.Exec(`INSERT INTO library_items (display_title, release_name, release_key, folder, imported_at, status)
		VALUES ('Player', 'Example.Player', ?, 'f', 1, 'expired')`, keyB)
	require.NoError(t, err)
	_, err = ts.DB.Exec(`INSERT INTO requests (requester_id, status, release, release_key, created_at, updated_at)
		VALUES (?, 'pending', '{}', ?, 1, 1)`, owner.ID, keyB)
	require.NoError(t, err)
	_, err = ts.DB.Exec(`INSERT INTO download_clients (name, type, protocol, enabled) VALUES ('qb', 'qbittorrent', 'torrent', 1)`)
	require.NoError(t, err)

	resp := ts.do("GET", "/api/v1/search?q=editor", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	rows := decode[[]searchRow](t, resp)
	require.Len(t, rows, 3)
	// Newest first: B (Mar 2), A (Mar 1), usenet (Feb 20).
	assert.Equal(t, keyB, rows[0].Key)
	assert.Equal(t, "requested", rows[0].Status)
	assert.Equal(t, keyA, rows[1].Key)
	assert.Equal(t, "available", rows[1].Status)
	require.NotNil(t, rows[1].LibraryItemID)
	assert.Equal(t, itemID, *rows[1].LibraryItemID)
	assert.Equal(t, keyUse, rows[2].Key)
	assert.Equal(t, "no_client", rows[2].Status)
	assert.Nil(t, rows[2].LibraryItemID)
}

func TestSearchRequestableAndValidation(t *testing.T) {
	ts := newTestServer(t)
	fp := newFakeProwlarr(t)

	assert.Equal(t, http.StatusUnauthorized, ts.do("GET", "/api/v1/search?q=x", nil).StatusCode)
	ts.newClient().loginAsOwner()
	ts.loginAsUser()

	resp := ts.do("GET", "/api/v1/search?q=x", nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "Prowlarr isn't set up yet. An admin can add it in Settings > Indexers.", errorBody(t, resp))

	configureProwlarr(t, ts, fp.URL)
	resp = ts.do("GET", "/api/v1/search?q=%20%20", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Enter something to search for.", errorBody(t, resp))

	_, err := ts.DB.Exec(`INSERT INTO download_clients (name, type, protocol, enabled) VALUES ('qb', 'qbittorrent', 'torrent', 1)`)
	require.NoError(t, err)
	_, err = ts.DB.Exec(`INSERT INTO download_clients (name, type, protocol, enabled) VALUES ('off', 'sab', 'usenet', 0)`)
	require.NoError(t, err)
	rows := decode[[]searchRow](t, ts.do("GET", "/api/v1/search?q=x", nil))
	require.Len(t, rows, 3)
	assert.Equal(t, "requestable", rows[0].Status)
	assert.Equal(t, "requestable", rows[1].Status)
	assert.Equal(t, "no_client", rows[2].Status) // usenet client is disabled

	// Configured categories are sent to Prowlarr.
	fp.mu.Lock()
	defer fp.mu.Unlock()
	assert.Equal(t, []string{"4000", "4010"}, fp.query["categories"])
}

func TestSearchNonAdminCannotFilterIndexers(t *testing.T) {
	ts := newTestServer(t)
	fp := newFakeProwlarr(t)
	configureProwlarr(t, ts, fp.URL)

	ts.newClient().loginAsOwner()
	ts.loginAsUser()
	require.Equal(t, http.StatusOK, ts.do("GET", "/api/v1/search?q=x&indexerIds=1,2", nil).StatusCode)
	fp.mu.Lock()
	assert.Empty(t, fp.query["indexerIds"])
	fp.mu.Unlock()

	admin := ts.newClient()
	admin.loginAsOwner()
	require.Equal(t, http.StatusOK, admin.do("GET", "/api/v1/search?q=x&indexerIds=1,2", nil).StatusCode)
	fp.mu.Lock()
	assert.Equal(t, []string{"1", "2"}, fp.query["indexerIds"])
	fp.mu.Unlock()
}

func TestSearchProwlarrUnavailable(t *testing.T) {
	ts := newTestServer(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "kaput", http.StatusInternalServerError)
	}))
	configureProwlarr(t, ts, bad.URL)
	ts.newClient().loginAsOwner()
	ts.loginAsUser()

	// Users get a generic message; the detail can carry Prowlarr's URL.
	resp := ts.do("GET", "/api/v1/search?q=x", nil)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, "Couldn't reach Prowlarr. Ask an admin to check System > Health.", errorBody(t, resp))

	admin := ts.newClient()
	admin.loginAsOwner()
	resp = admin.do("GET", "/api/v1/search?q=x", nil)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Contains(t, errorBody(t, resp), "Couldn't reach Prowlarr: ")

	bad.Close()
	resp = ts.do("GET", "/api/v1/search?q=x", nil)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, "Couldn't reach Prowlarr. Ask an admin to check System > Health.", errorBody(t, resp))
}

func TestSearchResultsHideDownloadLinks(t *testing.T) {
	ts := newTestServer(t)
	fp := newFakeProwlarr(t)
	configureProwlarr(t, ts, fp.URL)
	seedClient(t, ts)
	admin := ts.newClient()
	o := admin.loginAsOwner()
	u := ts.loginAsUser()

	for _, c := range []struct {
		name   string
		client *client
		uid    int64
	}{{"user", ts.client, u.ID}, {"admin", admin, o.ID}} {
		t.Run(c.name, func(t *testing.T) {
			resp := c.client.do("GET", "/api/v1/search?q=x", nil)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			body := readBody(t, resp)
			for _, leak := range []string{"downloadUrl", "magnetUrl", "prowlarr.local", "magnet:"} {
				assert.NotContains(t, body, leak)
			}
			rows := decodeString[[]struct {
				Title string `json:"title"`
				Token string `json:"token"`
			}](t, body)
			require.Len(t, rows, 3)
			key, err := ts.Deps.Settings.ReleaseSecret(t.Context())
			require.NoError(t, err)
			for _, r := range rows {
				require.NotEmpty(t, r.Token)
				rel, err := releasetoken.Open(key, r.Token, c.uid, ts.Clock.Now())
				require.NoError(t, err)
				assert.Equal(t, r.Title, rel.Title)
				assert.True(t, rel.DownloadURL != "" || rel.MagnetURL != "", "the token keeps the link")
			}
		})
	}

	// A user requests straight from a search result.
	rows := decode[[]struct {
		Title  string `json:"title"`
		Token  string `json:"token"`
		Status string `json:"status"`
	}](t, ts.do("GET", "/api/v1/search?q=x", nil))
	require.Equal(t, "requestable", rows[1].Status)
	resp := ts.do("POST", "/api/v1/requests", map[string]any{"token": rows[1].Token, "comment": "for work"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var stored string
	require.NoError(t, ts.DB.QueryRow("SELECT release FROM requests").Scan(&stored))
	assert.Contains(t, stored, "http://prowlarr.local/1/download")
}

func TestSearchProwlarrTimeout(t *testing.T) {
	ts := newTestServerWith(t, "", func(d *Deps, q *dbq.Queries) {
		d.NewProwlarr = func(url, key string) *prowlarr.Client {
			return prowlarr.New(url, key, &http.Client{Timeout: 100 * time.Millisecond})
		}
	})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	configureProwlarr(t, ts, slow.URL)
	ts.newClient().loginAsOwner()
	ts.loginAsUser()

	start := time.Now()
	resp := ts.do("GET", "/api/v1/search?q=x", nil)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Contains(t, errorBody(t, resp), "Couldn't reach Prowlarr")
	assert.Less(t, time.Since(start), 900*time.Millisecond)
}

func TestIndexersAdminOnly(t *testing.T) {
	ts := newTestServer(t)
	fp := newFakeProwlarr(t)
	configureProwlarr(t, ts, fp.URL)

	ts.newClient().loginAsOwner()
	ts.loginAsUser()
	assert.Equal(t, http.StatusForbidden, ts.do("GET", "/api/v1/indexers", nil).StatusCode)

	admin := ts.newClient()
	admin.loginAsOwner()
	resp := admin.do("GET", "/api/v1/indexers", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decode[[]prowlarr.Indexer](t, resp)
	require.Len(t, got, 1)
	assert.Equal(t, "A", got[0].Name)

	raw := decode[[]map[string]any](t, admin.do("GET", "/api/v1/indexers", nil))
	require.Len(t, raw, 1)
	for _, k := range []string{"id", "name", "protocol", "enabled"} {
		assert.Contains(t, raw[0], k)
	}
}

func TestProwlarrTestEndpoint(t *testing.T) {
	ts := newTestServer(t)
	fp := newFakeProwlarr(t)

	ts.newClient().loginAsOwner()
	ts.loginAsUser()
	body := map[string]string{"url": fp.URL, "apiKey": "good"}
	assert.Equal(t, http.StatusForbidden, ts.do("POST", "/api/v1/settings/prowlarr/test", body).StatusCode)

	admin := ts.newClient()
	admin.loginAsOwner()
	type result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	resp := admin.do("POST", "/api/v1/settings/prowlarr/test", body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, result{OK: true}, decode[result](t, resp))

	resp = admin.do("POST", "/api/v1/settings/prowlarr/test", map[string]string{"url": fp.URL, "apiKey": "bad"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	r := decode[result](t, resp)
	assert.False(t, r.OK)
	assert.Contains(t, r.Error, "401")
}
