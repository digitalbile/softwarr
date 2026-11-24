package library

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/fsutil"
	"github.com/digitalbile/softwarr/internal/importer"
)

var ErrFilesMissing = errors.New("Files for this item are missing on disk.")

// ErrAborted means the response had started when the download failed, so the
// caller can no longer send an error body.
// testHookAfterStat runs after files are stat-ed; tests use it to race removals.
var testHookAfterStat func()

var ErrAborted = errors.New("library: download aborted")

var archiveExts = map[string]bool{
	".zip": true, ".7z": true, ".rar": true, ".dmg": true, ".iso": true, ".pkg": true, ".mpkg": true,
	".exe": true, ".msi": true, ".img": true, ".tar": true, ".gz": true, ".tgz": true, ".xz": true,
	".bz2": true, ".zst": true,
}

func attachment(name string) string {
	if v := mime.FormatMediaType("attachment", map[string]string{"filename": name}); v != "" {
		return v
	}
	return "attachment"
}

// Download serves an item from its stored folder and records the download
// when the response starts at byte 0 of a GET.
func (s *Service) Download(w http.ResponseWriter, r *http.Request, it Item) error {
	if err := checkFolder(it.Folder); err != nil {
		return err
	}
	return serve(w, r, it.Folder, it, func() {
		ctx := context.WithoutCancel(r.Context())
		if err := s.q.RecordLibraryDownload(ctx, dbq.RecordLibraryDownloadParams{
			LastDownloadedAt: nullTime(s.now().Unix()), ID: it.ID,
		}); err != nil {
			slog.Error("library: record download", "id", it.ID, "err", err)
		} else {
			s.publish(it.ID)
		}
	})
}

type resolved struct {
	rel  string
	path string
	info os.FileInfo
}

// serve sends the item's files from folder: one archive-like file directly,
// anything else as a stored zip. It stats every file first and returns
// ErrFilesMissing before writing if any is gone. onStart runs once the
// response starts at byte 0 of a GET.
func serve(w http.ResponseWriter, r *http.Request, folder string, it Item, onStart func()) error {
	if len(it.Files) == 0 {
		return ErrFilesMissing
	}
	files := make([]resolved, 0, len(it.Files))
	for _, f := range it.Files {
		p := filepath.Join(folder, filepath.FromSlash(f.Path))
		if !fsutil.Within(folder, p) {
			return fmt.Errorf("%w: %q", ErrUnsafePath, f.Path)
		}
		// Lstat: a symlink in the folder is never followed.
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			return ErrFilesMissing
		}
		files = append(files, resolved{rel: f.Path, path: p, info: fi})
	}
	if testHookAfterStat != nil {
		testHookAfterStat()
	}
	head := r.Method == http.MethodHead

	if len(files) == 1 && archiveExts[strings.ToLower(filepath.Ext(files[0].path))] {
		f, err := os.Open(files[0].path)
		if err != nil {
			return ErrFilesMissing
		}
		defer f.Close()
		name := filepath.Base(files[0].path)
		w.Header().Set("Content-Disposition", attachment(name))
		sw := &statusWriter{ResponseWriter: w}
		http.ServeContent(sw, r, name, files[0].info.ModTime(), f)
		if onStart != nil && !head && (sw.status == http.StatusOK || sw.status == http.StatusPartialContent) && rangeStartsAtZero(r.Header.Get("Range")) {
			onStart()
		}
		return nil
	}

	// Open the first file before anything is written, so a file that vanished
	// since the stat still gets a clean error response.
	var first *os.File
	if !head {
		var err error
		if first, err = os.Open(files[0].path); err != nil {
			return ErrFilesMissing
		}
		defer first.Close()
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachment(importer.SanitizeName(it.Title)+".zip"))
	if head {
		return nil
	}
	if onStart != nil {
		onStart()
	}
	ctx := r.Context()
	zw := zip.NewWriter(w)
	for i, f := range files {
		src := first
		if i > 0 {
			var err error
			if src, err = os.Open(f.path); err != nil {
				return fmt.Errorf("%w: %w", ErrAborted, err)
			}
		}
		err := addToZip(ctx, zw, f, src)
		if i > 0 {
			src.Close()
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrAborted, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrAborted, err)
	}
	return nil
}

func addToZip(ctx context.Context, zw *zip.Writer, f resolved, src io.Reader) error {
	hdr := &zip.FileHeader{
		Name:               path.Clean(filepath.ToSlash(f.rel)),
		Method:             zip.Store,
		Modified:           f.info.ModTime(),
		UncompressedSize64: uint64(f.info.Size()),
	}
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, fsutil.ContextReader(ctx, src))
	return err
}

// statusWriter remembers the status code ServeContent chose.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// rangeStartsAtZero reports whether a Range header is absent or begins at 0.
func rangeStartsAtZero(h string) bool {
	if h == "" {
		return true
	}
	spec, ok := strings.CutPrefix(h, "bytes=")
	if !ok {
		return true
	}
	first, _, _ := strings.Cut(spec, ",")
	return strings.HasPrefix(strings.TrimSpace(first), "0-")
}
