// Package importer places completed downloads into the library. It never
// modifies files under the download client path (except moving Usenet files)
// and never writes outside the item's library folder.
package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/fsutil"
)

type Mode string

const (
	ModeHardlink Mode = "hardlink"
	ModeCopy     Mode = "copy"
	ModeMove     Mode = "move"
)

type Source struct {
	BaseDir  string
	Files    []downloadclient.File
	Protocol string
}

type Result struct {
	Folder    string // absolute
	Mode      Mode
	Files     []downloadclient.File // relative to Folder, forward slashes
	TotalSize int64
}

var ErrUnsafePath = errors.New("unsafe file path in download")

// link is replaced in tests.
var link = os.Link

// rename is replaced in tests.
var rename = os.Rename

// copySource wraps the file being copied; tests replace it to slow a copy.
var copySource = func(r io.Reader) io.Reader { return r }

type plan struct {
	rel  []string // source segments
	dest []string // sanitized destination segments (common root stripped)
}

// splitPath validates an untrusted client-reported path and returns its segments.
func splitPath(p string) ([]string, error) {
	p = strings.ReplaceAll(p, `\`, "/")
	if p == "" || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") ||
		(len(p) >= 2 && p[1] == ':') {
		return nil, ErrUnsafePath
	}
	var segs []string
	for _, s := range strings.Split(p, "/") {
		switch s {
		case "..":
			return nil, ErrUnsafePath
		case "", ".":
			continue
		}
		segs = append(segs, s)
	}
	if len(segs) == 0 {
		return nil, ErrUnsafePath
	}
	return segs, nil
}

// checkSource verifies the source stays inside base, is a regular file, and
// that no component along the way is a symlink.
func checkSource(base string, segs []string) (string, error) {
	cur := base
	for i, s := range segs {
		cur = filepath.Join(cur, s)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", ErrUnsafePath
		}
		if i == len(segs)-1 && !fi.Mode().IsRegular() {
			return "", ErrUnsafePath
		}
	}
	if !fsutil.WithinOrEqual(base, cur) {
		return "", ErrUnsafePath
	}
	return cur, nil
}

func buildPlans(src Source) ([]plan, error) {
	plans := make([]plan, 0, len(src.Files))
	for _, f := range src.Files {
		segs, err := splitPath(f.Path)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan{rel: segs})
	}
	strip := len(plans) > 0
	for _, p := range plans {
		if len(p.rel) < 2 || p.rel[0] != plans[0].rel[0] {
			strip = false
			break
		}
	}
	seen := map[string]bool{}
	for i := range plans {
		segs := plans[i].rel
		if strip {
			segs = segs[1:]
		}
		dest := make([]string, len(segs))
		for j, s := range segs {
			dest[j] = SanitizeName(s)
		}
		key := path.Join(dest...)
		if seen[key] {
			return nil, fmt.Errorf("importer: duplicate destination %q", key)
		}
		seen[key] = true
		plans[i].dest = dest
	}
	return plans, nil
}

// createFolder claims libraryRoot/name, adding " (2)", " (3)", ... on collision.
func createFolder(libraryRoot, name string) (string, error) {
	for n := 1; ; n++ {
		candidate := name
		if n > 1 {
			candidate = name + " (" + strconv.Itoa(n) + ")"
		}
		folder := filepath.Join(libraryRoot, candidate)
		err := os.Mkdir(folder, 0o755)
		if err == nil {
			return folder, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
}

// ClaimFolder creates a new folder for releaseName directly in libraryRoot,
// adding " (2)", " (3)", ... if the name is taken, and returns its absolute
// path.
func ClaimFolder(libraryRoot, releaseName string) (string, error) {
	root, err := filepath.Abs(libraryRoot)
	if err != nil {
		return "", err
	}
	return createFolder(root, SanitizeName(releaseName))
}

// Import claims a folder for releaseName in libraryRoot and imports into it.
func Import(ctx context.Context, src Source, libraryRoot, releaseName string) (Result, error) {
	if err := check(ctx, src); err != nil {
		return Result{}, err
	}
	folder, err := ClaimFolder(libraryRoot, releaseName)
	if err != nil {
		return Result{}, err
	}
	return ImportInto(ctx, src, folder)
}

// check validates src before a folder is claimed for it.
func check(ctx context.Context, src Source) error {
	_, _, err := prepare(src)
	if err != nil {
		return err
	}
	return ctx.Err()
}

func prepare(src Source) ([]plan, []string, error) {
	if len(src.Files) == 0 {
		return nil, nil, errors.New("importer: no files to import")
	}
	plans, err := buildPlans(src)
	if err != nil {
		return nil, nil, err
	}
	base := filepath.Clean(src.BaseDir)
	sources := make([]string, len(plans))
	for i, p := range plans {
		if sources[i], err = checkSource(base, p.rel); err != nil {
			return nil, nil, err
		}
	}
	return plans, sources, nil
}

// ImportInto places src's files in folder, which the caller claimed with
// ClaimFolder. Temp files left there by an earlier copy that was killed are
// removed first. On failure folder is removed, unless it holds moved files
// that couldn't be put back.
func ImportInto(ctx context.Context, src Source, folder string) (res Result, err error) {
	type moved struct{ from, to string }
	var movedFiles []moved
	defer func() {
		if err == nil {
			return
		}
		// Moved files are the only copy outside the library: put them back
		// before removing the folder, and keep the folder if that fails.
		for i := len(movedFiles) - 1; i >= 0; i-- {
			m := movedFiles[i]
			if rerr := rename(m.to, m.from); rerr != nil {
				err = fmt.Errorf("%w; could not restore moved file %q, left in %q", err, m.from, folder)
				return
			}
		}
		os.RemoveAll(folder) // only the folder this import owns
	}()
	plans, sources, err := prepare(src)
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	if err = removeTempFiles(folder); err != nil {
		return Result{}, err
	}
	mode := ModeHardlink
	if src.Protocol == "usenet" {
		mode = ModeMove
	}
	result := Result{Folder: folder}
	for i, p := range plans {
		if err = ctx.Err(); err != nil {
			return Result{}, err
		}
		dst := filepath.Join(folder, filepath.Join(p.dest...))
		if !fsutil.Within(folder, dst) {
			return Result{}, ErrUnsafePath
		}
		if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return Result{}, err
		}
		switch mode {
		case ModeMove:
			if err = rename(sources[i], dst); err == nil {
				movedFiles = append(movedFiles, moved{sources[i], dst})
			}
		case ModeHardlink:
			err = link(sources[i], dst)
			if err != nil && isCrossDevice(err) {
				mode = ModeCopy
				err = copyFile(ctx, sources[i], dst)
			}
		default:
			err = copyFile(ctx, sources[i], dst)
		}
		if err != nil {
			return Result{}, err
		}
		fi, serr := os.Lstat(dst)
		if serr != nil {
			err = serr
			return Result{}, err
		}
		result.TotalSize += fi.Size()
		result.Files = append(result.Files, downloadclient.File{Path: path.Join(p.dest...), Size: fi.Size()})
	}
	result.Mode = mode
	return result, nil
}

// removeTempFiles deletes the temp files copyFile leaves when it is killed
// part way, anywhere under folder.
func removeTempFiles(folder string) error {
	return filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), tempPrefix) {
			return os.Remove(p)
		}
		return nil
	})
}

const tempPrefix = ".softwarr-tmp-"

// copyFile copies src to dst via a temp file in dst's directory, so a partial
// copy never has the final name. Mode bits are preserved, ownership is not.
// It checks ctx between reads, so cancelling stops a large copy promptly.
func copyFile(ctx context.Context, srcPath, dst string) (err error) {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), tempPrefix+"*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = io.Copy(tmp, fsutil.ContextReader(ctx, copySource(in))); err != nil {
		return err
	}
	if err = tmp.Chmod(fi.Mode().Perm()); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
