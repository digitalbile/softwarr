package httpapi

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAndPutSettings(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	resp := ts.do("GET", "/api/v1/settings", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	st := decode[settings.Settings](t, resp)
	assert.Len(t, st.APIKey, 32)

	root := filepath.Join(t.TempDir(), "library")
	st.CommentRequired = true
	st.LibraryRoot = root
	resp = ts.do("PUT", "/api/v1/settings", st)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	saved := decode[settings.Settings](t, resp)
	assert.True(t, saved.CommentRequired)
	assert.NoDirExists(t, root, "saving settings writes nothing to disk")

	got, err := ts.Deps.Settings.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, root, got.LibraryRoot)
	assert.Equal(t, st.APIKey, got.APIKey)
}

func TestPutSettingsValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	st := decode[settings.Settings](t, ts.do("GET", "/api/v1/settings", nil))
	st.OIDC.Enabled = true
	resp := ts.do("PUT", "/api/v1/settings", st)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Set the external URL before enabling OIDC.", errorBody(t, resp))

	st.OIDC.Enabled = false
	st.OIDC.AdminClaim = "groups"
	resp = ts.do("PUT", "/api/v1/settings", st)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Set both the admin claim and the admin value, or leave both empty.", errorBody(t, resp))
}

func TestRegenerateAPIKey(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	old := decode[settings.Settings](t, ts.do("GET", "/api/v1/settings", nil)).APIKey

	resp := ts.do("POST", "/api/v1/settings/apikey", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	fresh := decode[map[string]string](t, resp)["apiKey"]
	assert.Regexp(t, `^[0-9a-f]{32}$`, fresh)
	assert.NotEqual(t, old, fresh)

	anon := ts.newClient()
	resp = anon.request("GET", "/api/v1/settings", nil, map[string]string{"X-Api-Key": old})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp = anon.request("GET", "/api/v1/settings", nil, map[string]string{"X-Api-Key": fresh})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPutSettingsRejectsRelativeLibraryRoot(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	st := decode[settings.Settings](t, ts.do("GET", "/api/v1/settings", nil))
	st.LibraryRoot = "relative/lib"
	resp := ts.do("PUT", "/api/v1/settings", st)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "The library folder must be an absolute path.", errorBody(t, resp))
}
