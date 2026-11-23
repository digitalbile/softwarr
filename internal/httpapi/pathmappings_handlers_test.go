package httpapi

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mappingBody struct {
	ID         int64  `json:"id"`
	Host       string `json:"host"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
}

func TestPathMappingCRUD(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()

	resp := ts.do("POST", "/api/v1/pathmappings", map[string]any{"host": "qbt", "remotePath": "/downloads", "localPath": "/data"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decode[mappingBody](t, resp)
	assert.Equal(t, "/downloads/", created.RemotePath)
	assert.Equal(t, "/data/", created.LocalPath)

	resp = ts.do("POST", "/api/v1/pathmappings", map[string]any{"host": "win", "remotePath": `D:\Downloads`, "localPath": "/data/win/"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, `D:\Downloads\`, decode[mappingBody](t, resp).RemotePath)

	list := decode[[]mappingBody](t, ts.do("GET", "/api/v1/pathmappings", nil))
	require.Len(t, list, 2)

	path := "/api/v1/pathmappings/" + strconv.FormatInt(created.ID, 10)
	resp = ts.do("PUT", path, map[string]any{"host": "qbt2", "remotePath": "/dl/", "localPath": "/mnt"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, mappingBody{ID: created.ID, Host: "qbt2", RemotePath: "/dl/", LocalPath: "/mnt/"}, decode[mappingBody](t, resp))

	require.Equal(t, http.StatusNoContent, ts.do("DELETE", path, nil).StatusCode)
	list = decode[[]mappingBody](t, ts.do("GET", "/api/v1/pathmappings", nil))
	assert.Len(t, list, 1)
}

func TestPathMappingValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	cases := map[string]map[string]any{
		"host":   {"host": " ", "remotePath": "/a", "localPath": "/b"},
		"remote": {"host": "h", "remotePath": "", "localPath": "/b"},
		"local":  {"host": "h", "remotePath": "/a", "localPath": ""},
		"abs":    {"host": "h", "remotePath": "/a", "localPath": "relative/path"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := ts.do("POST", "/api/v1/pathmappings", body)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestPathMappingNotFound(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	body := map[string]any{"host": "h", "remotePath": "/a", "localPath": "/b"}
	for _, resp := range []*http.Response{
		ts.do("PUT", "/api/v1/pathmappings/99", body),
		ts.do("PUT", "/api/v1/pathmappings/abc", body),
		ts.do("DELETE", "/api/v1/pathmappings/99", nil),
	} {
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		assert.Equal(t, "That path mapping doesn't exist.", decode[map[string]string](t, resp)["error"])
	}
}

func TestPathMappingAdminOnly(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsUser()
	assert.Equal(t, http.StatusForbidden, ts.do("GET", "/api/v1/pathmappings", nil).StatusCode)
	assert.Equal(t, http.StatusForbidden, ts.do("POST", "/api/v1/pathmappings", map[string]any{}).StatusCode)
}
