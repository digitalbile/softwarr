package qbittorrent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/torrentfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTorrent struct {
	Hash, Name, State, SavePath, Category, Tags string
	Progress                                    float64
	Files                                       []map[string]any
}

// fakeQbit is a qBittorrent stand-in. It fails the test on any endpoint that
// softwarr is not allowed to call.
type fakeQbit struct {
	t   *testing.T
	srv *httptest.Server

	mu           sync.Mutex
	sids         map[string]bool
	logins       int
	torrents     map[string]*fakeTorrent
	badLogin     bool
	badSID       bool // logins succeed but the issued SID is never valid
	paths        []string
	nextSID      int
	lastAdd      map[string]string
	lastFile     []byte
	addJSON      bool
	forceAddFail bool
	dupStatus    int // when set, a duplicate add answers with this status
	failStatus   int // when set, every add answers with this status
	infoCalls    int
}

func newFakeQbit(t *testing.T) *fakeQbit {
	f := &fakeQbit{t: t, sids: map[string]bool{}, torrents: map[string]*fakeTorrent{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeQbit) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	switch r.URL.Path {
	case "/api/v2/auth/login":
		if r.Method != http.MethodPost {
			f.t.Errorf("login must be POST")
		}
		host := r.Host
		if !strings.Contains(r.Header.Get("Referer"), host) || !strings.Contains(r.Header.Get("Origin"), host) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = r.ParseForm()
		if f.badLogin || r.PostForm.Get("username") != "admin" || r.PostForm.Get("password") != "secret" {
			_, _ = w.Write([]byte("Fails."))
			return
		}
		f.logins++
		f.nextSID++
		sid := "sid" + strconv.Itoa(f.nextSID)
		if !f.badSID {
			f.sids[sid] = true
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: sid, Path: "/"})
		_, _ = w.Write([]byte("Ok."))
		return
	case "/api/v2/app/version", "/api/v2/torrents/add", "/api/v2/torrents/info", "/api/v2/torrents/files":
	default:
		f.t.Errorf("forbidden qBittorrent endpoint called: %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	c, err := r.Cookie("SID")
	if err != nil || !f.sids[c.Value] {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/api/v2/app/version":
		_, _ = w.Write([]byte("v5.0.0"))
	case "/api/v2/torrents/add":
		f.add(w, r)
	case "/api/v2/torrents/info":
		f.infoCalls++
		out := []map[string]any{}
		for _, h := range strings.Split(r.URL.Query().Get("hashes"), "|") {
			if tr, ok := f.torrents[h]; ok {
				out = append(out, map[string]any{"hash": tr.Hash, "name": tr.Name, "progress": tr.Progress, "state": tr.State, "save_path": tr.SavePath})
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	case "/api/v2/torrents/files":
		tr, ok := f.torrents[r.URL.Query().Get("hash")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(tr.Files)
	}
}

func (f *fakeQbit) add(w http.ResponseWriter, r *http.Request) {
	if f.forceAddFail {
		_, _ = w.Write([]byte("Fails."))
		return
	}
	if f.failStatus != 0 {
		w.WriteHeader(f.failStatus)
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		f.t.Errorf("parse multipart: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.lastAdd = map[string]string{}
	for k, v := range r.MultipartForm.Value {
		f.lastAdd[k] = v[0]
	}
	var hash string
	if fh := r.MultipartForm.File["torrents"]; len(fh) > 0 {
		file, err := fh[0].Open()
		if err != nil {
			f.t.Errorf("open upload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		buf := make([]byte, fh[0].Size)
		_, _ = file.Read(buf)
		f.lastFile = buf
		hash, err = torrentfile.InfoHash(buf)
		if err != nil {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
	} else {
		var err error
		hash, err = torrentfile.ParseMagnet(f.lastAdd["urls"])
		if err != nil {
			f.t.Errorf("bad magnet: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if _, dup := f.torrents[hash]; dup {
		if f.dupStatus != 0 {
			w.WriteHeader(f.dupStatus)
			_, _ = w.Write([]byte("Torrent is already present"))
			return
		}
		_, _ = w.Write([]byte("Fails."))
		return
	}
	f.torrents[hash] = &fakeTorrent{Hash: hash, Name: "added", State: "downloading", SavePath: "/downloads", Category: f.lastAdd["category"], Tags: f.lastAdd["tags"]}
	if f.addJSON {
		_, _ = w.Write([]byte(`{"success_count":1,"failure_count":0,"pending_count":0,"added_torrent_ids":["` + hash + `"]}`))
		return
	}
	_, _ = w.Write([]byte("Ok."))
}

func (f *fakeQbit) config(t *testing.T, extra map[string]any) downloadclient.Config {
	u, err := url.Parse(f.srv.URL)
	require.NoError(t, err)
	port, _ := strconv.Atoi(u.Port())
	s := map[string]any{"host": u.Hostname(), "port": port, "username": "admin", "password": "secret"}
	for k, v := range extra {
		s[k] = v
	}
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	return downloadclient.Config{ID: 1, Name: "qbt", Type: "qbittorrent", Enabled: true, Settings: raw}
}

func (f *fakeQbit) client(t *testing.T, extra map[string]any) downloadclient.Client {
	c, err := downloadclient.New(f.config(t, extra), f.srv.Client())
	require.NoError(t, err)
	return c
}

func sampleTorrent(t *testing.T) ([]byte, string) {
	b, err := os.ReadFile("../../torrentfile/testdata/sample.torrent")
	require.NoError(t, err)
	h, err := torrentfile.InfoHash(b)
	require.NoError(t, err)
	return b, h
}

func TestQbitAddUploadsFileWithCategoryAndTag(t *testing.T) {
	f := newFakeQbit(t)
	c := f.client(t, nil)
	b, hash := sampleTorrent(t)
	id, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	assert.Equal(t, hash, id)
	assert.Equal(t, "softwarr", f.lastAdd["category"])
	assert.Equal(t, "softwarr", f.lastAdd["tags"])
	assert.Equal(t, b, f.lastFile)
	assert.Equal(t, "softwarr", f.torrents[hash].Category)
}

func TestQbitAddCustomCategoryAndJSONResponse(t *testing.T) {
	f := newFakeQbit(t)
	f.addJSON = true
	c := f.client(t, map[string]any{"category": "apps"})
	b, hash := sampleTorrent(t)
	id, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b, InfoHash: strings.ToUpper(hash)})
	require.NoError(t, err)
	assert.Equal(t, hash, id)
	assert.Equal(t, "apps", f.lastAdd["category"])
}

func TestQbitAddMagnet(t *testing.T) {
	f := newFakeQbit(t)
	c := f.client(t, nil)
	hash := strings.Repeat("ab", 20)
	magnet := "magnet:?xt=urn:btih:" + hash + "&dn=x"
	id, err := c.Add(context.Background(), downloadclient.AddRequest{MagnetURI: magnet, InfoHash: hash})
	require.NoError(t, err)
	assert.Equal(t, hash, id)
	assert.Equal(t, magnet, f.lastAdd["urls"])
	assert.Equal(t, "softwarr", f.lastAdd["tags"])
	// hash derived from the magnet when not provided
	f2 := newFakeQbit(t)
	id, err = f2.client(t, nil).Add(context.Background(), downloadclient.AddRequest{MagnetURI: magnet})
	require.NoError(t, err)
	assert.Equal(t, hash, id)
}

func TestQbitAddRequiresPayload(t *testing.T) {
	f := newFakeQbit(t)
	_, err := f.client(t, nil).Add(context.Background(), downloadclient.AddRequest{})
	require.Error(t, err)
}

func TestQbitAddInvalidTorrent(t *testing.T) {
	f := newFakeQbit(t)
	_, err := f.client(t, nil).Add(context.Background(), downloadclient.AddRequest{TorrentFile: []byte("junk"), InfoHash: strings.Repeat("cd", 20)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestQbitAddDuplicateReattaches(t *testing.T) {
	f := newFakeQbit(t)
	c := f.client(t, nil)
	b, hash := sampleTorrent(t)
	id1, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	id2, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	assert.Equal(t, hash, id1)
	assert.Equal(t, id1, id2)
}

// qBittorrent 5 answers a duplicate add with 409 Conflict rather than "Fails.".
func TestQbitAddDuplicateConflictReattaches(t *testing.T) {
	f := newFakeQbit(t)
	f.dupStatus = http.StatusConflict
	c := f.client(t, nil)
	b, hash := sampleTorrent(t)
	_, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	id, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	assert.Equal(t, hash, id)
}

func TestQbitAddErrorStatusAndNotPresentIsError(t *testing.T) {
	f := newFakeQbit(t)
	f.failStatus = http.StatusConflict
	c := f.client(t, nil)
	_, err := c.Add(context.Background(), downloadclient.AddRequest{MagnetURI: "magnet:?xt=urn:btih:" + strings.Repeat("ef", 20)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 409")
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, 1, f.infoCalls, "Add should check whether the torrent is already there")
}

func TestQbitAddFailsAndNotPresentIsError(t *testing.T) {
	f := newFakeQbit(t)
	f.forceAddFail = true
	c := f.client(t, nil)
	hash := strings.Repeat("ef", 20)
	_, err := c.Add(context.Background(), downloadclient.AddRequest{MagnetURI: "magnet:?xt=urn:btih:" + hash})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not accept the torrent")
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, 1, f.infoCalls, "Add should check whether the torrent is already there")
	assert.Empty(t, f.torrents)
}

func TestQbitGetCompletedStates(t *testing.T) {
	cases := []struct {
		state    string
		progress float64
		want     downloadclient.State
	}{
		{"uploading", 1, downloadclient.StateCompleted},
		{"stalledUP", 1, downloadclient.StateCompleted},
		{"pausedUP", 1, downloadclient.StateCompleted},
		{"stoppedUP", 1, downloadclient.StateCompleted},
		{"queuedUP", 1, downloadclient.StateCompleted},
		{"forcedUP", 1, downloadclient.StateCompleted},
		{"checkingUP", 1, downloadclient.StateCompleted},
		{"downloading", 0.4, downloadclient.StateDownloading},
		{"uploading", 0.99, downloadclient.StateDownloading},
		{"queuedDL", 0, downloadclient.StateQueued},
		{"error", 0.2, downloadclient.StateError},
		{"missingFiles", 1, downloadclient.StateError},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			f := newFakeQbit(t)
			hash := strings.Repeat("12", 20)
			f.torrents[hash] = &fakeTorrent{Hash: hash, Name: "Root", State: tc.state, Progress: tc.progress, SavePath: "/downloads/softwarr",
				Files: []map[string]any{{"name": "Root/a.bin", "size": 10, "progress": 1}, {"name": "Root/sub/b.txt", "size": 5, "progress": 1}}}
			it, err := f.client(t, nil).Get(context.Background(), hash)
			require.NoError(t, err)
			assert.Equal(t, tc.want, it.State)
			assert.Equal(t, hash, it.ID)
			assert.Equal(t, "Root", it.Name)
			assert.Equal(t, "/downloads/softwarr", it.SavePath)
			assert.InDelta(t, tc.progress, it.Progress, 0.0001)
			require.Len(t, it.Files, 2)
			assert.Equal(t, downloadclient.File{Path: "Root/sub/b.txt", Size: 5}, it.Files[1])
			if tc.want == downloadclient.StateError {
				assert.NotEmpty(t, it.Message)
			}
		})
	}
}

func TestQbitGetMissing(t *testing.T) {
	f := newFakeQbit(t)
	it, err := f.client(t, nil).Get(context.Background(), strings.Repeat("99", 20))
	require.NoError(t, err)
	assert.Equal(t, downloadclient.StateMissing, it.State)
}

func TestQbitRelogsOnForbidden(t *testing.T) {
	f := newFakeQbit(t)
	c := f.client(t, nil)
	require.NoError(t, c.Test(context.Background()))
	assert.Equal(t, 1, f.logins)
	f.mu.Lock()
	f.sids = map[string]bool{} // SID expires
	f.mu.Unlock()
	require.NoError(t, c.Test(context.Background()))
	assert.Equal(t, 2, f.logins)

	// A second consecutive 403 is an error and logs in only once more.
	f.mu.Lock()
	f.badSID = true
	f.sids = map[string]bool{}
	f.mu.Unlock()
	err := c.Test(context.Background())
	require.Error(t, err)
	assert.Equal(t, 3, f.logins)
}

func TestQbitRelogsForMultipartAdd(t *testing.T) {
	f := newFakeQbit(t)
	c := f.client(t, nil)
	require.NoError(t, c.Test(context.Background()))
	f.mu.Lock()
	f.sids = map[string]bool{}
	f.mu.Unlock()
	b, hash := sampleTorrent(t)
	id, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	assert.Equal(t, hash, id)
	assert.Equal(t, b, f.lastFile)
}

func TestQbitBadCredentials(t *testing.T) {
	f := newFakeQbit(t)
	c := f.client(t, map[string]any{"password": "wrong"})
	err := c.Test(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username or password")
}

func TestQbitOnlyUsesAllowedEndpoints(t *testing.T) {
	f := newFakeQbit(t)
	c := f.client(t, nil)
	b, hash := sampleTorrent(t)
	_, err := c.Add(context.Background(), downloadclient.AddRequest{TorrentFile: b})
	require.NoError(t, err)
	_, err = c.Get(context.Background(), hash)
	require.NoError(t, err)
	require.NoError(t, c.Test(context.Background()))
	// The fake fails the test on any other path; also assert the set.
	for _, p := range f.paths {
		assert.Contains(t, []string{
			"POST /api/v2/auth/login", "GET /api/v2/app/version", "POST /api/v2/torrents/add",
			"GET /api/v2/torrents/info", "GET /api/v2/torrents/files",
		}, p)
	}
}

func TestQbitSettingsValidation(t *testing.T) {
	cases := map[string]string{
		`{}`:                        "host",
		`{"host":"h"}`:              "port",
		`{"host":"h","port":70000}`: "port",
		`{"host":"h","port":0}`:     "port",
		`not json`:                  "settings",
	}
	for raw, want := range cases {
		_, err := downloadclient.New(downloadclient.Config{Type: "qbittorrent", Settings: json.RawMessage(raw)}, nil)
		require.Error(t, err, raw)
		assert.Contains(t, strings.ToLower(err.Error()), want, raw)
		assert.True(t, downloadclient.IsInputError(err), raw)
	}
}

func TestBaseURL(t *testing.T) {
	cases := []struct {
		s    settings
		want string
		host string
	}{
		{settings{Host: "qbt", Port: 8080}, "http://qbt:8080", "qbt"},
		{settings{Host: "qbt", Port: 443, UseTLS: true, URLBase: "qb"}, "https://qbt:443/qb", "qbt"},
		{settings{Host: "qbt", Port: 8080, URLBase: "/qb/"}, "http://qbt:8080/qb", "qbt"},
		{settings{Host: "https://qbt.example.com", Port: 8443, URLBase: "//x//"}, "https://qbt.example.com:8443/x", "qbt.example.com"},
		{settings{Host: "http://qbt:9090/", Port: 8080}, "http://qbt:9090", "qbt"},
		{settings{Host: " ::1 ", Port: 8080}, "http://[::1]:8080", "::1"},
	}
	for _, tc := range cases {
		u, err := tc.s.baseURL()
		require.NoError(t, err, tc.s.Host)
		assert.Equal(t, tc.want, u.String())
		assert.Equal(t, tc.host, u.Hostname())
	}
}
