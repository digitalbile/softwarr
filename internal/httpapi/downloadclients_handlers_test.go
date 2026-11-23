package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	_ "github.com/digitalbile/softwarr/internal/downloadclient/qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type clientBody struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Protocol string         `json:"protocol"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Settings map[string]any `json:"settings"`
}

func validClient() map[string]any {
	return map[string]any{
		"name": "qbt", "type": "qbittorrent", "enabled": true, "priority": 1,
		"settings": map[string]any{"host": "qbt", "port": 8080, "username": "admin", "password": "hunter2"},
	}
}

func TestDownloadClientPasswordMasked(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()

	resp := ts.do("POST", "/api/v1/downloadclients", validClient())
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decode[clientBody](t, resp)
	assert.Equal(t, "torrent", created.Protocol)
	assert.Equal(t, "********", created.Settings["password"])
	assert.Equal(t, "qbt", created.Settings["host"])

	// Stored in plaintext.
	row, err := ts.Q.GetDownloadClient(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Contains(t, row.Settings, "hunter2")

	// List masks too.
	list := decode[[]clientBody](t, ts.do("GET", "/api/v1/downloadclients", nil))
	require.Len(t, list, 1)
	assert.Equal(t, "********", list[0].Settings["password"])

	// PUT carrying the mask keeps the stored password.
	body := validClient()
	body["name"] = "renamed"
	body["settings"].(map[string]any)["password"] = "********"
	resp = ts.do("PUT", "/api/v1/downloadclients/"+strconv.FormatInt(created.ID, 10), body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "renamed", decode[clientBody](t, resp).Name)
	row, err = ts.Q.GetDownloadClient(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Contains(t, row.Settings, "hunter2")

	// PUT with a new password replaces it.
	body["settings"].(map[string]any)["password"] = "newpass"
	resp = ts.do("PUT", "/api/v1/downloadclients/"+strconv.FormatInt(created.ID, 10), body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	row, _ = ts.Q.GetDownloadClient(t.Context(), created.ID)
	assert.Contains(t, row.Settings, "newpass")
}

func TestDownloadClientValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	cases := map[string]func(m map[string]any){
		"name":     func(m map[string]any) { m["name"] = " " },
		"type":     func(m map[string]any) { m["type"] = "nope" },
		"priority": func(m map[string]any) { m["priority"] = -1 },
		"host":     func(m map[string]any) { m["settings"] = map[string]any{"port": 8080} },
		"port":     func(m map[string]any) { m["settings"] = map[string]any{"host": "h", "port": 0} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := validClient()
			mutate(b)
			resp := ts.do("POST", "/api/v1/downloadclients", b)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			assert.NotEmpty(t, errorBody(t, resp))
		})
	}
}

func TestDownloadClientCRUDTypesAndAuth(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner() // creates the owner so the regular user can be added
	u := ts.newClient()
	u.loginAsUser()
	assert.Equal(t, http.StatusForbidden, u.do("GET", "/api/v1/downloadclients", nil).StatusCode)
	assert.Equal(t, http.StatusForbidden, u.do("GET", "/api/v1/downloadclients/types", nil).StatusCode)

	types := decode[[]map[string]string](t, ts.do("GET", "/api/v1/downloadclients/types", nil))
	require.NotEmpty(t, types)
	assert.Equal(t, map[string]string{"type": "qbittorrent", "protocol": "torrent", "name": "qBittorrent"}, types[0])

	created := decode[clientBody](t, ts.do("POST", "/api/v1/downloadclients", validClient()))
	path := "/api/v1/downloadclients/" + strconv.FormatInt(created.ID, 10)
	assert.Equal(t, http.StatusNotFound, ts.do("PUT", "/api/v1/downloadclients/999", validClient()).StatusCode)
	assert.Equal(t, http.StatusNoContent, ts.do("DELETE", path, nil).StatusCode)
	assert.Equal(t, http.StatusNotFound, ts.do("DELETE", path, nil).StatusCode)
}

func TestDownloadClientTestEndpoint(t *testing.T) {
	qbt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_ = r.ParseForm()
			if r.PostForm.Get("password") != "hunter2" {
				_, _ = w.Write([]byte("Fails."))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "x", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("v5"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(qbt.Close)
	u, _ := url.Parse(qbt.URL)
	port, _ := strconv.Atoi(u.Port())

	ts := newTestServer(t)
	ts.loginAsOwner()
	body := func(pw string) map[string]any {
		return map[string]any{"name": "q", "type": "qbittorrent", "settings": map[string]any{"host": u.Hostname(), "port": port, "username": "a", "password": pw}}
	}
	type result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	r := decode[result](t, ts.do("POST", "/api/v1/downloadclients/test", body("hunter2")))
	assert.True(t, r.OK)
	r = decode[result](t, ts.do("POST", "/api/v1/downloadclients/test", body("bad")))
	assert.False(t, r.OK)
	assert.NotEmpty(t, r.Error)

	// A masked password resolves against the saved client when an id is given.
	b := body("hunter2")
	created := decode[clientBody](t, ts.do("POST", "/api/v1/downloadclients", b))
	b = body("********")
	b["id"] = created.ID
	assert.True(t, decode[result](t, ts.do("POST", "/api/v1/downloadclients/test", b)).OK)
}
