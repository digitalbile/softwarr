package requests

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/importer"
	"github.com/digitalbile/softwarr/internal/library"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/pathmap"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/internal/torrentfile"
)

const (
	fetchTimeout   = 60 * time.Second
	maxTorrentSize = 10 << 20
	maxRedirects   = 10
)

// Worker moves approved requests through the download client and into the
// library.
type Worker struct {
	svc       *Service
	q         *dbq.Queries
	s         *settings.Store
	httpc     *http.Client
	newClient func(downloadclient.Config) (downloadclient.Client, error)
	now       func() time.Time

	mu      sync.Mutex
	clients map[int64]cachedClient
}

// cachedClient keeps a client between ticks so it can reuse its login. It is
// rebuilt when the stored config changes.
type cachedClient struct {
	cfg downloadclient.Config
	c   downloadclient.Client
}

// NewWorker builds a worker. httpc fetches torrent files from indexers;
// newClient builds download clients (nil means downloadclient.New).
func NewWorker(svc *Service, q *dbq.Queries, s *settings.Store, httpc *http.Client, newClient func(downloadclient.Config) (downloadclient.Client, error), now func() time.Time) *Worker {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	if newClient == nil {
		newClient = func(cfg downloadclient.Config) (downloadclient.Client, error) { return downloadclient.New(cfg, nil) }
	}
	if now == nil {
		now = time.Now
	}
	return &Worker{svc: svc, q: q, s: s, httpc: httpc, newClient: newClient, now: now, clients: map[int64]cachedClient{}}
}

// Run ticks now and then every interval until ctx is done.
func (w *Worker) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("request worker tick", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick makes one pass over approved, downloading, and importing requests.
// Each is handled on its own: a failure is logged and the pass goes on.
func (w *Worker) Tick(ctx context.Context) error {
	rows, err := w.q.ListWorkerRequests(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
		switch Status(row.Status) {
		case Approved:
			err = w.grab(ctx, row)
		case Downloading:
			err = w.poll(ctx, row)
		case Importing:
			err = w.importDownload(ctx, row)
		}
		if err != nil && ctx.Err() == nil {
			slog.Error("request worker", "request", row.ID, "status", row.Status, "err", err)
		}
	}
	return nil
}

func (w *Worker) grab(ctx context.Context, row dbq.Request) error {
	rel, err := releaseOf(row)
	if err != nil {
		return err
	}
	if row.ClientItemID != "" && row.ClientID.Valid {
		done, err := w.reattach(ctx, row)
		if done || err != nil {
			return err
		}
	}
	if rel.Protocol != "torrent" {
		return w.fail(ctx, row.ID, ActGrabFailed, "Only torrent downloads are supported.")
	}
	cfg, ok, err := downloadclient.PickClient(ctx, w.q, rel.Protocol)
	if err != nil {
		return err
	}
	if !ok {
		return w.fail(ctx, row.ID, ActGrabFailed, ErrNoClient.Error())
	}
	c, err := w.client(cfg)
	if err != nil {
		return w.fail(ctx, row.ID, ActGrabFailed, fmt.Sprintf("The download client %s couldn't be set up: %v", cfg.Name, err))
	}
	add, err := w.resolve(ctx, rel)
	if err != nil {
		return w.fail(ctx, row.ID, ActGrabFailed, err.Error())
	}
	// Fetching can take a while; don't add a torrent for a request an admin
	// abandoned meanwhile.
	if cur, err := w.q.GetRequest(ctx, row.ID); err != nil || Status(cur.Status) != Approved {
		return skipChanged(err)
	}
	id, err := c.Add(ctx, add)
	if err != nil {
		return w.fail(ctx, row.ID, ActGrabFailed, fmt.Sprintf("%s didn't accept the torrent: %v", cfg.Name, err))
	}
	now := w.now().Unix()
	_, err = w.svc.transition(ctx, row.ID, ActGrabbed, func(u *dbq.UpdateRequestParams) {
		u.ClientID = sql.NullInt64{Int64: cfg.ID, Valid: true}
		u.ClientItemID = id
		u.Progress = 0
		u.LastProgressAt = sql.NullInt64{Int64: now, Valid: true}
		u.Error = ""
	})
	return skipChanged(err)
}

// reattach handles a retried request the download client may still have. It
// reports whether the request is back to downloading.
func (w *Worker) reattach(ctx context.Context, row dbq.Request) (bool, error) {
	c, err := w.clientByID(ctx, row.ClientID.Int64)
	if errors.Is(err, errClientGone) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	it, err := c.Get(ctx, row.ClientItemID)
	if err != nil {
		return false, err
	}
	if it.State == downloadclient.StateMissing {
		return false, nil
	}
	now := w.now().Unix()
	_, err = w.svc.transition(ctx, row.ID, ActGrabbed, func(u *dbq.UpdateRequestParams) {
		u.LastProgressAt = sql.NullInt64{Int64: now, Valid: true}
		u.Error = ""
	})
	return true, skipChanged(err)
}

// resolve works out what to hand the download client: the torrent file or
// magnet behind DownloadURL (which may itself be a magnet), falling back to
// MagnetURL.
func (w *Worker) resolve(ctx context.Context, rel prowlarr.Release) (downloadclient.AddRequest, error) {
	if isMagnet(rel.DownloadURL) {
		return magnet(rel.DownloadURL)
	}
	if rel.DownloadURL != "" {
		add, err := w.fetch(ctx, rel.DownloadURL)
		if err == nil || rel.MagnetURL == "" {
			return add, err
		}
		slog.Warn("using the magnet link after the download link failed", "title", rel.Title, "err", err)
	}
	if rel.MagnetURL != "" {
		return magnet(rel.MagnetURL)
	}
	return downloadclient.AddRequest{}, errors.New("This release has no download link.")
}

// fetch downloads a torrent from an indexer. Error text never includes the
// URL's query string, which often carries an API key.
func (w *Worker) fetch(ctx context.Context, raw string) (downloadclient.AddRequest, error) {
	shown := redactURL(raw)
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return downloadclient.AddRequest{}, fmt.Errorf("The download link %s isn't valid.", shown)
	}
	hc := *w.httpc
	hc.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		// The client can't fetch a magnet link; hand back the redirect.
		if strings.EqualFold(r.URL.Scheme, "magnet") {
			return http.ErrUseLastResponse
		}
		if len(via) >= maxRedirects {
			return errors.New("too many redirects")
		}
		return nil
	}
	resp, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return downloadclient.AddRequest{}, fmt.Errorf("Downloading the torrent from %s failed: %v", shown, err)
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); isMagnet(loc) {
		return magnet(loc)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return downloadclient.AddRequest{}, fmt.Errorf("Downloading the torrent from %s failed with HTTP status %d.", shown, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentSize+1))
	if err != nil {
		return downloadclient.AddRequest{}, fmt.Errorf("Downloading the torrent from %s failed: %v", shown, err)
	}
	if len(body) > maxTorrentSize {
		return downloadclient.AddRequest{}, fmt.Errorf("The file at %s is too large to be a torrent.", shown)
	}
	hash, err := torrentfile.InfoHash(body)
	if err != nil {
		return downloadclient.AddRequest{}, fmt.Errorf("The download link %s didn't return a torrent file.", shown)
	}
	return downloadclient.AddRequest{TorrentFile: body, InfoHash: hash}, nil
}

func isMagnet(s string) bool {
	return len(s) > 7 && strings.EqualFold(s[:7], "magnet:")
}

func magnet(uri string) (downloadclient.AddRequest, error) {
	hash, err := torrentfile.ParseMagnet(uri)
	if err != nil {
		return downloadclient.AddRequest{}, errors.New("The magnet link for this release isn't valid.")
	}
	return downloadclient.AddRequest{MagnetURI: uri, InfoHash: hash}, nil
}

// redactURL drops the credentials, query, and fragment from a URL so it can
// be shown to users and logged.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "the download link"
	}
	u.User, u.RawQuery, u.Fragment, u.ForceQuery = nil, "", "", false
	return u.String()
}

func (w *Worker) poll(ctx context.Context, row dbq.Request) error {
	c, err := w.clientByID(ctx, row.ClientID.Int64)
	if errors.Is(err, errClientGone) {
		return w.fail(ctx, row.ID, ActClientError, errClientGone.Error())
	}
	if err != nil {
		return err
	}
	it, err := c.Get(ctx, row.ClientItemID)
	if err != nil {
		return err
	}
	now := w.now().Unix()
	switch it.State {
	case downloadclient.StateError, downloadclient.StateMissing:
		return w.fail(ctx, row.ID, ActClientError, clientMessage(it))
	case downloadclient.StateCompleted:
		_, err := w.svc.transition(ctx, row.ID, ActCompleted, func(u *dbq.UpdateRequestParams) {
			if u.Progress < 1 {
				u.Progress = 1
				u.LastProgressAt = sql.NullInt64{Int64: now, Valid: true}
			}
		})
		return skipChanged(err)
	}
	if it.Progress <= row.Progress {
		return nil
	}
	// A narrow write, not a transition: it only touches progress and only
	// while still downloading, so it can't undo an admin's abandon.
	n, err := w.q.UpdateRequestProgress(ctx, dbq.UpdateRequestProgressParams{
		Progress: it.Progress, LastProgressAt: sql.NullInt64{Int64: now, Valid: true}, UpdatedAt: now, ID: row.ID,
	})
	if err != nil || n == 0 {
		return err
	}
	data := map[string]any{"id": row.ID, "status": Downloading, "progress": it.Progress}
	w.svc.b.Publish(events.Message{Type: events.TypeActivity, Data: data, Admins: true})
	w.svc.b.Publish(events.Message{Type: events.TypeRequestUpdated, Data: data, UserIDs: []int64{row.RequesterID}})
	return nil
}

func clientMessage(it downloadclient.Item) string {
	if strings.TrimSpace(it.Message) != "" {
		return it.Message
	}
	if it.State == downloadclient.StateMissing {
		return "The download client no longer has this download."
	}
	return "The download client reported an error."
}

func (w *Worker) importDownload(ctx context.Context, row dbq.Request) error {
	rel, err := releaseOf(row)
	if err != nil {
		return err
	}
	st, err := w.s.Get(ctx)
	if err != nil {
		return err
	}
	root := strings.TrimSpace(st.LibraryRoot)
	if root == "" {
		return w.fail(ctx, row.ID, ActImportFailed, ErrNoLibraryRoot.Error())
	}
	c, err := w.clientByID(ctx, row.ClientID.Int64)
	if errors.Is(err, errClientGone) {
		return w.fail(ctx, row.ID, ActImportFailed, errClientGone.Error())
	}
	if err != nil {
		return err
	}
	it, err := c.Get(ctx, row.ClientItemID)
	if err != nil {
		return err
	}
	if it.State == downloadclient.StateMissing {
		return w.fail(ctx, row.ID, ActImportFailed, clientMessage(it))
	}
	ms, err := pathmap.Load(ctx, w.q)
	if err != nil {
		return err
	}
	base := pathmap.Map(ms, c.Host(), it.SavePath)
	res, err := importer.Import(ctx, importer.Source{BaseDir: base, Files: it.Files, Protocol: rel.Protocol}, root, rel.Title)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return w.fail(ctx, row.ID, ActImportFailed, fmt.Sprintf("Importing from %s failed: %v", base, err))
	}

	req, itemID, err := w.record(ctx, row, rel, res)
	if err != nil {
		// The files are in the library but nothing points at them. Leave them
		// for an admin rather than guess.
		slog.Error("imported files were not recorded and are left in place", "request", row.ID, "folder", res.Folder, "err", err)
		return skipChanged(err)
	}
	w.svc.publish(req)
	w.svc.emit(ctx, notify.Event{
		Type:      notify.EvItemAvailable,
		Message:   fmt.Sprintf("%s is ready to download.", req.DisplayTitle),
		RequestID: req.ID, ItemID: itemID, ToUserIDs: []int64{req.RequesterID},
		Payload: map[string]any{"requestId": req.ID, "itemId": itemID, "title": req.DisplayTitle},
	})
	w.svc.b.Publish(events.Message{Type: events.TypeLibraryUpdated, Data: map[string]any{"id": itemID}, All: true})
	return nil
}

// record creates the library item and marks the request available in one
// transaction, so an item never exists without its request pointing at it.
func (w *Worker) record(ctx context.Context, row dbq.Request, rel prowlarr.Release, res importer.Result) (Request, int64, error) {
	tx, err := w.svc.db.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, 0, err
	}
	defer tx.Rollback()
	q := w.q.WithTx(tx)
	itemID, err := library.CreateItem(ctx, q, row.ID, row.DisplayTitle, rel, res, w.now())
	if err != nil {
		return Request{}, 0, err
	}
	req, err := w.svc.apply(ctx, q, row.ID, ActImported, func(u *dbq.UpdateRequestParams) {
		u.LibraryItemID = sql.NullInt64{Int64: itemID, Valid: true}
		u.Progress = 1
		u.Error = ""
	})
	if err != nil {
		return Request{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return Request{}, 0, err
	}
	return req, itemID, nil
}

// fail applies a failing action with msg as the request's error and tells the
// requester and admins.
func (w *Worker) fail(ctx context.Context, id int64, a Action, msg string) error {
	if err := ctx.Err(); err != nil {
		return err // shutting down, not a real failure
	}
	req, err := w.svc.transition(ctx, id, a, func(u *dbq.UpdateRequestParams) { u.Error = msg })
	if err != nil {
		return skipChanged(err)
	}
	slog.Warn("request failed", "request", id, "action", a, "err", msg)
	w.svc.emit(ctx, notify.Event{
		Type:      notify.EvRequestFailed,
		Message:   fmt.Sprintf("The request for %s failed: %s", req.DisplayTitle, msg),
		RequestID: id, ToUserIDs: []int64{req.RequesterID}, ToAdmins: true,
		Payload: map[string]any{"requestId": id, "title": req.DisplayTitle, "error": msg},
	})
	return nil
}

// skipChanged drops the errors that mean someone else changed the request
// first, such as an admin abandoning it. The worker just moves on.
func skipChanged(err error) error {
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

var errClientGone = errors.New("The download client for this request was removed.")

// clientByID returns the client a request was grabbed with.
func (w *Worker) clientByID(ctx context.Context, id int64) (downloadclient.Client, error) {
	row, err := w.q.GetDownloadClient(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errClientGone
	}
	if err != nil {
		return nil, err
	}
	return w.client(downloadclient.ConfigFromRow(row))
}

func (w *Worker) client(cfg downloadclient.Config) (downloadclient.Client, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if e, ok := w.clients[cfg.ID]; ok && e.cfg.Type == cfg.Type && bytes.Equal(e.cfg.Settings, cfg.Settings) {
		return e.c, nil
	}
	c, err := w.newClient(cfg)
	if err != nil {
		return nil, err
	}
	w.clients[cfg.ID] = cachedClient{cfg: cfg, c: c}
	return c, nil
}

func releaseOf(row dbq.Request) (prowlarr.Release, error) {
	rel, err := prowlarr.UnmarshalStored([]byte(row.Release))
	if err != nil {
		return rel, fmt.Errorf("request %d release: %w", row.ID, err)
	}
	return rel, nil
}
