package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/importer"
	"github.com/digitalbile/softwarr/internal/library"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addLibraryItem creates files in a library root and an item for requesterID
// (0 for none), returning the item id and folder.
func addLibraryItem(t *testing.T, ts *testServer, root, name string, requesterID int64, files map[string]string) (int64, string) {
	t.Helper()
	ctx := context.Background()
	cur, err := ts.Deps.Settings.Get(ctx)
	require.NoError(t, err)
	if cur.LibraryRoot != root {
		require.NoError(t, importer.PrepareRoot(root))
		cur.LibraryRoot = root
		require.NoError(t, ts.Deps.Settings.Save(ctx, cur))
	}
	folder := filepath.Join(root, name)
	res := importer.Result{Folder: folder, Mode: importer.ModeHardlink}
	for rel, content := range files {
		p := filepath.Join(folder, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		res.Files = append(res.Files, downloadclient.File{Path: rel, Size: int64(len(content))})
		res.TotalSize += int64(len(content))
	}
	var reqID int64
	if requesterID != 0 {
		r, err := ts.DB.Exec(`INSERT INTO requests (requester_id, status, release, release_key, comment, created_at, updated_at)
			VALUES (?, 'available', '{}', ?, 'please and thanks', 1, 1)`, requesterID, "k:"+name)
		require.NoError(t, err)
		reqID, _ = r.LastInsertId()
	}
	require.NoError(t, importer.MarkItemFolder(folder, reqID))
	rel := prowlarr.Release{GUID: name, Title: name, Indexer: "idx", Protocol: "torrent", InfoHash: name}
	id, err := library.CreateItem(ctx, ts.Q, reqID, name+" title", rel, res, ts.Clock.Now())
	require.NoError(t, err)
	return id, folder
}

func TestLibraryListGetAndFilter(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.loginAsOwner()
	root := t.TempDir()
	id, _ := addLibraryItem(t, ts, root, "Alpha", owner.ID, map[string]string{"a.txt": "a"})
	addLibraryItem(t, ts, root, "Beta", 0, map[string]string{"b.txt": "b"})

	resp := ts.do("GET", "/api/v1/library", nil)
	require.Equal(t, 200, resp.StatusCode)
	assert.Len(t, decode[[]map[string]any](t, resp), 2)

	resp = ts.do("GET", "/api/v1/library?q=alpha", nil)
	list := decode[[]map[string]any](t, resp)
	require.Len(t, list, 1)
	assert.Equal(t, "Alpha title", list[0]["title"])

	resp = ts.do("GET", "/api/v1/library/"+itoa(id), nil)
	require.Equal(t, 200, resp.StatusCode)
	got := decode[map[string]any](t, resp)
	assert.Equal(t, "Alpha", got["releaseName"])
	assert.Equal(t, "owner", got["requester"])
	assert.Nil(t, got["retentionDays"])
	assert.Equal(t, []any{map[string]any{"path": "a.txt", "size": float64(1)}}, got["files"])

	resp = ts.do("GET", "/api/v1/library/999", nil)
	assert.Equal(t, 404, resp.StatusCode)
	assert.Equal(t, "That library item doesn't exist.", errorBody(t, resp))
	resp = ts.do("GET", "/api/v1/library/abc", nil)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestLibraryCommentHiddenFromOtherUsers(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.loginAsOwner()
	user := ts.loginAsUser()
	oc := ts.newClient()
	oc.login("owner", testPassword)
	root := t.TempDir()
	ownIDItem, _ := addLibraryItem(t, ts, root, "Mine", user.ID, map[string]string{"a.txt": "a"})
	otherItem, _ := addLibraryItem(t, ts, root, "Theirs", owner.ID, map[string]string{"b.txt": "b"})

	c := ts.newClient()
	c.login("user", testPassword)
	mine := decode[map[string]any](t, c.do("GET", "/api/v1/library/"+itoa(ownIDItem), nil))
	assert.Equal(t, "please and thanks", mine["comment"])
	theirs := decode[map[string]any](t, c.do("GET", "/api/v1/library/"+itoa(otherItem), nil))
	assert.Equal(t, "", theirs["comment"])
	for _, it := range decode[[]map[string]any](t, c.do("GET", "/api/v1/library", nil)) {
		if it["title"] == "Theirs title" {
			assert.Equal(t, "", it["comment"])
		}
	}

	admin := decode[map[string]any](t, oc.do("GET", "/api/v1/library/"+itoa(ownIDItem), nil))
	assert.Equal(t, "please and thanks", admin["comment"])
}

func TestLibraryAdminOnlyMutations(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	ts.loginAsUser()
	oc := ts.newClient()
	oc.login("owner", testPassword)
	id, folder := addLibraryItem(t, ts, t.TempDir(), "Item", 0, map[string]string{"a.txt": "a"})

	c := ts.newClient()
	c.login("user", testPassword)
	assert.Equal(t, 403, c.do("PATCH", "/api/v1/library/"+itoa(id), map[string]any{"title": "x"}).StatusCode)
	assert.Equal(t, 403, c.do("DELETE", "/api/v1/library/"+itoa(id), nil).StatusCode)
	assert.DirExists(t, folder)

	anon := ts.newClient()
	assert.Equal(t, 401, anon.do("GET", "/api/v1/library", nil).StatusCode)

	resp := oc.do("PATCH", "/api/v1/library/"+itoa(id), map[string]any{"title": "Renamed", "retentionDays": 7})
	require.Equal(t, 200, resp.StatusCode)
	got := decode[map[string]any](t, resp)
	assert.Equal(t, "Renamed", got["title"])
	assert.Equal(t, float64(7), got["retentionDays"])
	assert.NotNil(t, got["expiresAt"])

	// An absent key is unchanged; an explicit null resets it.
	resp = oc.do("PATCH", "/api/v1/library/"+itoa(id), map[string]any{"title": "Again"})
	got = decode[map[string]any](t, resp)
	assert.Equal(t, float64(7), got["retentionDays"])
	resp = oc.do("PATCH", "/api/v1/library/"+itoa(id), map[string]any{"retentionDays": nil})
	got = decode[map[string]any](t, resp)
	assert.Nil(t, got["retentionDays"])
	assert.Equal(t, "Again", got["title"])
	resp = oc.do("PATCH", "/api/v1/library/"+itoa(id), map[string]any{"title": nil})
	assert.Equal(t, "Item", decode[map[string]any](t, resp)["title"])

	resp = oc.do("PATCH", "/api/v1/library/"+itoa(id), map[string]any{"retentionDays": -1})
	assert.Equal(t, 400, resp.StatusCode)
	resp = oc.do("PATCH", "/api/v1/library/"+itoa(id), map[string]any{"bogus": 1})
	assert.Equal(t, 400, resp.StatusCode)
	resp = oc.do("PATCH", "/api/v1/library/999", map[string]any{"title": "x"})
	assert.Equal(t, 404, resp.StatusCode)

	resp = oc.do("DELETE", "/api/v1/library/"+itoa(id), nil)
	assert.Equal(t, 204, resp.StatusCode)
	assert.NoDirExists(t, folder)
	assert.Equal(t, 404, oc.do("GET", "/api/v1/library/"+itoa(id), nil).StatusCode)
	assert.Equal(t, 404, oc.do("DELETE", "/api/v1/library/"+itoa(id), nil).StatusCode)
}

func TestDeleteWithMissingRootConflicts(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	root := t.TempDir()
	id, _ := addLibraryItem(t, ts, root, "Item", 0, map[string]string{"a.txt": "a"})
	require.NoError(t, os.RemoveAll(root))
	resp := ts.do("DELETE", "/api/v1/library/"+itoa(id), nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "The library folder for this item is missing. Check that the library path is mounted.", errorBody(t, resp))
	assert.Equal(t, http.StatusOK, ts.do("GET", "/api/v1/library/"+itoa(id), nil).StatusCode)
}

func TestItemWithMissingFolderIsGoneAndCanBeDeleted(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	root := t.TempDir()
	id, folder := addLibraryItem(t, ts, root, "Item", 0, map[string]string{"a.txt": "a"})
	addLibraryItem(t, ts, root, "Other", 0, map[string]string{"b.txt": "b"})
	require.NoError(t, os.RemoveAll(folder))

	resp := ts.do("GET", "/api/v1/library/"+itoa(id)+"/download", nil)
	assert.Equal(t, http.StatusGone, resp.StatusCode)
	assert.Equal(t, http.StatusNoContent, ts.do("DELETE", "/api/v1/library/"+itoa(id), nil).StatusCode)
	assert.Equal(t, http.StatusNotFound, ts.do("GET", "/api/v1/library/"+itoa(id), nil).StatusCode)
}

func TestDownloadMissingFileReturnsGone(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	id, folder := addLibraryItem(t, ts, t.TempDir(), "Item", 0, map[string]string{"a.txt": "a", "b.txt": "b"})
	require.NoError(t, os.Remove(filepath.Join(folder, "b.txt")))

	resp := ts.do("GET", "/api/v1/library/"+itoa(id)+"/download", nil)
	assert.Equal(t, http.StatusGone, resp.StatusCode)
	assert.Equal(t, "Files for this item are missing on disk.", errorBody(t, resp))
}

func TestDownloadAndDeleteRefuseSymlinkedFolder(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	id, folder := addLibraryItem(t, ts, t.TempDir(), "Item", 0, map[string]string{"a.txt": "a"})
	elsewhere := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "a.txt"), []byte("a"), 0o644))
	require.NoError(t, os.RemoveAll(folder))
	require.NoError(t, os.Symlink(elsewhere, folder))

	const msg = "The folder for this item isn't a plain folder inside the library folder, so softwarr left it alone. Check the library folder in Settings."
	resp := ts.do("GET", "/api/v1/library/"+itoa(id)+"/download", nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, msg, errorBody(t, resp))
	resp = ts.do("DELETE", "/api/v1/library/"+itoa(id), nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, msg, errorBody(t, resp))
	assert.FileExists(t, filepath.Join(elsewhere, "a.txt"))
}

func TestDeleteAfterRootRepointedAtDownloadsConflicts(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	id, _ := addLibraryItem(t, ts, t.TempDir(), "Thing", 0, map[string]string{"setup.dmg": "x"})
	downloads := t.TempDir()
	seeding := filepath.Join(downloads, "Thing", "setup.dmg")
	require.NoError(t, os.MkdirAll(filepath.Dir(seeding), 0o755))
	require.NoError(t, os.WriteFile(seeding, []byte("x"), 0o644))
	cur, err := ts.Deps.Settings.Get(context.Background())
	require.NoError(t, err)
	cur.LibraryRoot = downloads
	require.NoError(t, ts.Deps.Settings.Save(context.Background(), cur))

	resp := ts.do("DELETE", "/api/v1/library/"+itoa(id), nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "softwarr couldn't confirm that the folder for this item is its own, so it left it alone. Check the library folder in Settings.", errorBody(t, resp))
	assert.FileExists(t, seeding)
}

func TestDownloadFindsFolderAfterRootMoves(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	root := t.TempDir()
	id, _ := addLibraryItem(t, ts, root, "Item", 0, map[string]string{"x.dmg": "0123456789"})
	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(root, moved))
	cur, err := ts.Deps.Settings.Get(context.Background())
	require.NoError(t, err)
	cur.LibraryRoot = " " + moved + " "
	require.NoError(t, ts.Deps.Settings.Save(context.Background(), cur))

	resp := ts.do("GET", "/api/v1/library/"+itoa(id)+"/download", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(body))
	assert.Equal(t, http.StatusNoContent, ts.do("DELETE", "/api/v1/library/"+itoa(id), nil).StatusCode)
	assert.NoDirExists(t, filepath.Join(moved, "Item"))
}

func TestDownloadUpdatesCountAndTime(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	root := t.TempDir()
	id, _ := addLibraryItem(t, ts, root, "Item", 0, map[string]string{"a.txt": "alpha", "b.txt": "bravo"})

	resp := ts.do("GET", "/api/v1/library/"+itoa(id)+"/download", nil)
	require.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	assert.Len(t, zr.File, 2)

	it := decode[map[string]any](t, ts.do("GET", "/api/v1/library/"+itoa(id), nil))
	assert.Equal(t, float64(1), it["downloadCount"])
	assert.NotNil(t, it["lastDownloadedAt"])

	archive, _ := addLibraryItem(t, ts, root, "Arch", 0, map[string]string{"x.dmg": "0123456789"})
	resp = ts.request("GET", "/api/v1/library/"+itoa(archive)+"/download", nil, map[string]string{"Range": "bytes=0-9"})
	assert.Equal(t, http.StatusPartialContent, resp.StatusCode)
	_, _ = io.Copy(io.Discard, resp.Body)
	resp = ts.request("HEAD", "/api/v1/library/"+itoa(archive)+"/download", nil, nil)
	assert.Equal(t, 200, resp.StatusCode)
	it = decode[map[string]any](t, ts.do("GET", "/api/v1/library/"+itoa(archive), nil))
	assert.Equal(t, float64(1), it["downloadCount"])
}
