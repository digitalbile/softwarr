package requests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/internal/torrentfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMagnet = "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=thing"

// fakeClient is a download client that records calls and serves canned items.
type fakeClient struct {
	mu    sync.Mutex
	host  string
	adds  []downloadclient.AddRequest
	gets  int
	item  downloadclient.Item
	getFn func(id string) (downloadclient.Item, error)
}

func (f *fakeClient) Protocol() string               { return "torrent" }
func (f *fakeClient) Test(ctx context.Context) error { return nil }
func (f *fakeClient) Host() string                   { return f.host }

func (f *fakeClient) Add(ctx context.Context, req downloadclient.AddRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds = append(f.adds, req)
	return "hash1", nil
}

func (f *fakeClient) Get(ctx context.Context, id string) (downloadclient.Item, error) {
	f.mu.Lock()
	f.gets++
	fn, it := f.getFn, f.item
	f.mu.Unlock()
	if fn != nil {
		return fn(id)
	}
	it.ID = id
	return it, nil
}

func (f *fakeClient) setItem(it downloadclient.Item) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.item = it
}

func (f *fakeClient) calls() (adds, gets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.adds), f.gets
}

type workerEnv struct {
	env
	w       *Worker
	fc      *fakeClient
	library string
	clock   *time.Time
	mu      *sync.Mutex
}

func newWorkerEnv(t *testing.T) workerEnv {
	t.Helper()
	e := newEnv(t)
	lib := t.TempDir()
	e.setSettings(t, func(s *settings.Settings) { s.LibraryRoot = lib })
	fc := &fakeClient{host: "qbit.lan"}
	now := fixedNow
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	w := NewWorker(e.svc, e.q, e.st, http.DefaultClient,
		func(downloadclient.Config) (downloadclient.Client, error) { return fc, nil }, clock)
	return workerEnv{env: e, w: w, fc: fc, library: lib, clock: &now, mu: &mu}
}

func (we workerEnv) advance(d time.Duration) {
	we.mu.Lock()
	defer we.mu.Unlock()
	*we.clock = we.clock.Add(d)
}

func sampleTorrent(t *testing.T) ([]byte, string) {
	t.Helper()
	b, err := os.ReadFile("../torrentfile/testdata/sample.torrent")
	require.NoError(t, err)
	h, err := torrentfile.InfoHash(b)
	require.NoError(t, err)
	return b, h
}

// indexer serves download links: /torrent gives the sample torrent, /magnet
// redirects to a magnet link, /html gives a web page, anything else is 404.
func indexer(t *testing.T, body []byte, onHit func()) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onHit != nil {
			onHit()
		}
		switch r.URL.Path {
		case "/torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			w.Write(body)
		case "/magnet":
			http.Redirect(w, r, testMagnet, http.StatusFound)
		case "/html":
			w.Write([]byte("<html>login</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// approved creates a request by the user and approves it.
func (we workerEnv) approved(t *testing.T, r prowlarr.Release) Request {
	t.Helper()
	ctx := context.Background()
	req, err := we.svc.Create(ctx, we.user, r, "")
	require.NoError(t, err)
	req, err = we.svc.Approve(ctx, we.admin, req.ID, ApproveOpts{})
	require.NoError(t, err)
	return req
}

// downloading creates a request and grabs it through a magnet link.
func (we workerEnv) downloading(t *testing.T, name string) Request {
	t.Helper()
	req := we.approved(t, prowlarr.Release{GUID: "g-" + name, Title: name, Protocol: "torrent", MagnetURL: testMagnet})
	require.NoError(t, we.w.Tick(context.Background()))
	return we.get(t, req.ID, Downloading)
}

func (we workerEnv) get(t *testing.T, id int64, want Status) Request {
	t.Helper()
	got, err := we.svc.Get(context.Background(), we.admin, id)
	require.NoError(t, err)
	require.Equal(t, want, got.Status, "error: %s", got.Error)
	return got
}

func (we workerEnv) row(t *testing.T, id int64) dbq.GetRequestRow {
	t.Helper()
	r, err := we.q.GetRequest(context.Background(), id)
	require.NoError(t, err)
	return r
}

func (we workerEnv) notifications(t *testing.T, userID int64) []dbq.Notification {
	t.Helper()
	n, err := we.q.ListNotifications(context.Background(), dbq.ListNotificationsParams{UserID: userID, Column2: 0})
	require.NoError(t, err)
	return n
}

func hasNotification(ns []dbq.Notification, typ string) bool {
	for _, n := range ns {
		if n.Type == typ {
			return true
		}
	}
	return false
}

func TestWorkerGrabsTorrentFile(t *testing.T) {
	we := newWorkerEnv(t)
	body, hash := sampleTorrent(t)
	srv := indexer(t, body, nil)
	req := we.approved(t, prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", DownloadURL: srv.URL + "/torrent?apikey=secret"})

	require.NoError(t, we.w.Tick(context.Background()))

	we.get(t, req.ID, Downloading)
	require.Len(t, we.fc.adds, 1)
	assert.Equal(t, body, we.fc.adds[0].TorrentFile)
	assert.Equal(t, hash, we.fc.adds[0].InfoHash)
	assert.Empty(t, we.fc.adds[0].MagnetURI)
	row := we.row(t, req.ID)
	assert.Equal(t, "hash1", row.ClientItemID)
	assert.True(t, row.ClientID.Valid)
	assert.Equal(t, fixedNow.Unix(), row.LastProgressAt.Int64)
}

func TestWorkerGrabFollowsMagnetRedirect(t *testing.T) {
	we := newWorkerEnv(t)
	srv := indexer(t, nil, nil)
	req := we.approved(t, prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", DownloadURL: srv.URL + "/magnet?apikey=secret"})

	require.NoError(t, we.w.Tick(context.Background()))

	we.get(t, req.ID, Downloading)
	require.Len(t, we.fc.adds, 1)
	assert.Equal(t, testMagnet, we.fc.adds[0].MagnetURI)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", we.fc.adds[0].InfoHash)
	assert.Nil(t, we.fc.adds[0].TorrentFile)
}

func TestWorkerGrabDownloadURLThatIsMagnet(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.approved(t, prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", DownloadURL: testMagnet})

	require.NoError(t, we.w.Tick(context.Background()))

	we.get(t, req.ID, Downloading)
	require.Len(t, we.fc.adds, 1)
	assert.Equal(t, testMagnet, we.fc.adds[0].MagnetURI)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", we.fc.adds[0].InfoHash)
}

func TestWorkerGrabFallsBackToMagnet(t *testing.T) {
	we := newWorkerEnv(t)
	srv := indexer(t, nil, nil)
	req := we.approved(t, prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", DownloadURL: srv.URL + "/html", MagnetURL: testMagnet})

	require.NoError(t, we.w.Tick(context.Background()))

	we.get(t, req.ID, Downloading)
	require.Len(t, we.fc.adds, 1)
	assert.Equal(t, testMagnet, we.fc.adds[0].MagnetURI)
}

func TestWorkerGrabMagnetURLThatIsHTTP(t *testing.T) {
	body, hash := sampleTorrent(t)
	srv := indexer(t, body, nil)
	cases := map[string]struct {
		rel        prowlarr.Release
		torrent    bool
		magnetHash string
	}{
		// Prowlarr usually proxies the magnet link through itself.
		"redirects to a magnet": {rel: prowlarr.Release{MagnetURL: srv.URL + "/magnet?apikey=secret"}, magnetHash: "0123456789abcdef0123456789abcdef01234567"},
		"serves a torrent file": {rel: prowlarr.Release{MagnetURL: srv.URL + "/torrent?apikey=secret"}, torrent: true},
		"after the download link fails": {
			rel:        prowlarr.Release{DownloadURL: srv.URL + "/html", MagnetURL: srv.URL + "/magnet"},
			magnetHash: "0123456789abcdef0123456789abcdef01234567",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			we := newWorkerEnv(t)
			r := c.rel
			r.GUID, r.Title, r.Protocol = "g", "Thing", "torrent"
			req := we.approved(t, r)

			require.NoError(t, we.w.Tick(context.Background()))

			we.get(t, req.ID, Downloading)
			require.Len(t, we.fc.adds, 1)
			if c.torrent {
				assert.Equal(t, body, we.fc.adds[0].TorrentFile)
				assert.Equal(t, hash, we.fc.adds[0].InfoHash)
			} else {
				assert.Equal(t, testMagnet, we.fc.adds[0].MagnetURI)
				assert.Equal(t, c.magnetHash, we.fc.adds[0].InfoHash)
			}
		})
	}
}

func TestWorkerGrabBadMagnetURLFails(t *testing.T) {
	srv := indexer(t, nil, nil)
	for _, link := range []string{srv.URL + "/gone?apikey=secret", "ftp://idx.example/x?apikey=secret", "javascript:alert(1)"} {
		we := newWorkerEnv(t)
		req := we.approved(t, prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", MagnetURL: link})

		require.NoError(t, we.w.Tick(context.Background()))

		got := we.get(t, req.ID, Failed)
		assert.NotEmpty(t, got.ErrorDetail, link)
		assert.NotContains(t, got.ErrorDetail, "secret", link)
		assert.Empty(t, we.fc.adds, link)
	}
}

func TestWorkerGrabStaleURLFails(t *testing.T) {
	we := newWorkerEnv(t)
	srv := indexer(t, nil, nil)
	req := we.approved(t, prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", DownloadURL: srv.URL + "/gone?apikey=secret"})

	require.NoError(t, we.w.Tick(context.Background()))

	got := we.get(t, req.ID, Failed)
	assert.Equal(t, "The download couldn't be started. An admin has the details.", got.Error)
	assert.Contains(t, got.ErrorDetail, "404")
	assert.Contains(t, got.ErrorDetail, srv.URL+"/gone")
	assert.NotContains(t, got.ErrorDetail, "secret")
	assert.Empty(t, we.fc.adds)
	assert.True(t, hasNotification(we.notifications(t, we.user.UserID), notify.EvRequestFailed))
	assert.True(t, hasNotification(we.notifications(t, we.admin.UserID), notify.EvRequestFailed))
}

func TestWorkerGrabWithoutClientFails(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.approved(t, prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", MagnetURL: testMagnet})
	_, err := we.svc.db.Exec("UPDATE download_clients SET enabled = 0")
	require.NoError(t, err)

	require.NoError(t, we.w.Tick(context.Background()))

	got := we.get(t, req.ID, Failed)
	assert.Equal(t, "No compatible download client", got.Error)
}

func TestWorkerTracksProgress(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.downloading(t, "a")
	adminCh, cancelAdmin := we.b.Subscribe(we.admin.UserID, true)
	defer cancelAdmin()
	userCh, cancelUser := we.b.Subscribe(we.user.UserID, false)
	defer cancelUser()

	we.advance(time.Hour)
	we.fc.setItem(downloadclient.Item{State: downloadclient.StateDownloading, Progress: 0.4})
	require.NoError(t, we.w.Tick(context.Background()))

	got := we.get(t, req.ID, Downloading)
	assert.InDelta(t, 0.4, got.Progress, 1e-9)
	assert.Equal(t, fixedNow.Add(time.Hour).Unix(), we.row(t, req.ID).LastProgressAt.Int64)
	assert.Equal(t, events.TypeActivity, (<-adminCh).Type)
	assert.Equal(t, events.TypeRequestUpdated, (<-userCh).Type)

	// No progress: last_progress_at stays put and nothing is published.
	we.advance(time.Hour)
	require.NoError(t, we.w.Tick(context.Background()))
	assert.Equal(t, fixedNow.Add(time.Hour).Unix(), we.row(t, req.ID).LastProgressAt.Int64)
	assert.Empty(t, adminCh)
	assert.Empty(t, userCh)
}

// completedItem lays out a finished two-file download under dir.
func completedItem(t *testing.T, dir, savePath string) downloadclient.Item {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Thing"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Thing", "setup.dmg"), []byte("binary"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Thing", "readme.txt"), []byte("hi"), 0o644))
	return downloadclient.Item{
		Name: "Thing", SavePath: savePath, Progress: 1, State: downloadclient.StateCompleted,
		Files: []downloadclient.File{{Path: "Thing/setup.dmg", Size: 6}, {Path: "Thing/readme.txt", Size: 2}},
	}
}

func (we workerEnv) assertImported(t *testing.T, id int64) {
	t.Helper()
	got := we.get(t, id, Available)
	require.NotNil(t, got.LibraryItemID)
	var folder, status, title string
	var size int64
	require.NoError(t, we.svc.db.QueryRow("SELECT folder, status, display_title, total_size FROM library_items WHERE id = ? AND request_id = ?", *got.LibraryItemID, id).
		Scan(&folder, &status, &title, &size))
	assert.Equal(t, "available", status)
	assert.Equal(t, got.DisplayTitle, title)
	assert.Equal(t, int64(8), size)
	assert.Equal(t, we.library, filepath.Dir(folder))
	var files int
	require.NoError(t, we.svc.db.QueryRow("SELECT COUNT(*) FROM library_files WHERE item_id = ?", *got.LibraryItemID).Scan(&files))
	assert.Equal(t, 2, files)
	b, err := os.ReadFile(filepath.Join(folder, "setup.dmg"))
	require.NoError(t, err)
	assert.Equal(t, "binary", string(b))
}

func TestWorkerCompletesAndImports(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.downloading(t, "a")
	dl := t.TempDir()
	we.fc.setItem(completedItem(t, dl, dl))
	ch, cancel := we.b.Subscribe(we.other.UserID, false)
	defer cancel()

	require.NoError(t, we.w.Tick(context.Background()))
	we.get(t, req.ID, Importing)

	require.NoError(t, we.w.Tick(context.Background()))
	we.assertImported(t, req.ID)
	assert.True(t, hasNotification(we.notifications(t, we.user.UserID), notify.EvItemAvailable))
	assert.Equal(t, events.TypeLibraryUpdated, (<-ch).Type)
	// The download client's copy is untouched.
	_, err := os.Stat(filepath.Join(dl, "Thing", "setup.dmg"))
	assert.NoError(t, err)
}

func TestWorkerImportRedoesAfterRecordFails(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.downloading(t, "a")
	dl := t.TempDir()
	we.fc.setItem(completedItem(t, dl, dl))
	require.NoError(t, we.w.Tick(context.Background()))
	we.get(t, req.ID, Importing)

	// The files land in the library but the item can't be recorded.
	_, err := we.svc.db.Exec(`CREATE TRIGGER fail_item BEFORE INSERT ON library_items BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	require.NoError(t, err)
	require.NoError(t, we.w.Tick(context.Background()))
	we.get(t, req.ID, Importing)
	assert.Equal(t, filepath.Join(we.library, "a"), we.row(t, req.ID).ImportFolder.String)
	// A copy killed part way leaves a temp file behind.
	require.NoError(t, os.WriteFile(filepath.Join(we.library, "a", ".softwarr-tmp-123"), []byte("x"), 0o644))

	_, err = we.svc.db.Exec(`DROP TRIGGER fail_item`)
	require.NoError(t, err)
	require.NoError(t, we.w.Tick(context.Background()))
	we.assertImported(t, req.ID)

	ents, err := os.ReadDir(we.library)
	require.NoError(t, err)
	require.Len(t, ents, 1, "one folder for the request, not a numbered second one")
	assert.Equal(t, "a", ents[0].Name())
	files, err := os.ReadDir(filepath.Join(we.library, "a"))
	require.NoError(t, err)
	assert.Len(t, files, 2)
	var items int
	require.NoError(t, we.svc.db.QueryRow("SELECT COUNT(*) FROM library_items WHERE request_id = ?", req.ID).Scan(&items))
	assert.Equal(t, 1, items)
}

func TestWorkerImportLeavesAnotherItemsFolderAlone(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.downloading(t, "a")
	dl := t.TempDir()
	we.fc.setItem(completedItem(t, dl, dl))
	require.NoError(t, we.w.Tick(context.Background()))
	we.get(t, req.ID, Importing)
	// An earlier attempt claimed "a", and since then a library item
	// took that name.
	taken := filepath.Join(we.library, "a")
	require.NoError(t, os.Mkdir(taken, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(taken, "keep.bin"), []byte("x"), 0o644))
	_, err := we.svc.db.Exec(`UPDATE requests SET import_folder = ? WHERE id = ?`, taken, req.ID)
	require.NoError(t, err)
	_, err = we.svc.db.Exec(`INSERT INTO library_items (display_title, release_name, release_key, folder, imported_at) VALUES ('x', 'x', 'k', ?, 1)`, taken)
	require.NoError(t, err)

	require.NoError(t, we.w.Tick(context.Background()))
	we.assertImported(t, req.ID)
	assert.FileExists(t, filepath.Join(taken, "keep.bin"))
	assert.DirExists(t, filepath.Join(we.library, "a (2)"))
}

func TestWorkerUsesPathMapping(t *testing.T) {
	we := newWorkerEnv(t)
	local := t.TempDir()
	_, err := we.q.CreatePathMapping(context.Background(), dbq.CreatePathMappingParams{Host: "qbit.lan", RemotePath: "/downloads", LocalPath: local})
	require.NoError(t, err)
	req := we.downloading(t, "a")
	we.fc.setItem(completedItem(t, local, "/downloads"))

	require.NoError(t, we.w.Tick(context.Background()))
	require.NoError(t, we.w.Tick(context.Background()))

	we.assertImported(t, req.ID)
}

func TestWorkerImportErrorFails(t *testing.T) {
	we := newWorkerEnv(t)
	hooks := make(chan map[string]any, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		hooks <- body
	}))
	defer receiver.Close()
	_, err := we.n.CreateWebhook(context.Background(), notify.WebhookInput{
		Name: "hook", URL: receiver.URL, Events: []string{notify.EvRequestFailed}, Enabled: true,
	})
	require.NoError(t, err)
	req := we.downloading(t, "a")
	it := completedItem(t, t.TempDir(), "/nowhere/downloads")
	we.fc.setItem(it)

	require.NoError(t, we.w.Tick(context.Background()))
	require.NoError(t, we.w.Tick(context.Background()))

	const userMsg = "The download finished but couldn't be added to the library. An admin has the details."
	got := we.get(t, req.ID, Failed)
	assert.Equal(t, userMsg, got.Error)
	assert.Contains(t, got.ErrorDetail, "Importing from /nowhere/downloads failed")

	// The requester sees the plain message, and no paths.
	mine, err := we.svc.Get(context.Background(), we.user, req.ID)
	require.NoError(t, err)
	b, err := json.Marshal(mine)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "/nowhere")
	assert.NotContains(t, string(b), "errorDetail")
	list, err := we.svc.List(context.Background(), we.user, true)
	require.NoError(t, err)
	b, err = json.Marshal(list)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "/nowhere")

	userNotes := we.notifications(t, we.user.UserID)
	require.True(t, hasNotification(userNotes, notify.EvRequestFailed))
	for _, n := range userNotes {
		assert.NotContains(t, n.Message, "/nowhere")
	}
	adminNotes := we.notifications(t, we.admin.UserID)
	require.True(t, hasNotification(adminNotes, notify.EvRequestFailed))
	assert.Contains(t, adminNotes[0].Message, "/nowhere/downloads")

	select {
	case body := <-hooks:
		assert.Equal(t, userMsg, body["error"])
		assert.NotContains(t, body["message"], "/nowhere")
	case <-time.After(5 * time.Second):
		t.Fatal("no webhook")
	}
}

func TestWorkerClientMissingFails(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.downloading(t, "a")
	we.fc.setItem(downloadclient.Item{State: downloadclient.StateMissing})

	require.NoError(t, we.w.Tick(context.Background()))

	got := we.get(t, req.ID, Failed)
	assert.NotEmpty(t, got.Error)
	assert.True(t, hasNotification(we.notifications(t, we.user.UserID), notify.EvRequestFailed))
}

func TestWorkerRetryReattaches(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.downloading(t, "a")
	we.fc.setItem(downloadclient.Item{State: downloadclient.StateError, Message: "Tracker is down."})
	require.NoError(t, we.w.Tick(context.Background()))
	got := we.get(t, req.ID, Failed)
	assert.Equal(t, "The download client reported an error. An admin has the details.", got.Error)
	assert.Equal(t, "Tracker is down.", got.ErrorDetail)

	got, err := we.svc.Retry(context.Background(), we.admin, req.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Error)
	assert.Empty(t, got.ErrorDetail, "the detail goes with the error")
	we.fc.setItem(downloadclient.Item{State: downloadclient.StateDownloading, Progress: 0.5})
	require.NoError(t, we.w.Tick(context.Background()))

	we.get(t, req.ID, Downloading)
	adds, _ := we.fc.calls()
	assert.Equal(t, 1, adds)
	assert.Equal(t, "hash1", we.row(t, req.ID).ClientItemID)
}

func TestWorkerRetryGrabsAgainWhenClientLostIt(t *testing.T) {
	we := newWorkerEnv(t)
	req := we.downloading(t, "a")
	we.fc.setItem(downloadclient.Item{State: downloadclient.StateMissing})
	require.NoError(t, we.w.Tick(context.Background()))
	we.get(t, req.ID, Failed)

	_, err := we.svc.Retry(context.Background(), we.admin, req.ID)
	require.NoError(t, err)
	require.NoError(t, we.w.Tick(context.Background()))

	we.get(t, req.ID, Downloading)
	adds, _ := we.fc.calls()
	assert.Equal(t, 2, adds)
}

func TestWorkerSkipsAbandoned(t *testing.T) {
	we := newWorkerEnv(t)
	ctx := context.Background()

	// Abandoned while the client is being polled.
	req := we.downloading(t, "a")
	we.fc.getFn = func(id string) (downloadclient.Item, error) {
		_, err := we.svc.Abandon(ctx, we.admin, req.ID)
		require.NoError(t, err)
		return downloadclient.Item{ID: id, State: downloadclient.StateCompleted, Progress: 1}, nil
	}
	require.NoError(t, we.w.Tick(ctx))
	we.get(t, req.ID, Abandoned)
	_, gets := we.fc.calls()
	require.NoError(t, we.w.Tick(ctx))
	_, after := we.fc.calls()
	assert.Equal(t, gets, after)

	// Abandoned while the torrent is being fetched: it is never added.
	var id int64
	srv := indexer(t, nil, func() {
		_, err := we.svc.Abandon(ctx, we.admin, id)
		assert.NoError(t, err) // runs on the server's goroutine
	})
	id = we.approved(t, prowlarr.Release{GUID: "g-b", Title: "b", Protocol: "torrent", DownloadURL: srv.URL + "/magnet"}).ID
	adds, _ := we.fc.calls()
	require.NoError(t, we.w.Tick(ctx))
	we.get(t, id, Abandoned)
	addsAfter, _ := we.fc.calls()
	assert.Equal(t, adds, addsAfter)
}

func TestWorkerOneFailureDoesNotStopTick(t *testing.T) {
	we := newWorkerEnv(t)
	a := we.downloading(t, "a")
	b := we.downloading(t, "b")
	we.fc.getFn = func(id string) (downloadclient.Item, error) {
		return downloadclient.Item{}, assert.AnError
	}
	// Both are polled even though each poll errors.
	require.NoError(t, we.w.Tick(context.Background()))
	_, gets := we.fc.calls()
	assert.GreaterOrEqual(t, gets, 2)
	we.get(t, a.ID, Downloading)
	we.get(t, b.ID, Downloading)
}

func TestWorkerRunStopsOnCancel(t *testing.T) {
	we := newWorkerEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		we.w.Run(ctx, time.Millisecond)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestActivityStalledFlag(t *testing.T) {
	we := newWorkerEnv(t)
	ctx := context.Background()
	stalled := we.downloading(t, "a")
	fresh := we.downloading(t, "b")
	_, err := we.svc.db.Exec("UPDATE requests SET last_progress_at = ? WHERE id = ?", fixedNow.Add(-25*time.Hour).Unix(), stalled.ID)
	require.NoError(t, err)
	_, err = we.svc.db.Exec("UPDATE requests SET last_progress_at = ? WHERE id = ?", fixedNow.Add(-23*time.Hour).Unix(), fresh.ID)
	require.NoError(t, err)
	pending, err := we.svc.Create(ctx, we.user, rel("c"), "")
	require.NoError(t, err)

	list, err := we.svc.Activity(ctx)
	require.NoError(t, err)
	byID := map[int64]Activity{}
	for _, a := range list {
		byID[a.RequestID] = a
	}
	require.Len(t, byID, 2)
	assert.NotContains(t, byID, pending.ID)
	assert.True(t, byID[stalled.ID].Stalled)
	assert.False(t, byID[fresh.ID].Stalled)
	assert.Equal(t, "a", byID[stalled.ID].Title)
	assert.Equal(t, "user", byID[stalled.ID].Requester)
	assert.Equal(t, Downloading, byID[stalled.ID].Status)

	// No notification is sent for a stall.
	for _, n := range we.notifications(t, we.admin.UserID) {
		assert.NotEqual(t, notify.EvRequestFailed, n.Type)
	}
}
