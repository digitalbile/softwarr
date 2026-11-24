package health

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/internal/testutil"
)

type fakeClient struct{ err error }

func (f fakeClient) Protocol() string           { return "torrent" }
func (f fakeClient) Test(context.Context) error { return f.err }
func (f fakeClient) Host() string               { return "h" }
func (f fakeClient) Add(context.Context, downloadclient.AddRequest) (string, error) {
	return "", nil
}
func (f fakeClient) Get(context.Context, string) (downloadclient.Item, error) {
	return downloadclient.Item{}, nil
}

type env struct {
	q      *dbq.Queries
	st     *settings.Store
	broker *events.Broker
	c      *Checker
	root   string
	cliErr error
	prowOK bool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	_, q := testutil.NewDB(t)
	e := &env{q: q, st: settings.NewStore(q), broker: events.NewBroker(), root: t.TempDir(), prowOK: true}
	prow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !e.prowOK {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("{}"))
	}))
	t.Cleanup(prow.Close)
	s, err := e.st.Get(context.Background())
	require.NoError(t, err)
	s.LibraryRoot = e.root
	s.Prowlarr.URL = prow.URL
	s.Prowlarr.APIKey = "k"
	require.NoError(t, e.st.Save(context.Background(), s))
	_, err = q.CreateDownloadClient(context.Background(), dbq.CreateDownloadClientParams{Name: "qb", Type: "fake", Protocol: "torrent", Enabled: 1, Priority: 1, Settings: "{}"})
	require.NoError(t, err)
	n := notify.New(q, e.broker, e.st, http.DefaultClient, time.Now)
	e.c = NewChecker(q, e.st,
		func(url, key string) *prowlarr.Client { return prowlarr.New(url, key, http.DefaultClient) },
		func(downloadclient.Config) (downloadclient.Client, error) { return fakeClient{err: e.cliErr}, nil },
		n, e.broker)
	return e
}

func byID(cs []Check) map[string]Check {
	m := map[string]Check{}
	for _, c := range cs {
		m[c.ID] = c
	}
	return m
}

func TestHealthAllOK(t *testing.T) {
	e := newEnv(t)
	cs := e.c.Run(context.Background())
	require.NotEmpty(t, cs)
	for _, c := range cs {
		assert.Equal(t, "ok", c.Level, "%s: %s", c.ID, c.Message)
	}
	for i := 1; i < len(cs); i++ {
		assert.Less(t, cs[i-1].ID, cs[i].ID)
	}
}

func TestHealthFailures(t *testing.T) {
	e := newEnv(t)
	e.prowOK = false
	e.cliErr = errors.New(`Get "http://qb:8080/api?apikey=secret": refused`)
	m := byID(e.c.Run(context.Background()))
	assert.Equal(t, "error", m["prowlarr"].Level)
	assert.Contains(t, m["prowlarr"].Message, "Can't reach Prowlarr: ")
	cl := m["downloadclient:1"]
	assert.Equal(t, "error", cl.Level)
	assert.Contains(t, cl.Message, "Can't reach download client qb: ")
	assert.NotContains(t, cl.Message, "secret")
}

func TestHealthNotConfigured(t *testing.T) {
	_, q := testutil.NewDB(t)
	c := NewChecker(q, settings.NewStore(q), nil, nil, nil, nil)
	m := byID(c.Run(context.Background()))
	assert.Equal(t, Check{ID: "prowlarr", Level: "warning", Message: "Prowlarr isn't set up."}, m["prowlarr"])
	assert.Equal(t, "warning", m["noclient"].Level)
	assert.Equal(t, "No download client is enabled, so requests can't be downloaded.", m["noclient"].Message)
	assert.Equal(t, "No library folder is set.", m["library"].Message)
}

func TestHealthLibraryMissing(t *testing.T) {
	e := newEnv(t)
	s, _ := e.st.Get(context.Background())
	s.LibraryRoot = filepath.Join(e.root, "nope")
	require.NoError(t, e.st.Save(context.Background(), s))
	m := byID(e.c.Run(context.Background()))
	assert.Equal(t, "error", m["library"].Level)
	assert.Equal(t, "The library folder "+s.LibraryRoot+" doesn't exist or isn't writable.", m["library"].Message)
}

func TestHealthLibraryLooksUnmounted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := byID(e.c.Run(ctx))
	assert.Equal(t, "ok", m["library"].Level, "an empty library with no items is fine")

	_, err := e.q.InsertLibraryItem(ctx, dbq.InsertLibraryItemParams{
		DisplayTitle: "x", ReleaseName: "x", ReleaseKey: "k", Folder: filepath.Join(e.root, "Item"), ImportedAt: 1,
	})
	require.NoError(t, err)
	m = byID(e.c.Run(ctx))
	assert.Equal(t, "warning", m["library"].Level)
	assert.Equal(t, "The library folder "+e.root+" looks unmounted. It is missing or empty, but the library still has items in it. Imports wait until it is back.", m["library"].Message)

	require.NoError(t, os.RemoveAll(e.root))
	assert.Equal(t, "warning", byID(e.c.Run(ctx))["library"].Level)

	require.NoError(t, os.MkdirAll(filepath.Join(e.root, "Item"), 0o755))
	assert.Equal(t, "ok", byID(e.c.Run(ctx))["library"].Level)
}

func TestHealthPathMapping(t *testing.T) {
	e := newEnv(t)
	local := filepath.Join(e.root, "missing")
	pm, err := e.q.CreatePathMapping(context.Background(), dbq.CreatePathMappingParams{Host: "h", RemotePath: "/dl", LocalPath: local})
	require.NoError(t, err)
	m := byID(e.c.Run(context.Background()))
	id := "pathmapping:1"
	assert.Equal(t, "error", m[id].Level)
	assert.Equal(t, "softwarr can't read "+local+" (mapped from /dl).", m[id].Message)
	_ = pm
}

func insertItem(t *testing.T, q *dbq.Queries, protocol, mode string, at int64) {
	t.Helper()
	_, err := q.InsertLibraryItem(context.Background(), dbq.InsertLibraryItemParams{
		RequestID: sql.NullInt64{}, DisplayTitle: "t", ReleaseName: "r" + mode, ReleaseKey: mode + protocol + time.Now().String(),
		Folder: "f", Protocol: protocol, Indexer: "i", ImportMode: mode, ImportedAt: at,
	})
	require.NoError(t, err)
}

func TestHealthHardlinkFallbackWarning(t *testing.T) {
	e := newEnv(t)
	insertItem(t, e.q, "torrent", "hardlink", 100)
	assert.Equal(t, "ok", byID(e.c.Run(context.Background()))["hardlinks"].Level)
	insertItem(t, e.q, "usenet", "move", 300) // ignored: not a torrent
	insertItem(t, e.q, "torrent", "copy", 200)
	h := byID(e.c.Run(context.Background()))["hardlinks"]
	assert.Equal(t, "warning", h.Level)
	assert.Equal(t, "The last import was copied instead of hardlinked, which uses extra disk space. Put the download and library folders on the same filesystem to fix this.", h.Message)
}

func TestHealthOIDCUnreachable(t *testing.T) {
	e := newEnv(t)
	s, _ := e.st.Get(context.Background())
	s.ExternalURL = "http://x"
	s.OIDC.Enabled = true
	s.OIDC.Issuer = "http://127.0.0.1:1"
	require.NoError(t, e.st.Save(context.Background(), s))
	m := byID(e.c.Run(context.Background()))
	assert.Equal(t, "error", m["oidc"].Level)
	assert.Contains(t, m["oidc"].Message, "Can't reach the OIDC provider: ")
}

func TestHealthLoopNotifiesOnlyOnNewFailures(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	admin, err := e.q.CreateUser(ctx, dbq.CreateUserParams{Username: "a", IsAdmin: 1, IsOwner: 1, Status: "active", CreatedAt: 1})
	require.NoError(t, err)
	ch, cancel := e.broker.Subscribe(admin.ID, true)
	defer cancel()

	e.c.runOnce(ctx)
	e.prowOK = false
	e.c.runOnce(ctx)
	e.c.runOnce(ctx)
	ns, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: admin.ID, Column2: 0})
	require.NoError(t, err)
	assert.Len(t, ns, 1)
	assert.Equal(t, notify.EvHealthWarning, ns[0].Type)

	healthMsgs := 0
	for len(ch) > 0 {
		if (<-ch).Type == events.TypeHealth {
			healthMsgs++
		}
	}
	assert.Equal(t, 3, healthMsgs)

	// Recover, then fail again: a new notification.
	e.prowOK = true
	e.c.runOnce(ctx)
	e.prowOK = false
	e.c.runOnce(ctx)
	ns, _ = e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: admin.ID, Column2: 0})
	assert.Len(t, ns, 2)
}

func TestHealthLoopFirstRunSetsBaseline(t *testing.T) {
	e := newEnv(t)
	e.prowOK = false
	ctx := context.Background()
	admin, err := e.q.CreateUser(ctx, dbq.CreateUserParams{Username: "a", IsAdmin: 1, IsOwner: 1, Status: "active", CreatedAt: 1})
	require.NoError(t, err)
	ch, cancel := e.broker.Subscribe(admin.ID, true)
	defer cancel()

	// A check already failing at startup was failing before the restart
	// too, so admins aren't told again.
	e.c.runOnce(ctx)
	ns, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: admin.ID, Column2: 0})
	require.NoError(t, err)
	assert.Empty(t, ns)
	require.Len(t, ch, 1, "the health page still gets the results")
	assert.Equal(t, events.TypeHealth, (<-ch).Type)

	// Library goes from OK to failing: one notification.
	require.NoError(t, os.RemoveAll(e.root))
	e.c.runOnce(ctx)
	ns, err = e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: admin.ID, Column2: 0})
	require.NoError(t, err)
	require.Len(t, ns, 1)
	assert.Contains(t, ns[0].Message, "library folder")
}

func TestScrubStripsQueryStrings(t *testing.T) {
	assert.Equal(t, `Get "http://h/x": refused`, scrub(`Get "http://h/x?apikey=abc&b=1": refused`))
}

func blocker(id string, release <-chan struct{}) task {
	return task{id, "", func(context.Context) []Check { <-release; return nil }}
}

func TestWithTimeoutKeepsCheckID(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	got := withTimeout(context.Background(), 20*time.Millisecond, task{"prowlarr", "Can't reach Prowlarr: it didn't respond within %s.", blocker("x", release).run})
	require.Len(t, got, 1)
	assert.Equal(t, "prowlarr", got[0].ID)
	assert.Equal(t, "error", got[0].Level)
	assert.Equal(t, "Can't reach Prowlarr: it didn't respond within 20ms.", got[0].Message)

}

func TestWithTimeoutDistinctIDs(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	a := withTimeout(context.Background(), 10*time.Millisecond, blocker("downloadclient:1", release))
	b := withTimeout(context.Background(), 10*time.Millisecond, blocker("downloadclient:2", release))
	assert.Equal(t, "downloadclient:1", a[0].ID)
	assert.Equal(t, "downloadclient:2", b[0].ID)
	assert.Equal(t, "downloadclient:1 didn't respond within 10ms.", a[0].Message)
}

func TestHealthLoopTimeoutDoesNotRenotify(t *testing.T) {
	e := newEnv(t)
	e.c.timeout = 50 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	// Prowlarr hangs.
	e.c.newProwlarr = func(string, string) *prowlarr.Client {
		return prowlarr.New("http://x", "k", &http.Client{Transport: hangTransport{release}})
	}
	ctx := context.Background()
	admin, err := e.q.CreateUser(ctx, dbq.CreateUserParams{Username: "a", IsAdmin: 1, IsOwner: 1, Status: "active", CreatedAt: 1})
	require.NoError(t, err)
	e.c.prev = map[string]string{"prowlarr": LevelOK} // Prowlarr was fine
	e.c.runOnce(ctx)
	e.c.runOnce(ctx)
	m := byID(e.c.Run(ctx))
	assert.Equal(t, "error", m["prowlarr"].Level)
	ns, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: admin.ID, Column2: 0})
	require.NoError(t, err)
	assert.Len(t, ns, 1)
}

type hangTransport struct{ release <-chan struct{} }

func (h hangTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	select {
	case <-r.Context().Done():
		return nil, r.Context().Err()
	case <-h.release:
		return nil, errors.New("released")
	}
}
