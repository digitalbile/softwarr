package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	_ "github.com/digitalbile/softwarr/internal/downloadclient/qbittorrent"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/releasetoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestBody struct {
	ID        int64          `json:"id"`
	Requester string         `json:"requester"`
	Status    string         `json:"status"`
	Comment   string         `json:"comment"`
	Release   map[string]any `json:"release"`
}

func seedClient(t *testing.T, ts *testServer) {
	t.Helper()
	_, err := ts.Q.CreateDownloadClient(t.Context(), dbq.CreateDownloadClientParams{Name: "qb", Type: "qbittorrent", Protocol: "torrent", Enabled: 1, Settings: "{}"})
	require.NoError(t, err)
}

func relFor(name string) prowlarr.Release {
	return prowlarr.Release{GUID: "g-" + name, Title: name, Protocol: "torrent", MagnetURL: "magnet:?xt=urn:btih:abc&tr=passkey-" + name}
}

// tokenAt seals rel for user uid as if a search had returned it at when.
func (ts *testServer) tokenAt(rel prowlarr.Release, uid int64, when time.Time) string {
	ts.t.Helper()
	key, err := ts.Deps.Settings.ReleaseSecret(ts.t.Context())
	require.NoError(ts.t, err)
	tok, err := releasetoken.Seal(key, rel, uid, when)
	require.NoError(ts.t, err)
	return tok
}

func (ts *testServer) token(rel prowlarr.Release, uid int64) string {
	return ts.tokenAt(rel, uid, ts.Clock.Now())
}

// relBody is the POST /requests body for a release found by user uid.
func (ts *testServer) relBody(name string, uid int64) map[string]any {
	return map[string]any{"token": ts.token(relFor(name), uid)}
}

func TestUserCannotSeeOthersRequests(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	o := owner.loginAsOwner()
	user := ts.newClient()
	u := user.loginAsUser()

	resp := owner.do("POST", "/api/v1/requests", map[string]any{"token": ts.token(relFor("a"), o.ID), "comment": "mine"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	ownerReq := decode[requestBody](t, resp)

	resp = user.do("POST", "/api/v1/requests", ts.relBody("b", u.ID))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	userReq := decode[requestBody](t, resp)
	assert.Equal(t, "pending", userReq.Status)
	assert.Equal(t, "user", userReq.Requester)

	resp = user.do("GET", "/api/v1/requests/"+strconv.FormatInt(ownerReq.ID, 10), nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = user.do("GET", "/api/v1/requests?all=1", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decode[[]requestBody](t, resp)
	require.Len(t, list, 1)
	assert.Equal(t, userReq.ID, list[0].ID)

	resp = owner.do("GET", "/api/v1/requests?all=1", nil)
	assert.Len(t, decode[[]requestBody](t, resp), 2)
	resp = owner.do("GET", "/api/v1/requests", nil)
	assert.Len(t, decode[[]requestBody](t, resp), 1)

	// Users cannot approve; others' cancel is a 404.
	path := "/api/v1/requests/" + strconv.FormatInt(userReq.ID, 10)
	assert.Equal(t, http.StatusForbidden, user.do("POST", path+"/approve", nil).StatusCode)
	assert.Equal(t, http.StatusNotFound, owner.do("POST", path+"/cancel", nil).StatusCode)
}

func TestRequestEndpointErrorMapping(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	owner.loginAsOwner()
	user := ts.newClient()
	u := user.loginAsUser()

	resp := user.do("POST", "/api/v1/requests", ts.relBody("a", u.ID))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	id := decode[requestBody](t, resp).ID

	resp = user.do("POST", "/api/v1/requests", ts.relBody("a", u.ID))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "This release has already been requested.", errorBody(t, resp))

	resp = user.do("POST", "/api/v1/requests", map[string]any{"token": ts.token(prowlarr.Release{Title: "x", Protocol: "torrent"}, u.ID)})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "This release has no download link.", errorBody(t, resp))

	path := "/api/v1/requests/" + strconv.FormatInt(id, 10)
	resp = owner.do("POST", path+"/approve", map[string]any{"displayTitle": "A"})
	assert.Equal(t, http.StatusConflict, resp.StatusCode) // no library root

	st, err := ts.Deps.Settings.Get(t.Context())
	require.NoError(t, err)
	st.LibraryRoot = "/library"
	require.NoError(t, ts.Deps.Settings.Save(t.Context(), st))

	resp = owner.do("POST", path+"/approve", map[string]any{"displayTitle": "A"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "approved", decode[requestBody](t, resp).Status)

	resp = owner.do("POST", path+"/approve", nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	assert.Equal(t, http.StatusNotFound, owner.do("GET", "/api/v1/requests/999", nil).StatusCode)
}

func TestCreateRequestRejectsBadTokens(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	o := owner.loginAsOwner()
	user := ts.newClient()
	u := user.loginAsUser()
	const invalid = "This search result isn't valid. Search again."

	// A client-written release is no longer accepted.
	resp := user.do("POST", "/api/v1/requests", map[string]any{"release": map[string]any{
		"title": "x", "protocol": "torrent", "downloadUrl": "http://127.0.0.1:22/",
	}})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = user.do("POST", "/api/v1/requests", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, invalid, errorBody(t, resp))

	tok := ts.token(relFor("a"), u.ID)
	tampered := []byte(tok)
	if tampered[10] == 'A' {
		tampered[10] = 'B'
	} else {
		tampered[10] = 'A'
	}
	resp = user.do("POST", "/api/v1/requests", map[string]any{"token": string(tampered)})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, invalid, errorBody(t, resp))

	resp = user.do("POST", "/api/v1/requests", map[string]any{"token": ts.token(relFor("a"), o.ID)})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "a token issued to someone else")
	assert.Equal(t, invalid, errorBody(t, resp))

	old := ts.tokenAt(relFor("a"), u.ID, ts.Clock.Now().Add(-25*time.Hour))
	resp = user.do("POST", "/api/v1/requests", map[string]any{"token": old})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "These search results are out of date. Search again.", errorBody(t, resp))

	var n int
	require.NoError(t, ts.DB.QueryRow("SELECT COUNT(*) FROM requests").Scan(&n))
	assert.Zero(t, n)
}

func TestRequestJSONHasNoDownloadLinks(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	o := owner.loginAsOwner()
	user := ts.newClient()
	u := user.loginAsUser()
	st, err := ts.Deps.Settings.Get(t.Context())
	require.NoError(t, err)
	st.LibraryRoot = "/library"
	require.NoError(t, ts.Deps.Settings.Save(t.Context(), st))

	resp := user.do("POST", "/api/v1/requests", ts.relBody("a", u.ID))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := readBody(t, resp)
	id := decodeString[requestBody](t, created).ID
	path := "/api/v1/requests/" + strconv.FormatInt(id, 10)

	// The worker still has the link: it is stored server side.
	var stored string
	require.NoError(t, ts.DB.QueryRow("SELECT release FROM requests WHERE id = ?", id).Scan(&stored))
	assert.Contains(t, stored, "passkey-a")

	repl := relFor("b")
	repl.DownloadURL = "http://prowlarr.local/2/download?apikey=secret"
	resp = owner.do("POST", path+"/approve", map[string]any{"replacementToken": ts.token(repl, o.ID)})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	approved := readBody(t, resp)

	bodies := []string{created, approved,
		readBody(t, user.do("GET", "/api/v1/requests", nil)),
		readBody(t, user.do("GET", path, nil)),
		readBody(t, owner.do("GET", "/api/v1/requests?all=1", nil)),
		readBody(t, owner.do("GET", path, nil)),
	}
	for _, b := range bodies {
		for _, leak := range []string{"downloadUrl", "magnetUrl", "magnet:", "passkey", "apikey"} {
			assert.NotContains(t, b, leak)
		}
	}
	assert.Contains(t, approved, `"originalRelease":{`)
}

func TestApproveReplacementTokenChecks(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	o := owner.loginAsOwner()
	user := ts.newClient()
	u := user.loginAsUser()
	st, err := ts.Deps.Settings.Get(t.Context())
	require.NoError(t, err)
	st.LibraryRoot = "/library"
	require.NoError(t, ts.Deps.Settings.Save(t.Context(), st))

	resp := user.do("POST", "/api/v1/requests", ts.relBody("a", u.ID))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	path := "/api/v1/requests/" + strconv.FormatInt(decode[requestBody](t, resp).ID, 10)

	// The requester's own token can't be used by the approving admin.
	resp = owner.do("POST", path+"/approve", map[string]any{"replacementToken": ts.token(relFor("b"), u.ID)})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "This search result isn't valid. Search again.", errorBody(t, resp))

	resp = owner.do("POST", path+"/approve", map[string]any{"replacementToken": ts.tokenAt(relFor("b"), o.ID, ts.Clock.Now().Add(-25*time.Hour))})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "These search results are out of date. Search again.", errorBody(t, resp))

	resp = owner.do("POST", path+"/approve", map[string]any{"replacement": map[string]any{"title": "b", "protocol": "torrent"}})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = owner.do("POST", path+"/approve", map[string]any{"displayTitle": "B", "replacementToken": ts.token(relFor("b"), o.ID)})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decode[requestBody](t, resp)
	assert.Equal(t, "approved", got.Status)
	assert.Equal(t, "b", got.Release["title"])
	var stored string
	require.NoError(t, ts.DB.QueryRow("SELECT release FROM requests WHERE id = ?", got.ID).Scan(&stored))
	assert.True(t, strings.Contains(stored, "passkey-b"))
}

func TestApproveFailedRequestWithReplacement(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	o := owner.loginAsOwner()
	st, err := ts.Deps.Settings.Get(t.Context())
	require.NoError(t, err)
	st.LibraryRoot = "/library"
	require.NoError(t, ts.Deps.Settings.Save(t.Context(), st))

	resp := owner.do("POST", "/api/v1/requests", ts.relBody("a", o.ID))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	id := decode[requestBody](t, resp).ID
	_, err = ts.DB.Exec("UPDATE requests SET status = 'failed', error = 'stale link', client_item_id = 'old' WHERE id = ?", id)
	require.NoError(t, err)
	path := "/api/v1/requests/" + strconv.FormatInt(id, 10)

	resp = owner.do("POST", path+"/approve", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Choose a different release, or retry this request.", errorBody(t, resp))

	resp = owner.do("POST", path+"/approve", map[string]any{"replacementToken": ts.token(relFor("b"), o.ID)})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decode[requestBody](t, resp)
	assert.Equal(t, "approved", got.Status)
	assert.Equal(t, "b", got.Release["title"])
}

func TestRequestErrorDetailIsForAdminsOnly(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	uc := ts.newClient()
	user := uc.loginAsUser()
	r, err := ts.DB.Exec(`INSERT INTO requests (requester_id, status, release, release_key, display_title, error, error_detail, created_at, updated_at)
		VALUES (?, 'failed', '{"title":"Thing","protocol":"torrent"}', 'k', 'Thing', 'The download finished but couldn''t be added to the library. An admin has the details.', 'Importing from /downloads/Thing failed: permission denied', 1, 1)`, user.ID)
	require.NoError(t, err)
	id, _ := r.LastInsertId()

	for _, path := range []string{"/api/v1/requests", "/api/v1/requests?all=1", "/api/v1/requests/" + itoa(id)} {
		resp := uc.do("GET", path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "An admin has the details.", path)
		assert.NotContains(t, string(body), "/downloads", path)
		assert.NotContains(t, string(body), "errorDetail", path)
	}

	got := decode[map[string]any](t, ts.do("GET", "/api/v1/requests/"+itoa(id), nil))
	assert.Equal(t, "Importing from /downloads/Thing failed: permission denied", got["errorDetail"])
	act := decode[[]map[string]any](t, ts.do("GET", "/api/v1/activity", nil))
	require.Len(t, act, 1)
	assert.Equal(t, "Importing from /downloads/Thing failed: permission denied", act[0]["errorDetail"])
}
