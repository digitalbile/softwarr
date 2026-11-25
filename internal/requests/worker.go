package requests

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
)

const (
	fetchTimeout   = 60 * time.Second
	recordTimeout  = 30 * time.Second
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

// Messages a requester sees when the real error has details only admins
// should see, such as paths or the download client's own words.
const (
	msgGrabFailed   = "The download couldn't be started. An admin has the details."
	msgImportFailed = "The download finished but couldn't be added to the library. An admin has the details."
)

// fail applies a failing action. msg is the request's error as the requester
// sees it; detail, if set, is the full error for admins. The requester is
// told msg, admins are told detail, and webhooks get msg.
func (w *Worker) fail(ctx context.Context, id int64, a Action, msg, detail string) error {
	if err := ctx.Err(); err != nil {
		return err // shutting down, not a real failure
	}
	req, err := w.svc.transition(ctx, id, a, func(u *dbq.UpdateRequestParams) {
		u.Error = msg
		u.ErrorDetail = sql.NullString{String: detail, Valid: detail != ""}
	})
	if err != nil {
		return skipChanged(err)
	}
	slog.Warn("request failed", "request", id, "action", a, "err", msg, "detail", detail)
	e := notify.Event{
		Type:      notify.EvRequestFailed,
		Message:   fmt.Sprintf("The request for %s failed: %s", req.DisplayTitle, msg),
		RequestID: id, ToUserIDs: []int64{req.RequesterID}, ToAdmins: true,
		Payload: map[string]any{"requestId": id, "title": req.DisplayTitle, "error": msg},
	}
	if detail != "" {
		e.AdminMessage = fmt.Sprintf("The request for %s failed: %s", req.DisplayTitle, detail)
	}
	w.svc.emit(ctx, e)
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
