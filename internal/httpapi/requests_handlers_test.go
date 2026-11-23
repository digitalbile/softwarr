package httpapi

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	_ "github.com/digitalbile/softwarr/internal/downloadclient/qbittorrent"
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

func relBody(name string) map[string]any {
	return map[string]any{"guid": "g-" + name, "title": name, "protocol": "torrent", "magnetUrl": "magnet:?xt=urn:btih:abc"}
}

func TestUserCannotSeeOthersRequests(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	owner.loginAsOwner()
	user := ts.newClient()
	user.loginAsUser()

	resp := owner.do("POST", "/api/v1/requests", map[string]any{"release": relBody("a"), "comment": "mine"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	ownerReq := decode[requestBody](t, resp)

	resp = user.do("POST", "/api/v1/requests", map[string]any{"release": relBody("b")})
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
	user.loginAsUser()

	resp := user.do("POST", "/api/v1/requests", map[string]any{"release": relBody("a")})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	id := decode[requestBody](t, resp).ID

	resp = user.do("POST", "/api/v1/requests", map[string]any{"release": relBody("a")})
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "This release has already been requested.", errorBody(t, resp))

	resp = user.do("POST", "/api/v1/requests", map[string]any{"release": map[string]any{"title": "x", "protocol": "torrent"}})
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
