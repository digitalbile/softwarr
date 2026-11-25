package requests

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/internal/testutil"
)

var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type env struct {
	svc    *Service
	q      *dbq.Queries
	st     *settings.Store
	b      *events.Broker
	n      *notify.Service
	admin  auth.Principal
	user   auth.Principal
	other  auth.Principal
	flaggd auth.Principal
}

func newEnv(t *testing.T) env {
	t.Helper()
	d, q := testutil.NewDB(t)
	st := settings.NewStore(q)
	b := events.NewBroker()
	n := notify.New(q, b, st, http.DefaultClient, func() time.Time { return fixedNow })
	users := auth.NewUsers(d, q)
	ctx := context.Background()
	mk := func(name string, admin, auto bool) auth.Principal {
		u, err := users.CreateLocal(ctx, name, "", "correct horse", admin)
		require.NoError(t, err)
		if auto {
			_, err = d.Exec("UPDATE users SET auto_approve = 1 WHERE id = ?", u.ID)
			require.NoError(t, err)
		}
		return auth.Principal{UserID: u.ID, Username: name, IsAdmin: admin, AutoApprove: auto}
	}
	e := env{
		svc: NewService(d, q, st, n, b, func() time.Time { return fixedNow }),
		q:   q, st: st, b: b, n: n,
		admin: mk("admin", true, false), user: mk("user", false, false),
		other: mk("other", false, false), flaggd: mk("flagged", false, true),
	}
	_, err := q.CreateDownloadClient(ctx, dbq.CreateDownloadClientParams{Name: "qb", Type: "qbittorrent", Protocol: "torrent", Enabled: 1, Settings: "{}"})
	require.NoError(t, err)
	e.setSettings(t, func(s *settings.Settings) { s.LibraryRoot = "/library" })
	return e
}

func (e env) setSettings(t *testing.T, f func(*settings.Settings)) {
	t.Helper()
	s, err := e.st.Get(context.Background())
	require.NoError(t, err)
	f(&s)
	require.NoError(t, e.st.Save(context.Background(), s))
}

func rel(name string) prowlarr.Release {
	return prowlarr.Release{GUID: "g-" + name, Title: name, Protocol: "torrent", MagnetURL: "magnet:?xt=urn:btih:abc"}
}

func TestCreatePendingForUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel("a"), "  need it  ")
	require.NoError(t, err)
	assert.Equal(t, Pending, r.Status)
	assert.Equal(t, "need it", r.Comment)
	assert.Equal(t, "user", r.Requester)
	assert.Equal(t, "a", r.Release.Title)

	n, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: e.admin.UserID, Column2: 0})
	require.NoError(t, err)
	require.Len(t, n, 1)
	assert.Equal(t, notify.EvRequestCreated, n[0].Type)
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.svc.Create(ctx, e.user, prowlarr.Release{Protocol: "torrent", MagnetURL: "m"}, "")
	assert.True(t, IsInputError(err))
	_, err = e.svc.Create(ctx, e.user, prowlarr.Release{Title: "x", Protocol: "ftp"}, "")
	assert.True(t, IsInputError(err))
	_, err = e.svc.Create(ctx, e.user, prowlarr.Release{Title: "x", Protocol: "torrent"}, "")
	require.Error(t, err)
	assert.True(t, IsInputError(err))
	assert.Equal(t, "This release has no download link.", err.Error())
}

func TestCreateAutoApprovedForAdminAndFlaggedUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i, p := range []auth.Principal{e.admin, e.flaggd} {
		r, err := e.svc.Create(ctx, p, rel(string(rune('a'+i))), "")
		require.NoError(t, err)
		assert.Equal(t, Approved, r.Status)
		var by int64
		require.NoError(t, e.svc.db.QueryRow("SELECT approved_by FROM requests WHERE id = ?", r.ID).Scan(&by))
		assert.Equal(t, p.UserID, by)
	}
	n, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: e.admin.UserID, Column2: 0})
	require.NoError(t, err)
	assert.Empty(t, n)
}

func TestCreateCommentRequired(t *testing.T) {
	e := newEnv(t)
	e.setSettings(t, func(s *settings.Settings) { s.CommentRequired = true })
	_, err := e.svc.Create(context.Background(), e.user, rel("a"), "   ")
	assert.ErrorIs(t, err, ErrCommentRequired)
	_, err = e.svc.Create(context.Background(), e.user, rel("a"), "because")
	assert.NoError(t, err)
}

func TestCreateDuplicateRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)
	_, err = e.svc.Create(ctx, e.other, rel("a"), "")
	assert.ErrorIs(t, err, ErrDuplicate)
	assert.Equal(t, "This release has already been requested.", err.Error())
}

func TestCreateAfterDenyAllowed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)
	_, err = e.svc.Deny(ctx, e.admin, r.ID, "no")
	require.NoError(t, err)
	_, err = e.svc.Create(ctx, e.user, rel("a"), "")
	assert.NoError(t, err)
}

func TestCreateInLibraryRejected(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.db.Exec(`INSERT INTO library_items (display_title, release_name, release_key, folder, imported_at) VALUES ('a','a',?, '/library/a', 1)`, rel("a").Key())
	require.NoError(t, err)
	_, err = e.svc.Create(context.Background(), e.user, rel("a"), "")
	assert.ErrorIs(t, err, ErrInLibrary)
}

func TestCreateNoClientRejected(t *testing.T) {
	e := newEnv(t)
	r := rel("a")
	r.Protocol = "usenet"
	r.DownloadURL = "http://x/nzb"
	_, err := e.svc.Create(context.Background(), e.user, r, "")
	assert.ErrorIs(t, err, ErrNoClient)
}

func TestApproveWithReplacementKeepsOriginal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)
	repl := rel("b")
	got, err := e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{DisplayTitle: "Nice B", Replacement: &repl})
	require.NoError(t, err)
	assert.Equal(t, Approved, got.Status)
	assert.Equal(t, "b", got.Release.Title)
	require.NotNil(t, got.OriginalRelease)
	assert.Equal(t, "a", got.OriginalRelease.Title)
	assert.Equal(t, "Nice B", got.DisplayTitle)

	// The replacement's key is now the open key; a request for it is a duplicate.
	_, err = e.svc.Create(ctx, e.other, rel("b"), "")
	assert.ErrorIs(t, err, ErrDuplicate)

	// Replacing with a release another request already holds is a duplicate.
	r2, err := e.svc.Create(ctx, e.other, rel("c"), "")
	require.NoError(t, err)
	repl2 := rel("b")
	_, err = e.svc.Approve(ctx, e.admin, r2.ID, ApproveOpts{Replacement: &repl2})
	assert.ErrorIs(t, err, ErrDuplicate)
	again, err := e.svc.Get(ctx, e.admin, r2.ID)
	require.NoError(t, err)
	assert.Equal(t, Pending, again.Status)

	bad := prowlarr.Release{Title: "u", Protocol: "usenet", DownloadURL: "x"}
	_, err = e.svc.Approve(ctx, e.admin, r2.ID, ApproveOpts{Replacement: &bad})
	assert.ErrorIs(t, err, ErrNoClient)
}

func TestApproveRequiresLibraryRoot(t *testing.T) {
	e := newEnv(t)
	e.setSettings(t, func(s *settings.Settings) { s.LibraryRoot = "" })
	r, err := e.svc.Create(context.Background(), e.user, rel("a"), "")
	require.NoError(t, err)
	_, err = e.svc.Approve(context.Background(), e.admin, r.ID, ApproveOpts{})
	assert.ErrorIs(t, err, ErrNoLibraryRoot)
}

func TestApproveTwiceSecondGetsConflict(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{})
		}()
	}
	close(start)
	wg.Wait()

	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrConflict):
			conflict++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	assert.Equal(t, 1, ok)
	assert.Equal(t, 1, conflict)
}

func TestTransitionMissingIsNotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Approve(context.Background(), e.admin, 999, ApproveOpts{})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCancelOnlyOwnPending(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)

	_, err = e.svc.Cancel(ctx, e.other, r.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = e.svc.Cancel(ctx, e.admin, r.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	got, err := e.svc.Cancel(ctx, e.user, r.ID)
	require.NoError(t, err)
	assert.Equal(t, Cancelled, got.Status)

	r2, err := e.svc.Create(ctx, e.user, rel("b"), "")
	require.NoError(t, err)
	_, err = e.svc.Approve(ctx, e.admin, r2.ID, ApproveOpts{})
	require.NoError(t, err)
	_, err = e.svc.Cancel(ctx, e.user, r2.ID)
	assert.ErrorIs(t, err, ErrConflict)
}

func TestRetryAndAbandon(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.admin, rel("a"), "")
	require.NoError(t, err)
	_, err = e.svc.transition(ctx, r.ID, ActGrabFailed, func(p *dbq.UpdateRequestParams) { p.Error = "boom" })
	require.NoError(t, err)
	got, err := e.svc.Retry(ctx, e.admin, r.ID)
	require.NoError(t, err)
	assert.Equal(t, Approved, got.Status)
	assert.Empty(t, got.Error)
	got, err = e.svc.Abandon(ctx, e.admin, r.ID)
	require.NoError(t, err)
	assert.Equal(t, Abandoned, got.Status)
}

func TestListScopes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)
	_, err = e.svc.Create(ctx, e.other, rel("b"), "")
	require.NoError(t, err)
	l, err := e.svc.List(ctx, e.user, true)
	require.NoError(t, err)
	assert.Len(t, l, 1)
	l, err = e.svc.List(ctx, e.admin, true)
	require.NoError(t, err)
	assert.Len(t, l, 2)
	_, err = e.svc.Get(ctx, e.user, l[0].ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestNotificationsOnApproveAndDeny(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)
	b, err := e.svc.Create(ctx, e.user, rel("b"), "")
	require.NoError(t, err)

	ch, cancel := e.b.Subscribe(e.user.UserID, false)
	defer cancel()

	_, err = e.svc.Approve(ctx, e.admin, a.ID, ApproveOpts{})
	require.NoError(t, err)
	_, err = e.svc.Deny(ctx, e.admin, b.ID, "not now")
	require.NoError(t, err)

	n, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: e.user.UserID, Column2: 0})
	require.NoError(t, err)
	types := map[string]string{}
	for _, x := range n {
		types[x.Type] = x.Message
	}
	assert.Contains(t, types, notify.EvRequestApproved)
	assert.Contains(t, types[notify.EvRequestDenied], "not now")

	var updated int
	for len(ch) > 0 {
		m := <-ch
		if m.Type == events.TypeRequestUpdated {
			updated++
			d := m.Data.(map[string]any)
			assert.Contains(t, d, "id")
			assert.Contains(t, d, "status")
			assert.Contains(t, d, "progress")
		}
	}
	assert.Equal(t, 2, updated)
}

func TestAutoApproveNeedsLibraryRoot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setSettings(t, func(s *settings.Settings) { s.LibraryRoot = "" })
	r, err := e.svc.Create(ctx, e.admin, rel("a"), "")
	require.NoError(t, err)
	assert.Equal(t, Pending, r.Status)
	n, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: e.admin.UserID, Column2: 0})
	require.NoError(t, err)
	require.Len(t, n, 1)
	assert.Equal(t, notify.EvRequestCreated, n[0].Type)

	e.setSettings(t, func(s *settings.Settings) { s.LibraryRoot = "/library" })
	r, err = e.svc.Create(ctx, e.admin, rel("b"), "")
	require.NoError(t, err)
	assert.Equal(t, Approved, r.Status)
}

func TestApproveReplacementInLibraryRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)
	_, err = e.svc.db.Exec(`INSERT INTO library_items (display_title, release_name, release_key, folder, imported_at) VALUES ('b','b',?, '/library/b', 1)`, rel("b").Key())
	require.NoError(t, err)
	repl := rel("b")
	_, err = e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{Replacement: &repl})
	assert.ErrorIs(t, err, ErrInLibrary)
	got, err := e.svc.Get(ctx, e.admin, r.ID)
	require.NoError(t, err)
	assert.Equal(t, Pending, got.Status)
	assert.Equal(t, "a", got.Release.Title)
}

// failedDownload walks a request through a download that failed in the client.
func (e env) failedDownload(t *testing.T, name string) Request {
	t.Helper()
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel(name), "")
	require.NoError(t, err)
	_, err = e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{})
	require.NoError(t, err)
	_, err = e.svc.transition(ctx, r.ID, ActGrabbed, func(u *dbq.UpdateRequestParams) {
		u.ClientID = sql.NullInt64{Int64: 1, Valid: true}
		u.ClientItemID = "hash-" + name
		u.Progress = 0.4
		u.LastProgressAt = sql.NullInt64{Int64: 100, Valid: true}
	})
	require.NoError(t, err)
	got, err := e.svc.transition(ctx, r.ID, ActClientError, func(u *dbq.UpdateRequestParams) { u.Error = "tracker gone" })
	require.NoError(t, err)
	require.Equal(t, Failed, got.Status)
	return got
}

func TestReplaceReleaseOnFailedRequest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r := e.failedDownload(t, "a")

	_, err := e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{})
	require.Error(t, err, "a failed request needs a replacement; retry covers the rest")
	assert.True(t, IsInputError(err))

	repl := rel("b")
	got, err := e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{DisplayTitle: "Nice B", Replacement: &repl})
	require.NoError(t, err)
	assert.Equal(t, Approved, got.Status)
	assert.Equal(t, "b", got.Release.Title)
	require.NotNil(t, got.OriginalRelease)
	assert.Equal(t, "a", got.OriginalRelease.Title)
	assert.Equal(t, "Nice B", got.DisplayTitle)
	assert.Empty(t, got.Error)
	assert.Zero(t, got.Progress)

	// The worker must grab the new release, not re-attach to the old torrent.
	row, err := e.q.GetRequest(ctx, r.ID)
	require.NoError(t, err)
	assert.False(t, row.ClientID.Valid)
	assert.Empty(t, row.ClientItemID)
	assert.False(t, row.LastProgressAt.Valid)
	assert.Equal(t, rel("b").Key(), row.ReleaseKey)

	n, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: e.user.UserID, Column2: 0})
	require.NoError(t, err)
	assert.Equal(t, notify.EvRequestApproved, n[0].Type)
}

func TestReplaceOnFailedKeepsFirstOriginal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.Create(ctx, e.user, rel("a"), "")
	require.NoError(t, err)
	b := rel("b")
	_, err = e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{Replacement: &b})
	require.NoError(t, err)
	_, err = e.svc.transition(ctx, r.ID, ActGrabFailed, func(u *dbq.UpdateRequestParams) { u.Error = "stale" })
	require.NoError(t, err)
	c := rel("c")
	got, err := e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{Replacement: &c})
	require.NoError(t, err)
	assert.Equal(t, "c", got.Release.Title)
	require.NotNil(t, got.OriginalRelease)
	assert.Equal(t, "a", got.OriginalRelease.Title, "the release the user asked for stays as history")
}

func TestReplaceOnFailedChecks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r := e.failedDownload(t, "a")

	_, err := e.svc.db.Exec(`INSERT INTO library_items (display_title, release_name, release_key, folder, imported_at) VALUES ('b','b',?, '/library/b', 1)`, rel("b").Key())
	require.NoError(t, err)
	b := rel("b")
	_, err = e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{Replacement: &b})
	assert.ErrorIs(t, err, ErrInLibrary)

	_, err = e.svc.Create(ctx, e.other, rel("c"), "")
	require.NoError(t, err)
	c := rel("c")
	_, err = e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{Replacement: &c})
	assert.ErrorIs(t, err, ErrDuplicate)

	u := prowlarr.Release{Title: "u", Protocol: "usenet", DownloadURL: "x"}
	_, err = e.svc.Approve(ctx, e.admin, r.ID, ApproveOpts{Replacement: &u})
	assert.ErrorIs(t, err, ErrNoClient)

	got, err := e.svc.Get(ctx, e.admin, r.ID)
	require.NoError(t, err)
	assert.Equal(t, Failed, got.Status)
	assert.Equal(t, "tracker gone", got.Error)
	assert.Equal(t, "a", got.Release.Title)
}

func TestRetryChecksLikeApprove(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r := e.failedDownload(t, "a")
	status := func() Status {
		got, err := e.svc.Get(ctx, e.admin, r.ID)
		require.NoError(t, err)
		return got.Status
	}

	e.setSettings(t, func(s *settings.Settings) { s.LibraryRoot = "" })
	_, err := e.svc.Retry(ctx, e.admin, r.ID)
	assert.ErrorIs(t, err, ErrNoLibraryRoot)
	assert.Equal(t, Failed, status())
	e.setSettings(t, func(s *settings.Settings) { s.LibraryRoot = "/library" })

	_, err = e.svc.db.Exec(`UPDATE download_clients SET enabled = 0`)
	require.NoError(t, err)
	_, err = e.svc.Retry(ctx, e.admin, r.ID)
	assert.ErrorIs(t, err, ErrNoClient)
	assert.Equal(t, Failed, status())
	_, err = e.svc.db.Exec(`UPDATE download_clients SET enabled = 1`)
	require.NoError(t, err)

	_, err = e.svc.db.Exec(`INSERT INTO library_items (display_title, release_name, release_key, folder, imported_at) VALUES ('a','a',?, '/library/a', 1)`, rel("a").Key())
	require.NoError(t, err)
	_, err = e.svc.Retry(ctx, e.admin, r.ID)
	assert.ErrorIs(t, err, ErrInLibrary)
	assert.Equal(t, Failed, status())
}

func TestRetryOfRequestThatIsNotFailedConflicts(t *testing.T) {
	e := newEnv(t)
	r, err := e.svc.Create(context.Background(), e.user, rel("a"), "")
	require.NoError(t, err)
	_, err = e.svc.Retry(context.Background(), e.admin, r.ID)
	assert.ErrorIs(t, err, ErrConflict)
	_, err = e.svc.Retry(context.Background(), e.admin, 999)
	assert.ErrorIs(t, err, ErrNotFound)
}
