package httpapi

import (
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testUI() fs.FS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><html><head><title>x</title></head><body></body></html>")},
		"assets/app-abc123.js": {Data: []byte("console.log(1)")},
		"favicon.svg":          {Data: []byte("<svg/>")},
	}
}

func uiServer(t *testing.T, urlBase string, ui fs.FS) *testServer {
	return newTestServerWith(t, urlBase, func(d *Deps) { d.UI = ui })
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func TestSPAFallbackServesIndex(t *testing.T) {
	ts := uiServer(t, "", testUI())
	resp := ts.do("GET", "/library/5", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	body := readBody(t, resp)
	assert.Contains(t, body, "window.__SOFTWARR__")
	assert.Contains(t, body, `<head><base href="/">`)
}

func TestSPAInjectsURLBase(t *testing.T) {
	ts := uiServer(t, "/sw", testUI())
	resp := ts.do("GET", "/sw/requests", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body := readBody(t, resp)
	assert.Contains(t, body, `<base href="/sw/">`)
	assert.Contains(t, body, `{"urlBase":"/sw"}`)
}

func TestSPAEscapesURLBase(t *testing.T) {
	ts := uiServer(t, `/a"b</script>`, testUI())
	resp := ts.do("GET", `/a%22b%3C/script%3E/x`, nil)
	body := readBody(t, resp)
	assert.NotContains(t, body, `"b</script>`)
	assert.Contains(t, body, `&#34;b&lt;/script&gt;/`)
}

func TestSPAAssetCacheHeaders(t *testing.T) {
	ts := uiServer(t, "", testUI())
	resp := ts.do("GET", "/assets/app-abc123.js", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "public, max-age=31536000, immutable", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "console.log(1)", readBody(t, resp))

	resp = ts.do("GET", "/favicon.svg", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, resp.Header.Get("Cache-Control"), "immutable")
	assert.Equal(t, "<svg/>", readBody(t, resp))
}

func TestSPAMissingAssetFallsBackToIndex(t *testing.T) {
	ts := uiServer(t, "", testUI())
	resp := ts.do("GET", "/assets/nope.js", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, readBody(t, resp), "window.__SOFTWARR__")
}

func TestAPIPathNotSwallowedBySPA(t *testing.T) {
	ts := uiServer(t, "", testUI())
	resp := ts.do("GET", "/api/v1/nope", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	assert.False(t, strings.Contains(readBody(t, resp), "__SOFTWARR__"))
}

func TestSPAMissingIndexReturns503(t *testing.T) {
	ts := uiServer(t, "", fstest.MapFS{".gitkeep": {}})
	for _, p := range []string{"/", "/library/5"} {
		resp := ts.do("GET", p, nil)
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		assert.Contains(t, readBody(t, resp), "The web UI hasn't been built. Run make web.")
	}
}

func TestWrongMethodOnAPIRouteIs405(t *testing.T) {
	ts := uiServer(t, "", testUI())
	resp := ts.do("POST", "/api/v1/ping", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Allow"), "GET")
}
