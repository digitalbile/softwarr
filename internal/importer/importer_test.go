package importer

import (
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/downloadclient"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"a/b:c":     "a_b_c",
		"  .x.  ":   "x",
		"":          "untitled",
		"...":       "untitled",
		"a\x00b\tc": "a_b_c",
		`a\*?"<>|b`: "a_______b",
		"Name 1.0":  "Name 1.0",
	}
	for in, want := range cases {
		assert.Equal(t, want, SanitizeName(in), "input %q", in)
	}
	long := SanitizeName(strings.Repeat("é", 300))
	assert.LessOrEqual(t, len(long), 200)
	assert.True(t, utf8.ValidString(long))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func setup(t *testing.T, files map[string]string) (Source, string) {
	t.Helper()
	base := t.TempDir()
	lib := t.TempDir()
	require.NoError(t, PrepareRoot(lib))
	var fs []downloadclient.File
	// Sorted, so tests that fail the Nth file know which one that is.
	for _, p := range slices.Sorted(maps.Keys(files)) {
		c := files[p]
		write(t, filepath.Join(base, filepath.FromSlash(p)), c)
		fs = append(fs, downloadclient.File{Path: p, Size: int64(len(c))})
	}
	return Source{BaseDir: base, Files: fs, Protocol: "torrent"}, lib
}

// libEntries lists the library root, leaving out the root marker.
func libEntries(t *testing.T, lib string) []string {
	t.Helper()
	ents, err := os.ReadDir(lib)
	require.NoError(t, err)
	var names []string
	for _, e := range ents {
		if e.Name() != RootMarker {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestImportHardlinks(t *testing.T) {
	src, lib := setup(t, map[string]string{"a.bin": "hello"})
	res, err := Import(context.Background(), src, lib, "My App")
	require.NoError(t, err)
	assert.Equal(t, ModeHardlink, res.Mode)
	assert.Equal(t, filepath.Join(lib, "My App"), res.Folder)
	assert.Equal(t, int64(5), res.TotalSize)
	require.Len(t, res.Files, 1)
	assert.Equal(t, "a.bin", res.Files[0].Path)
	s, _ := os.Stat(filepath.Join(src.BaseDir, "a.bin"))
	d, err := os.Stat(filepath.Join(res.Folder, "a.bin"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(s, d))
}

func TestImportStripsCommonRoot(t *testing.T) {
	src, lib := setup(t, map[string]string{"Rel/a.bin": "1", "Rel/sub/b.bin": "22"})
	res, err := Import(context.Background(), src, lib, "Rel")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(res.Folder, "a.bin"))
	assert.FileExists(t, filepath.Join(res.Folder, "sub", "b.bin"))
	paths := []string{}
	for _, f := range res.Files {
		paths = append(paths, f.Path)
	}
	assert.ElementsMatch(t, []string{"a.bin", "sub/b.bin"}, paths)
	assert.Equal(t, int64(3), res.TotalSize)
}

func TestImportFolderCollisionSuffix(t *testing.T) {
	src, lib := setup(t, map[string]string{"a.bin": "x"})
	r1, err := Import(context.Background(), src, lib, "App")
	require.NoError(t, err)
	r2, err := Import(context.Background(), src, lib, "App")
	require.NoError(t, err)
	r3, err := Import(context.Background(), src, lib, "App")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(lib, "App"), r1.Folder)
	assert.Equal(t, filepath.Join(lib, "App (2)"), r2.Folder)
	assert.Equal(t, filepath.Join(lib, "App (3)"), r3.Folder)
}

func TestImportMissingLibraryRoot(t *testing.T) {
	src, lib := setup(t, map[string]string{"a.bin": "x"})
	_, err := Import(context.Background(), src, filepath.Join(lib, "nope"), "App")
	require.Error(t, err)
}

func TestImportFallsBackToCopyOnEXDEV(t *testing.T) {
	orig := link
	t.Cleanup(func() { link = orig })
	link = func(o, n string) error { return &os.LinkError{Op: "link", Old: o, New: n, Err: syscall.EXDEV} }
	src, lib := setup(t, map[string]string{"a.bin": "hello", "b.bin": "world"})
	require.NoError(t, os.Chmod(filepath.Join(src.BaseDir, "a.bin"), 0o640))
	res, err := Import(context.Background(), src, lib, "App")
	require.NoError(t, err)
	assert.Equal(t, ModeCopy, res.Mode)
	for _, n := range []string{"a.bin", "b.bin"} {
		sb, _ := os.ReadFile(filepath.Join(src.BaseDir, n))
		db, err := os.ReadFile(filepath.Join(res.Folder, n))
		require.NoError(t, err)
		assert.Equal(t, sb, db)
		s, _ := os.Stat(filepath.Join(src.BaseDir, n))
		d, _ := os.Stat(filepath.Join(res.Folder, n))
		assert.False(t, os.SameFile(s, d))
	}
	d, _ := os.Stat(filepath.Join(res.Folder, "a.bin"))
	assert.Equal(t, os.FileMode(0o640), d.Mode().Perm())
	ents, _ := os.ReadDir(res.Folder)
	assert.Len(t, ents, 3, "the two files and the marker, no temp files")
}

func TestImportOtherLinkErrorFailsAndCleansUp(t *testing.T) {
	orig := link
	t.Cleanup(func() { link = orig })
	link = func(o, n string) error { return &os.LinkError{Op: "link", Old: o, New: n, Err: syscall.EPERM} }
	src, lib := setup(t, map[string]string{"a.bin": "x"})
	_, err := Import(context.Background(), src, lib, "App")
	require.Error(t, err)
	ents := libEntries(t, lib)
	assert.Empty(t, ents)
}

func TestImportRejectsEscapingPaths(t *testing.T) {
	for _, bad := range []string{"../../etc/x", "/abs", `C:\x`, `\\srv\x`, "a/../../b", "", "a\x00b", `..\x`} {
		src, lib := setup(t, map[string]string{"ok.bin": "x"})
		src.Files = append(src.Files, downloadclient.File{Path: bad})
		_, err := Import(context.Background(), src, lib, "App")
		require.ErrorIs(t, err, ErrUnsafePath, "path %q", bad)
		ents := libEntries(t, lib)
		assert.Empty(t, ents, "path %q", bad)
	}
}

func TestImportRejectsSymlinkSource(t *testing.T) {
	src, lib := setup(t, map[string]string{"ok.bin": "x"})
	secret := filepath.Join(t.TempDir(), "secret")
	write(t, secret, "s")
	require.NoError(t, os.Symlink(secret, filepath.Join(src.BaseDir, "evil.bin")))
	src.Files = append(src.Files, downloadclient.File{Path: "evil.bin"})
	_, err := Import(context.Background(), src, lib, "App")
	require.ErrorIs(t, err, ErrUnsafePath)
	ents := libEntries(t, lib)
	assert.Empty(t, ents)
}

func TestImportCanceledContext(t *testing.T) {
	src, lib := setup(t, map[string]string{"a.bin": "x"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Import(ctx, src, lib, "App")
	require.ErrorIs(t, err, context.Canceled)
	ents := libEntries(t, lib)
	assert.Empty(t, ents)
}

func TestImportSourceUntouched(t *testing.T) {
	for _, exdev := range []bool{false, true} {
		orig := link
		if exdev {
			link = func(o, n string) error { return &os.LinkError{Err: syscall.EXDEV} }
		}
		src, lib := setup(t, map[string]string{"a.bin": "hello"})
		p := filepath.Join(src.BaseDir, "a.bin")
		old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
		require.NoError(t, os.Chtimes(p, old, old))
		_, err := Import(context.Background(), src, lib, "App")
		link = orig
		require.NoError(t, err)
		st, _ := os.Stat(p)
		assert.True(t, st.ModTime().Equal(old))
		b, _ := os.ReadFile(p)
		assert.Equal(t, "hello", string(b))
	}
}

func TestImportUsenetMoves(t *testing.T) {
	src, lib := setup(t, map[string]string{"a.nzbout": "x"})
	src.Protocol = "usenet"
	res, err := Import(context.Background(), src, lib, "App")
	require.NoError(t, err)
	assert.Equal(t, ModeMove, res.Mode)
	assert.NoFileExists(t, filepath.Join(src.BaseDir, "a.nzbout"))
	assert.FileExists(t, filepath.Join(res.Folder, "a.nzbout"))
}

func TestImportMoveFailureRestoresMovedFiles(t *testing.T) {
	orig := rename
	t.Cleanup(func() { rename = orig })
	calls := 0
	rename = func(o, n string) error {
		calls++
		if calls == 2 {
			return os.ErrPermission
		}
		return os.Rename(o, n)
	}
	src, lib := setup(t, map[string]string{"a.bin": "first", "b.bin": "second"})
	src.Protocol = "usenet"
	_, err := Import(context.Background(), src, lib, "App")
	require.Error(t, err)
	for n, c := range map[string]string{"a.bin": "first", "b.bin": "second"} {
		b, rerr := os.ReadFile(filepath.Join(src.BaseDir, n))
		require.NoError(t, rerr)
		assert.Equal(t, c, string(b))
	}
	ents := libEntries(t, lib)
	assert.Empty(t, ents)
}

func TestImportMoveRestoreFailureKeepsFolder(t *testing.T) {
	orig := rename
	t.Cleanup(func() { rename = orig })
	calls := 0
	rename = func(o, n string) error {
		calls++
		if calls >= 2 {
			return os.ErrPermission
		}
		return os.Rename(o, n)
	}
	src, lib := setup(t, map[string]string{"a.bin": "first", "b.bin": "second"})
	src.Protocol = "usenet"
	_, err := Import(context.Background(), src, lib, "App")
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(lib, "App"))
	assert.FileExists(t, filepath.Join(lib, "App", "a.bin"))
}

// slowReader hands out small reads and runs after once the first read is done.
type slowReader struct {
	r     io.Reader
	after func()
	reads int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if len(p) > 4096 {
		p = p[:4096]
	}
	n, err := s.r.Read(p)
	s.reads++
	if s.reads == 1 {
		s.after()
	}
	time.Sleep(time.Millisecond)
	return n, err
}

func TestImportCancelDuringCopyStopsAndCleansUp(t *testing.T) {
	origLink, origSource := link, copySource
	t.Cleanup(func() { link, copySource = origLink, origSource })
	link = func(o, n string) error { return &os.LinkError{Op: "link", Old: o, New: n, Err: syscall.EXDEV} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sr *slowReader
	copySource = func(r io.Reader) io.Reader {
		sr = &slowReader{r: r, after: cancel}
		return sr
	}
	src, lib := setup(t, map[string]string{"big.bin": strings.Repeat("x", 4<<20)})

	start := time.Now()
	_, err := Import(ctx, src, lib, "App")
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 2*time.Second, "the copy stops soon after cancel")
	assert.Less(t, sr.reads, 5)
	ents := libEntries(t, lib)
	assert.Empty(t, ents, "no folder or temp file is left")
}

func TestImportIntoRemovesStaleTempFiles(t *testing.T) {
	src, lib := setup(t, map[string]string{"a.bin": "hello"})
	folder, err := ClaimFolder(lib, "App", 0)
	require.NoError(t, err)
	write(t, filepath.Join(folder, ".softwarr-tmp-111"), "partial")
	write(t, filepath.Join(folder, "sub", ".softwarr-tmp-222"), "partial")

	res, err := ImportInto(context.Background(), src, folder)
	require.NoError(t, err)
	assert.Equal(t, folder, res.Folder)
	assert.NoFileExists(t, filepath.Join(folder, ".softwarr-tmp-111"))
	assert.NoFileExists(t, filepath.Join(folder, "sub", ".softwarr-tmp-222"))
	assert.FileExists(t, filepath.Join(folder, "a.bin"))
}

func TestImportIntoRemovesFolderOnFailure(t *testing.T) {
	src, lib := setup(t, map[string]string{"a.bin": "hello"})
	folder, err := ClaimFolder(lib, "App", 0)
	require.NoError(t, err)
	src.Files = append(src.Files, downloadclient.File{Path: "missing.bin"})
	_, err = ImportInto(context.Background(), src, folder)
	require.Error(t, err)
	assert.NoDirExists(t, folder)
}

func TestClaimFolderMarksIt(t *testing.T) {
	lib := t.TempDir()
	require.NoError(t, PrepareRoot(lib))
	folder, err := ClaimFolder(lib, "App", 7)
	require.NoError(t, err)
	assert.True(t, FolderMarkedFor(folder, 7))
	assert.False(t, FolderMarkedFor(folder, 8))
	assert.False(t, FolderMarkedFor(t.TempDir(), 7), "no marker")

	// The marker isn't one of the item's files.
	src, _ := setup(t, map[string]string{"a.bin": "hello"})
	res, err := ImportInto(context.Background(), src, folder)
	require.NoError(t, err)
	assert.Equal(t, []downloadclient.File{{Path: "a.bin", Size: 5}}, res.Files)
	assert.Equal(t, int64(5), res.TotalSize)
	assert.True(t, FolderMarkedFor(folder, 7))

	// A symlinked marker doesn't count.
	other := t.TempDir()
	require.NoError(t, MarkItemFolder(other, 9))
	linked := t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(other, ItemMarker), filepath.Join(linked, ItemMarker)))
	assert.False(t, FolderMarkedFor(linked, 9))
}

func TestRootMarker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lib", "apps")
	assert.False(t, RootReady(root))

	require.NoError(t, PrepareRoot(root))
	assert.True(t, RootReady(root))
	require.NoError(t, PrepareRoot(root), "again is fine")
	folder, err := ClaimFolder(root, "App", 1)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "App"), folder)
}
