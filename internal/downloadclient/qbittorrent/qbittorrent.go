// Package qbittorrent is a download client for the qBittorrent Web API v2.
// It only logs in, reads the version, adds torrents, and reads torrent info
// and files. It never pauses, removes, rechecks, or recategorizes anything.
package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/torrentfile"
)

const (
	typeName        = "qbittorrent"
	defaultCategory = "softwarr"
	tag             = "softwarr"
)

func init() {
	downloadclient.RegisterNamed(typeName, "qBittorrent", "torrent", func(cfg downloadclient.Config, httpc *http.Client) (downloadclient.Client, error) {
		return newClient(cfg, httpc)
	})
}

type settings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	UseTLS   bool   `json:"useTls"`
	URLBase  string `json:"urlBase"`
	Username string `json:"username"`
	Password string `json:"password"`
	Category string `json:"category"`
}

// baseURL builds the API root. Host may be a bare name or carry a scheme (and
// optionally a port); the settings port applies when the host has none.
func (s settings) baseURL() (*url.URL, error) {
	host := strings.TrimSpace(s.Host)
	scheme := "http"
	if s.UseTLS {
		scheme = "https"
	}
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, downloadclient.Invalid("Enter a valid host.")
		}
		scheme = u.Scheme
		host = u.Host
		if u.Port() == "" {
			host = net.JoinHostPort(u.Hostname(), strconv.Itoa(s.Port))
		}
	} else {
		host = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(s.Port))
	}
	u := &url.URL{Scheme: scheme, Host: host}
	var parts []string
	for _, p := range strings.Split(s.URLBase, "/") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) > 0 {
		u.Path = "/" + strings.Join(parts, "/")
	}
	return u, nil
}

type client struct {
	s     settings
	base  *url.URL
	http  *http.Client
	cat   string
	mu    sync.Mutex // guards login state
	login bool
}

func newClient(cfg downloadclient.Config, httpc *http.Client) (*client, error) {
	var s settings
	if err := json.Unmarshal(cfg.Settings, &s); err != nil {
		return nil, downloadclient.Invalid("The qBittorrent settings aren't valid.")
	}
	if strings.TrimSpace(s.Host) == "" {
		return nil, downloadclient.Invalid("Enter the qBittorrent host.")
	}
	if s.Port < 1 || s.Port > 65535 {
		return nil, downloadclient.Invalid("Enter a port between 1 and 65535.")
	}
	base, err := s.baseURL()
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	// Own copy so the SID cookie jar is not shared with other clients.
	hc := &http.Client{Transport: httpc.Transport, Timeout: httpc.Timeout, Jar: jar, CheckRedirect: httpc.CheckRedirect}
	cat := strings.TrimSpace(s.Category)
	if cat == "" {
		cat = defaultCategory
	}
	return &client{s: s, base: base, http: hc, cat: cat}, nil
}

func (c *client) Protocol() string { return "torrent" }
func (c *client) Host() string     { return c.base.Hostname() }

func (c *client) endpoint(path string) string { return c.base.String() + "/api/v2/" + path }

func (c *client) setHeaders(req *http.Request) {
	// qBittorrent's CSRF protection wants Referer/Origin to match the host.
	req.Header.Set("Referer", c.base.String())
	req.Header.Set("Origin", c.base.Scheme+"://"+c.base.Host)
}

func (c *client) doLogin(ctx context.Context) error {
	form := url.Values{"username": {c.s.Username}, "password": {c.s.Password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("auth/login"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return errors.New("qBittorrent refused the login. Too many failed attempts can ban an address for a while.")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("qBittorrent login failed with status %d", resp.StatusCode)
	case strings.TrimSpace(string(body)) != "Ok.":
		return errors.New("qBittorrent rejected the username or password.")
	}
	return nil
}

// ensureLogin logs in once; later calls reuse the session. stale is true when
// the caller just saw a 403 and wants a fresh login.
func (c *client) ensureLogin(ctx context.Context, stale bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.login && !stale {
		return nil
	}
	c.login = false
	if err := c.doLogin(ctx); err != nil {
		return err
	}
	c.login = true
	return nil
}

// do sends a request, logging in first and again once on a 403. build is
// called per attempt because multipart bodies cannot be replayed.
func (c *client) do(ctx context.Context, method, path string, build func() (io.Reader, string)) (int, []byte, error) {
	if err := c.ensureLogin(ctx, false); err != nil {
		return 0, nil, err
	}
	for attempt := 0; ; attempt++ {
		var body io.Reader
		var ctype string
		if build != nil {
			body, ctype = build()
		}
		req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
		if err != nil {
			return 0, nil, err
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		c.setHeaders(req)
		resp, err := c.http.Do(req)
		if err != nil {
			return 0, nil, err
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if rerr != nil {
			return 0, nil, rerr
		}
		if resp.StatusCode == http.StatusForbidden {
			if attempt == 0 {
				if err := c.ensureLogin(ctx, true); err != nil {
					return 0, nil, err
				}
				continue
			}
			return resp.StatusCode, data, errors.New("qBittorrent denied access even after logging in again.")
		}
		return resp.StatusCode, data, nil
	}
}

func (c *client) Test(ctx context.Context) error {
	status, body, err := c.do(ctx, http.MethodGet, "app/version", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("qBittorrent answered with status %d", status)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("qBittorrent did not return a version.")
	}
	return nil
}

func (c *client) Add(ctx context.Context, req downloadclient.AddRequest) (string, error) {
	id := strings.ToLower(strings.TrimSpace(req.InfoHash))
	if len(req.TorrentFile) == 0 && strings.TrimSpace(req.MagnetURI) == "" {
		return "", errors.New("nothing to add: no torrent file or magnet link")
	}
	if id == "" {
		var err error
		if len(req.TorrentFile) > 0 {
			id, err = torrentfile.InfoHash(req.TorrentFile)
		} else {
			id, err = torrentfile.ParseMagnet(req.MagnetURI)
		}
		if err != nil {
			return "", fmt.Errorf("work out the infohash: %w", err)
		}
	}
	build := func() (io.Reader, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if len(req.TorrentFile) > 0 {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", `form-data; name="torrents"; filename="softwarr.torrent"`)
			h.Set("Content-Type", "application/x-bittorrent")
			part, _ := mw.CreatePart(h)
			_, _ = part.Write(req.TorrentFile)
		} else {
			_ = mw.WriteField("urls", req.MagnetURI)
		}
		_ = mw.WriteField("category", c.cat)
		_ = mw.WriteField("tags", tag)
		_ = mw.Close()
		return &buf, mw.FormDataContentType()
	}
	status, body, err := c.do(ctx, http.MethodPost, "torrents/add", build)
	if err != nil {
		return "", err
	}
	if status == http.StatusUnsupportedMediaType {
		return "", errors.New("qBittorrent says the torrent file is invalid")
	}
	if status != http.StatusOK {
		// qBittorrent 5 answers a duplicate with 409; attach if it's there.
		if c.has(ctx, id) {
			return id, nil
		}
		return "", fmt.Errorf("qBittorrent answered with status %d when adding the torrent", status)
	}
	if addFailed(body) {
		// Usually a duplicate: attach to the existing torrent if it is there.
		if c.has(ctx, id) {
			return id, nil
		}
		return "", errors.New("qBittorrent did not accept the torrent")
	}
	return id, nil
}

// has reports whether qBittorrent already has the torrent with this hash.
func (c *client) has(ctx context.Context, id string) bool {
	it, err := c.Get(ctx, id)
	return err == nil && it.State != downloadclient.StateMissing
}

// addFailed reads the add response: "Ok." or "Fails." on older versions, and a
// JSON summary on newer ones.
func addFailed(body []byte) bool {
	b := bytes.TrimSpace(body)
	if len(b) > 0 && b[0] == '{' {
		var r struct {
			Success int `json:"success_count"`
			Failure int `json:"failure_count"`
			Pending int `json:"pending_count"`
		}
		if json.Unmarshal(b, &r) == nil {
			return r.Failure > 0 && r.Success == 0 && r.Pending == 0
		}
	}
	return string(b) == "Fails."
}

type info struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	State    string  `json:"state"`
	SavePath string  `json:"save_path"`
}

var completeStates = map[string]bool{
	"uploading": true, "stalledUP": true, "pausedUP": true, "stoppedUP": true,
	"queuedUP": true, "forcedUP": true, "checkingUP": true,
}

func (c *client) Get(ctx context.Context, id string) (downloadclient.Item, error) {
	id = strings.ToLower(id)
	status, body, err := c.do(ctx, http.MethodGet, "torrents/info?hashes="+url.QueryEscape(id), nil)
	if err != nil {
		return downloadclient.Item{}, err
	}
	if status != http.StatusOK {
		return downloadclient.Item{}, fmt.Errorf("qBittorrent answered with status %d reading the torrent", status)
	}
	var infos []info
	if err := json.Unmarshal(body, &infos); err != nil {
		return downloadclient.Item{}, fmt.Errorf("read torrent info: %w", err)
	}
	if len(infos) == 0 {
		return downloadclient.Item{ID: id, State: downloadclient.StateMissing}, nil
	}
	in := infos[0]
	item := downloadclient.Item{ID: id, Name: in.Name, SavePath: in.SavePath, Progress: in.Progress}
	item.State, item.Message = mapState(in.State, in.Progress)

	status, body, err = c.do(ctx, http.MethodGet, "torrents/files?hash="+url.QueryEscape(id), nil)
	if err != nil {
		return downloadclient.Item{}, err
	}
	switch status {
	case http.StatusOK:
		var files []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
		if err := json.Unmarshal(body, &files); err != nil {
			return downloadclient.Item{}, fmt.Errorf("read torrent files: %w", err)
		}
		for _, f := range files {
			item.Files = append(item.Files, downloadclient.File{Path: strings.ReplaceAll(f.Name, "\\", "/"), Size: f.Size})
		}
	case http.StatusNotFound:
		return downloadclient.Item{ID: id, State: downloadclient.StateMissing}, nil
	default:
		return downloadclient.Item{}, fmt.Errorf("qBittorrent answered with status %d reading the files", status)
	}
	return item, nil
}

func mapState(state string, progress float64) (downloadclient.State, string) {
	switch {
	case state == "error" || state == "missingFiles":
		return downloadclient.StateError, "qBittorrent reports the state " + state
	case progress >= 1 && completeStates[state]:
		return downloadclient.StateCompleted, ""
	case state == "queuedDL":
		return downloadclient.StateQueued, ""
	default:
		return downloadclient.StateDownloading, ""
	}
}
