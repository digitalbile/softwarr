package library

import (
	"context"
	"database/sql"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/importer"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/internal/testutil"
)

type env struct {
	svc    *Service
	db     *sql.DB
	q      *dbq.Queries
	root   string
	now    time.Time
	clock  *time.Time
	broker *events.Broker
	st     *settings.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, q := testutil.NewDB(t)
	st := settings.NewStore(q)
	root := t.TempDir()
	cur, err := st.Get(context.Background())
	require.NoError(t, err)
	cur.LibraryRoot = root
	require.NoError(t, st.Save(context.Background(), cur))
	e := &env{db: d, q: q, root: root, st: st, broker: events.NewBroker()}
	e.now = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	cl := e.now
	e.clock = &cl
	n := notify.New(q, e.broker, st, nil, func() time.Time { return *e.clock })
	e.svc = NewService(q, st, n, e.broker, func() time.Time { return *e.clock })
	return e
}

// addItem writes files under root/name and records an item importedAt now.
func (e *env) addItem(t *testing.T, name string, files map[string]string) int64 {
	t.Helper()
	folder := filepath.Join(e.root, name)
	res := importer.Result{Folder: folder, Mode: importer.ModeHardlink}
	// Sorted, so the stored file order (and which file a test removes first)
	// is the same on every run.
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		content := files[rel]
		p := filepath.Join(folder, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		res.Files = append(res.Files, downloadclient.File{Path: rel, Size: int64(len(content))})
		res.TotalSize += int64(len(content))
	}
	rel := prowlarr.Release{GUID: name, Title: name, Indexer: "idx", Protocol: "torrent", InfoHash: name}
	id, err := CreateItem(context.Background(), e.q, 0, name+" title", rel, res, e.now)
	require.NoError(t, err)
	return id
}

// repoint sets the library root to dir.
func (e *env) repoint(t *testing.T, dir string) {
	t.Helper()
	cur, err := e.st.Get(context.Background())
	require.NoError(t, err)
	cur.LibraryRoot = dir
	require.NoError(t, e.st.Save(context.Background(), cur))
}

func TestRootRepointedAtDownloadsLeavesSeedingFiles(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setDefaultDays(t, e, 1)
	// The importer strips the torrent's top folder, so the items' files are
	// named exactly like the seeding copies'.
	deleted := e.addItem(t, "Thing", map[string]string{"setup.dmg": "payload"})
	expiring := e.addItem(t, "Other", map[string]string{"setup.dmg": "payload"})
	downloads := t.TempDir()
	var seeding []string
	for _, name := range []string{"Thing", "Other"} {
		p := filepath.Join(downloads, name, "setup.dmg")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("payload"), 0o644))
		seeding = append(seeding, p)
	}
	e.repoint(t, downloads)

	require.NoError(t, e.svc.Delete(ctx, deleted))
	*e.clock = e.now.Add(48 * time.Hour)
	n, err := e.svc.ApplyRetention(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Only the items' own folders, at their stored paths, were removed.
	assert.NoDirExists(t, filepath.Join(e.root, "Thing"))
	assert.NoDirExists(t, filepath.Join(e.root, "Other"))
	for _, p := range seeding {
		assert.FileExists(t, p)
	}
	assert.Equal(t, "deleted", itemStatus(t, e, deleted))
	assert.Equal(t, "expired", itemStatus(t, e, expiring))
}

func TestSameNamedFolderInNewRootIsLeftAlone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setDefaultDays(t, e, 1)
	gone := e.addItem(t, "Thing", map[string]string{"a.txt": "a"})
	expiring := e.addItem(t, "Other", map[string]string{"a.txt": "a"})
	other := t.TempDir()
	var theirs []string
	for _, name := range []string{"Thing", "Other"} {
		p := filepath.Join(other, name, "a.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("theirs"), 0o644))
		theirs = append(theirs, p)
	}
	e.repoint(t, other)
	// The old copies are gone too, so nothing at the stored paths remains.
	require.NoError(t, os.RemoveAll(e.root))

	require.NoError(t, e.svc.Delete(ctx, gone))
	*e.clock = e.now.Add(48 * time.Hour)
	n, err := e.svc.ApplyRetention(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	assert.Equal(t, "deleted", itemStatus(t, e, gone))
	assert.Equal(t, "expired", itemStatus(t, e, expiring))
	for _, p := range theirs {
		assert.FileExists(t, p)
	}
}

func TestExpiresAt(t *testing.T) {
	imp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dl := imp.Add(10 * 24 * time.Hour)
	zero, forty := 0, 40
	cases := []struct {
		name string
		it   Item
		def  int
		want *time.Time
	}{
		{"default applies", Item{ImportedAt: imp}, 30, ptr(imp.Add(30 * 24 * time.Hour))},
		{"override zero never", Item{ImportedAt: imp, RetentionDays: &zero}, 30, nil},
		{"default zero never", Item{ImportedAt: imp}, 0, nil},
		{"override beats default", Item{ImportedAt: imp, RetentionDays: &forty}, 0, ptr(imp.Add(40 * 24 * time.Hour))},
		{"later download moves it", Item{ImportedAt: imp, LastDownloadedAt: &dl}, 30, ptr(dl.Add(30 * 24 * time.Hour))},
		{"earlier download ignored", Item{ImportedAt: dl, LastDownloadedAt: &imp}, 30, ptr(dl.Add(30 * 24 * time.Hour))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExpiresAt(c.it, c.def)
			if c.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.True(t, c.want.Equal(*got), "want %v got %v", c.want, got)
		})
	}
}

func ptr[T any](v T) *T { return &v }

func setDefaultDays(t *testing.T, e *env, days int) {
	t.Helper()
	cur, err := e.st.Get(context.Background())
	require.NoError(t, err)
	cur.DefaultRetentionDays = days
	require.NoError(t, e.st.Save(context.Background(), cur))
}

func TestApplyRetentionRemovesOnlyLibraryCopy(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setDefaultDays(t, e, 30)

	// A download client file with a hardlink in the library.
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "setup.dmg")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o644))
	folder := filepath.Join(e.root, "Thing")
	require.NoError(t, os.Mkdir(folder, 0o755))
	require.NoError(t, os.Link(src, filepath.Join(folder, "setup.dmg")))
	rel := prowlarr.Release{GUID: "g", Title: "Thing", Protocol: "torrent", InfoHash: "x"}
	id, err := CreateItem(ctx, e.q, 0, "Thing", rel, importer.Result{
		Folder: folder, Mode: importer.ModeHardlink, TotalSize: 7,
		Files: []downloadclient.File{{Path: "setup.dmg", Size: 7}},
	}, e.now)
	require.NoError(t, err)
	keep := e.addItem(t, "Fresh", map[string]string{"a.txt": "a"})
	*e.clock = e.now.Add(29 * 24 * time.Hour)
	require.NoError(t, e.q.RecordLibraryDownload(ctx, dbq.RecordLibraryDownloadParams{LastDownloadedAt: nullTime(e.clock.Unix()), ID: keep}))

	n, err := e.svc.ApplyRetention(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nothing is old enough yet")

	*e.clock = e.now.Add(31 * 24 * time.Hour)
	n, err = e.svc.ApplyRetention(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the downloaded item was refreshed")

	assert.NoFileExists(t, filepath.Join(folder, "setup.dmg"))
	assert.NoDirExists(t, folder)
	b, err := os.ReadFile(src)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(b))

	var status string
	require.NoError(t, e.db.QueryRow("SELECT status FROM library_items WHERE id = ?", id).Scan(&status))
	assert.Equal(t, "expired", status)
	require.NoError(t, e.db.QueryRow("SELECT status FROM library_items WHERE id = ?", keep).Scan(&status))
	assert.Equal(t, "available", status)
}

func TestApplyRetentionEmitsItemExpired(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setDefaultDays(t, e, 1)
	_, err := e.db.Exec(`INSERT INTO users (id, username, created_at) VALUES (5, 'admin', 1)`)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE users SET is_admin = 1 WHERE id = 5`)
	require.NoError(t, err)
	id := e.addItem(t, "Old", map[string]string{"a.txt": "a"})
	ch, cancel := e.broker.Subscribe(5, true)
	defer cancel()

	*e.clock = e.now.Add(48 * time.Hour)
	n, err := e.svc.ApplyRetention(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var typ string
	require.NoError(t, e.db.QueryRow("SELECT type FROM notifications WHERE item_id = ?", id).Scan(&typ))
	assert.Equal(t, notify.EvItemExpired, typ)
	var sawLibrary bool
	for len(ch) > 0 {
		if (<-ch).Type == events.TypeLibraryUpdated {
			sawLibrary = true
		}
	}
	assert.True(t, sawLibrary)
}

func TestDeleteRefusesUnsafeFolders(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.addItem(t, "Item", map[string]string{"a.txt": "a"})

	for _, folder := range []string{"", "/", ".", "..", "relative/Item", e.root + "/..", e.root + "/./Item", "/Item"} {
		_, err := e.db.Exec("UPDATE library_items SET folder = ? WHERE id = ?", folder, id)
		require.NoError(t, err)
		require.ErrorIs(t, e.svc.Delete(ctx, id), ErrUnsafePath, "folder %q", folder)
	}
	assert.DirExists(t, filepath.Join(e.root, "Item"))
	assert.Equal(t, "available", itemStatus(t, e, id))
}

func TestOldItemsStayAtTheirFolderAfterRootChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.addItem(t, "Item", map[string]string{"setup.dmg": "payload"})
	setDefaultDays(t, e, 1)
	gone := e.addItem(t, "Gone", map[string]string{"a.txt": "a"})
	expiring := e.addItem(t, "Expiring", map[string]string{"a.txt": "a"})
	require.NoError(t, os.RemoveAll(filepath.Join(e.root, "Gone")))
	require.NoError(t, os.RemoveAll(filepath.Join(e.root, "Expiring")))
	e.repoint(t, t.TempDir())

	// Still at the old path: downloads and deletes there.
	it, err := e.svc.Get(ctx, id)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	require.NoError(t, e.svc.Download(rec, httptest.NewRequest("GET", "/dl", nil), it))
	assert.Equal(t, "payload", rec.Body.String())
	require.NoError(t, e.svc.Delete(ctx, id))
	assert.NoDirExists(t, filepath.Join(e.root, "Item"))

	// Gone from the old path: marked gone.
	require.NoError(t, e.svc.Delete(ctx, gone))
	assert.Equal(t, "deleted", itemStatus(t, e, gone))
	*e.clock = e.now.Add(48 * time.Hour)
	n, err := e.svc.ApplyRetention(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "expired", itemStatus(t, e, expiring))
}

func TestRootWithWhitespaceWorks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.addItem(t, "Item", map[string]string{"setup.dmg": "payload"})
	// As stored by a version that didn't normalize the root.
	raw := `{"libraryRoot": "` + e.root + ` "}`
	require.NoError(t, e.q.UpsertSetting(ctx, dbq.UpsertSettingParams{Key: "app", Value: raw}))
	e.svc.set = settings.NewStore(e.q)

	it, err := e.svc.Get(ctx, id)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	require.NoError(t, e.svc.Download(rec, httptest.NewRequest("GET", "/dl", nil), it))
	assert.Equal(t, "payload", rec.Body.String())
	require.NoError(t, e.svc.Delete(ctx, id))
	assert.NoDirExists(t, filepath.Join(e.root, "Item"))
}

func TestDeleteRemovesFolderAndMarksDeleted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.addItem(t, "Item", map[string]string{"a.txt": "a"})
	require.NoError(t, e.svc.Delete(ctx, id))
	assert.NoDirExists(t, filepath.Join(e.root, "Item"))
	_, err := e.svc.Get(ctx, id)
	assert.ErrorIs(t, err, ErrNotFound)
}

func itemStatus(t *testing.T, e *env, id int64) string {
	t.Helper()
	var status string
	require.NoError(t, e.db.QueryRow("SELECT status FROM library_items WHERE id = ?", id).Scan(&status))
	return status
}

func TestDeleteAndRetentionKeepItemWhenRootMissing(t *testing.T) {
	for name, unmount := range map[string]func(t *testing.T, e *env){
		// As if the library volume weren't mounted.
		"root gone": func(t *testing.T, e *env) { require.NoError(t, os.RemoveAll(e.root)) },
		// An empty mount point while the library has items in it, which is
		// what Docker leaves for a missing bind mount.
		"root empty": func(t *testing.T, e *env) { require.NoError(t, os.RemoveAll(filepath.Join(e.root, "Item"))) },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			setDefaultDays(t, e, 1)
			id := e.addItem(t, "Item", map[string]string{"a.txt": "a"})
			unmount(t, e)

			err := e.svc.Delete(ctx, id)
			require.ErrorIs(t, err, ErrFolderMissing)
			assert.Equal(t, "The library folder for this item is missing. Check that the library path is mounted.", err.Error())

			*e.clock = e.now.Add(48 * time.Hour)
			n, err := e.svc.ApplyRetention(ctx)
			require.NoError(t, err)
			assert.Zero(t, n)
			assert.Equal(t, "available", itemStatus(t, e, id))
		})
	}
}

func TestDeleteRemovesItemWhoseFolderIsGone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.addItem(t, "Item", map[string]string{"a.txt": "a"})
	e.addItem(t, "Other", map[string]string{"b.txt": "b"})
	require.NoError(t, os.RemoveAll(filepath.Join(e.root, "Item")))

	require.NoError(t, e.svc.Delete(ctx, id))
	assert.Equal(t, "deleted", itemStatus(t, e, id))
	assert.DirExists(t, filepath.Join(e.root, "Other"))
}

func TestRetentionExpiresItemWhoseFolderIsGone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	setDefaultDays(t, e, 1)
	id := e.addItem(t, "Item", map[string]string{"a.txt": "a"})
	require.NoError(t, os.RemoveAll(filepath.Join(e.root, "Item")))
	// Something else in the root shows the volume is there.
	require.NoError(t, os.WriteFile(filepath.Join(e.root, "notes.txt"), []byte("x"), 0o644))

	*e.clock = e.now.Add(48 * time.Hour)
	n, err := e.svc.ApplyRetention(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "expired", itemStatus(t, e, id))
}

func TestUpdateTitleAndRetention(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.addItem(t, "Item", map[string]string{"a.txt": "a"})

	it, err := e.svc.Update(ctx, id, ptr("New name"), nil)
	require.NoError(t, err)
	assert.Equal(t, "New name", it.Title)
	assert.Nil(t, it.RetentionDays)

	it, err = e.svc.Update(ctx, id, nil, ptr(ptr(5)))
	require.NoError(t, err)
	assert.Equal(t, "New name", it.Title)
	require.NotNil(t, it.RetentionDays)
	assert.Equal(t, 5, *it.RetentionDays)
	assert.NotNil(t, it.ExpiresAt)

	var none *int
	it, err = e.svc.Update(ctx, id, ptr(""), &none)
	require.NoError(t, err)
	assert.Equal(t, "Item", it.Title, "empty title restores the release name")
	assert.Nil(t, it.RetentionDays)

	_, err = e.svc.Update(ctx, id, nil, ptr(ptr(-1)))
	assert.ErrorIs(t, err, ErrInvalidRetention)
}

func TestListFilter(t *testing.T) {
	e := newEnv(t)
	e.addItem(t, "Alpha.v1", map[string]string{"a.txt": "a"})
	e.addItem(t, "Beta.v2", map[string]string{"b.txt": "b"})
	got, err := e.svc.List(context.Background(), "ALPHA")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Alpha.v1", got[0].ReleaseName)
	assert.Equal(t, []File{{Path: "a.txt", Size: 1}}, got[0].Files)
	all, err := e.svc.List(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestDeleteRefusesSymlinkedFolder(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	downloads := t.TempDir()
	victim := filepath.Join(downloads, "keep.bin")
	require.NoError(t, os.WriteFile(victim, []byte("x"), 0o644))
	id := e.addItem(t, "Item", map[string]string{"a.txt": "a"})

	// The folder itself is a symlink to a download client directory.
	link := filepath.Join(e.root, "link")
	require.NoError(t, os.Symlink(downloads, link))
	_, err := e.db.Exec("UPDATE library_items SET folder = ? WHERE id = ?", link, id)
	require.NoError(t, err)
	require.ErrorIs(t, e.svc.Delete(ctx, id), ErrUnsafePath)
	rec := httptest.NewRecorder()
	it, err := e.svc.Get(ctx, id)
	require.NoError(t, err)
	require.ErrorIs(t, e.svc.Download(rec, httptest.NewRequest("GET", "/dl", nil), it), ErrUnsafePath)
	assert.FileExists(t, victim)
}

func TestRootUnmounted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	check := func(root string) bool {
		t.Helper()
		got, err := RootUnmounted(ctx, e.q, root)
		require.NoError(t, err)
		return got
	}
	assert.False(t, check(e.root), "an empty root with no items is a fresh library")
	assert.True(t, check(filepath.Join(e.root, "missing")))
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	assert.True(t, check(file))

	e.addItem(t, "Item", map[string]string{"a.txt": "a"})
	assert.False(t, check(e.root))
	require.NoError(t, os.RemoveAll(filepath.Join(e.root, "Item")))
	assert.True(t, check(e.root), "empty while an item is recorded in it")
	assert.False(t, check(t.TempDir()), "items elsewhere don't count")
}
