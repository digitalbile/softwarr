package requests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/torrentfile"
)

// Grabbing: handing an approved request to a download client.

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
		return w.fail(ctx, row.ID, ActGrabFailed, "Only torrent downloads are supported.", "")
	}
	cfg, ok, err := downloadclient.PickClient(ctx, w.q, rel.Protocol)
	if err != nil {
		return err
	}
	if !ok {
		return w.fail(ctx, row.ID, ActGrabFailed, ErrNoClient.Error(), "")
	}
	c, err := w.client(cfg)
	if err != nil {
		return w.fail(ctx, row.ID, ActGrabFailed, msgGrabFailed, fmt.Sprintf("The download client %s couldn't be set up: %v", cfg.Name, err))
	}
	add, err := w.resolve(ctx, rel)
	if err != nil {
		return w.fail(ctx, row.ID, ActGrabFailed, msgGrabFailed, err.Error())
	}
	// Fetching can take a while; don't add a torrent for a request an admin
	// abandoned meanwhile.
	if cur, err := w.q.GetRequest(ctx, row.ID); err != nil || Status(cur.Status) != Approved {
		return skipChanged(err)
	}
	id, err := c.Add(ctx, add)
	if err != nil {
		return w.fail(ctx, row.ID, ActGrabFailed, msgGrabFailed, fmt.Sprintf("%s didn't accept the torrent: %v", cfg.Name, err))
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
// magnet behind DownloadURL, falling back to MagnetURL. Either may be a
// magnet link itself or an http(s) link to fetch; Prowlarr usually proxies
// the magnet link through itself.
func (w *Worker) resolve(ctx context.Context, rel prowlarr.Release) (downloadclient.AddRequest, error) {
	if rel.DownloadURL != "" {
		add, err := w.link(ctx, rel.DownloadURL)
		if err == nil || rel.MagnetURL == "" {
			return add, err
		}
		slog.Warn("using the magnet link after the download link failed", "title", rel.Title, "err", err)
	}
	if rel.MagnetURL != "" {
		return w.link(ctx, rel.MagnetURL)
	}
	return downloadclient.AddRequest{}, errors.New("This release has no download link.")
}

// link resolves one release link: a magnet is parsed, anything else fetched.
func (w *Worker) link(ctx context.Context, raw string) (downloadclient.AddRequest, error) {
	if isMagnet(raw) {
		return magnet(raw)
	}
	return w.fetch(ctx, raw)
}

// fetch downloads a torrent from an indexer. Error text never includes the
// URL's query string, which often carries an API key.
func (w *Worker) fetch(ctx context.Context, raw string) (downloadclient.AddRequest, error) {
	shown := redactURL(raw)
	if u, err := url.Parse(raw); err != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return downloadclient.AddRequest{}, fmt.Errorf("The download link %s isn't valid.", shown)
	}
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
