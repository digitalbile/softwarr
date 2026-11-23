package library

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (e *env) download(t *testing.T, id int64, method string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	it, err := e.svc.Get(context.Background(), id)
	require.NoError(t, err)
	req := httptest.NewRequest(method, "/dl", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	require.NoError(t, e.svc.Download(rec, req, it))
	return rec
}

func (e *env) count(t *testing.T, id int64) (n int, last bool) {
	t.Helper()
	it, err := e.svc.Get(context.Background(), id)
	require.NoError(t, err)
	return it.DownloadCount, it.LastDownloadedAt != nil
}

func TestServeSingleArchiveDirect(t *testing.T) {
	e := newEnv(t)
	id := e.addItem(t, "App", map[string]string{"setup.dmg": "0123456789abcdef"})
	rec := e.download(t, id, "GET", nil)
	assert.Equal(t, 200, rec.Code)
	assert.NotContains(t, rec.Header().Get("Content-Type"), "zip")
	assert.Equal(t, "attachment; filename=setup.dmg", rec.Header().Get("Content-Disposition"))
	assert.Equal(t, "0123456789abcdef", rec.Body.String())

	rec = e.download(t, id, "GET", map[string]string{"Range": "bytes=0-9"})
	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, "0123456789", rec.Body.String())
}

func TestServeMultiFileZip(t *testing.T) {
	e := newEnv(t)
	id := e.addItem(t, "Multi", map[string]string{"a.txt": "alpha", "sub/b.bin": "bravo!"})
	rec := e.download(t, id, "GET", nil)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Equal(t, "attachment; filename=\"Multi title.zip\"", rec.Header().Get("Content-Disposition"))
	assert.Empty(t, rec.Header().Get("Content-Length"))

	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	got := map[string]string{}
	for _, f := range zr.File {
		assert.Equal(t, zip.Store, f.Method, f.Name)
		rc, err := f.Open()
		require.NoError(t, err)
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		rc.Close()
		got[f.Name] = string(b)
	}
	assert.Equal(t, map[string]string{"a.txt": "alpha", "sub/b.bin": "bravo!"}, got)
}

func TestServeSingleNonArchiveZipped(t *testing.T) {
	e := newEnv(t)
	id := e.addItem(t, "Doc", map[string]string{"readme.txt": "hello"})
	rec := e.download(t, id, "GET", nil)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	assert.Equal(t, "readme.txt", zr.File[0].Name)
}

func TestServeNonASCIIName(t *testing.T) {
	e := newEnv(t)
	id := e.addItem(t, "Uni", map[string]string{"café.dmg": "x"})
	rec := e.download(t, id, "GET", nil)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "filename*=utf-8''caf%C3%A9.dmg")
}

func TestServeMissingFileReturnsErrBeforeWriting(t *testing.T) {
	e := newEnv(t)
	id := e.addItem(t, "Gone", map[string]string{"a.txt": "a", "b.txt": "b"})
	require.NoError(t, os.Remove(filepath.Join(e.root, "Gone", "b.txt")))
	it, err := e.svc.Get(context.Background(), id)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	err = e.svc.Download(rec, httptest.NewRequest("GET", "/dl", nil), it)
	assert.ErrorIs(t, err, ErrFilesMissing)
	assert.Zero(t, rec.Body.Len())
	n, _ := e.count(t, id)
	assert.Zero(t, n)
}

func TestServeRefusesPathEscapes(t *testing.T) {
	e := newEnv(t)
	secret := filepath.Join(e.root, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("s"), 0o644))
	id := e.addItem(t, "Esc", map[string]string{"a.txt": "a"})
	_, err := e.db.Exec("UPDATE library_files SET rel_path = '../secret.txt' WHERE item_id = ?", id)
	require.NoError(t, err)
	it, err := e.svc.Get(context.Background(), id)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	err = e.svc.Download(rec, httptest.NewRequest("GET", "/dl", nil), it)
	assert.ErrorIs(t, err, ErrUnsafePath)
	assert.Zero(t, rec.Body.Len())

	_, err = e.db.Exec("UPDATE library_items SET folder = ? WHERE id = ?", t.TempDir(), id)
	require.NoError(t, err)
	it, err = e.svc.Get(context.Background(), id)
	require.NoError(t, err)
	err = e.svc.Download(httptest.NewRecorder(), httptest.NewRequest("GET", "/dl", nil), it)
	assert.ErrorIs(t, err, ErrUnsafePath)
}

func TestDownloadCounting(t *testing.T) {
	e := newEnv(t)
	arch := e.addItem(t, "Arch", map[string]string{"a.zip": "0123456789"})
	multi := e.addItem(t, "Multi", map[string]string{"a.txt": "a", "b.txt": "b"})

	e.download(t, arch, "HEAD", nil)
	e.download(t, multi, "HEAD", nil)
	n, last := e.count(t, arch)
	assert.Zero(t, n)
	assert.False(t, last)

	e.download(t, arch, "GET", nil)
	n, last = e.count(t, arch)
	assert.Equal(t, 1, n)
	assert.True(t, last)

	e.download(t, arch, "GET", map[string]string{"Range": "bytes=5-"})
	n, _ = e.count(t, arch)
	assert.Equal(t, 1, n, "a range not starting at 0 does not count")

	e.download(t, arch, "GET", map[string]string{"Range": "bytes=0-3"})
	n, _ = e.count(t, arch)
	assert.Equal(t, 2, n)

	e.download(t, multi, "GET", nil)
	n, _ = e.count(t, multi)
	assert.Equal(t, 1, n)
}
