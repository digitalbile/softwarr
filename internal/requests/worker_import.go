package requests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/fsutil"
	"github.com/digitalbile/softwarr/internal/importer"
	"github.com/digitalbile/softwarr/internal/library"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/pathmap"
	"github.com/digitalbile/softwarr/internal/prowlarr"
)

// Importing: placing a finished download in the library.

func (w *Worker) importDownload(ctx context.Context, row dbq.Request) error {
	rel, err := releaseOf(row)
	if err != nil {
		return err
	}
	st, err := w.s.Get(ctx)
	if err != nil {
		return err
	}
	root := st.LibraryRoot
	if root == "" {
		return w.fail(ctx, row.ID, ActImportFailed, ErrNoLibraryRoot.Error(), "")
	}
	unmounted, err := library.RootUnmounted(ctx, w.q, root)
	if err != nil {
		return err
	}
	if unmounted {
		// Wait for the volume rather than fail, or import into a bare mount point.
		return w.waitForRoot(ctx, row, root)
	}
	c, err := w.clientByID(ctx, row.ClientID.Int64)
	if errors.Is(err, errClientGone) {
		return w.fail(ctx, row.ID, ActImportFailed, errClientGone.Error(), "")
	}
	if err != nil {
		return err
	}
	it, err := c.Get(ctx, row.ClientItemID)
	if err != nil {
		return err
	}
	if it.State == downloadclient.StateMissing {
		msg, detail := clientMessage(it)
		return w.fail(ctx, row.ID, ActImportFailed, msg, detail)
	}
	ms, err := pathmap.Load(ctx, w.q)
	if err != nil {
		return err
	}
	base := pathmap.Map(ms, c.Host(), it.SavePath)
	folder, err := w.importFolder(ctx, row, rel, root)
	if errors.Is(err, ErrConflict) {
		return nil // abandoned meanwhile
	}
	if err != nil {
		return w.fail(ctx, row.ID, ActImportFailed, msgImportFailed, fmt.Sprintf("The library folder for this import couldn't be created: %v", err))
	}
	res, err := importer.ImportInto(ctx, importer.Source{BaseDir: base, Files: it.Files, Protocol: rel.Protocol}, folder)
	if err != nil {
		// The importer removed the folder, or kept it on purpose because it
		// holds moved files. Either way a later attempt must not reuse it.
		w.setImportFolder(context.WithoutCancel(ctx), row.ID, sql.NullString{})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return w.fail(ctx, row.ID, ActImportFailed, msgImportFailed, fmt.Sprintf("Importing from %s failed: %v", base, err))
	}

	// Record even when shutting down: the files are in place, and an
	// unrecorded import has to be redone on the next start.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	req, itemID, err := w.record(rctx, row, rel, res)
	if err != nil {
		// The files are in the library but nothing points at them. The
		// request is still importing, and the next attempt redoes the import
		// in the same folder.
		slog.Error("imported files were not recorded; the import will be redone", "request", row.ID, "folder", res.Folder, "err", err)
		return skipChanged(err)
	}
	w.svc.publish(req)
	w.svc.emit(ctx, notify.Event{
		Type:      notify.EvItemAvailable,
		Message:   fmt.Sprintf("%s is ready to download.", req.DisplayTitle),
		RequestID: req.ID, ItemID: itemID, ToUserIDs: []int64{req.RequesterID},
		Payload: map[string]any{"requestId": req.ID, "itemId": itemID, "title": req.DisplayTitle},
	})
	w.svc.b.Publish(events.Message{Type: events.TypeLibraryUpdated, Data: map[string]any{"id": itemID}, All: true})
	return nil
}

const msgWaitingForLibrary = "Waiting for the library folder. An admin has the details."

// waitForRoot leaves row importing, to be tried again next tick, and says why
// on the request. It writes only when the message is new.
func (w *Worker) waitForRoot(ctx context.Context, row dbq.Request, root string) error {
	detail := fmt.Sprintf("The library folder %s looks unmounted: it is missing, or empty while the library has items in it. softwarr will import once it is back.", root)
	if row.Error == msgWaitingForLibrary && row.ErrorDetail.String == detail {
		return nil
	}
	slog.Warn("library folder looks unmounted; import waits", "request", row.ID, "root", root)
	n, err := w.q.SetRequestImportWaiting(ctx, dbq.SetRequestImportWaitingParams{
		Error: msgWaitingForLibrary, ErrorDetail: sql.NullString{String: detail, Valid: true},
		UpdatedAt: w.now().Unix(), ID: row.ID,
	})
	if err != nil || n == 0 {
		return err
	}
	if req, err := w.svc.load(ctx, row.ID); err == nil {
		w.svc.publish(req)
	}
	return nil
}

// importFolder returns the folder to import row into and saves it on the
// request before any file is placed. A request that already claimed a folder
// in the current root, in an attempt whose files were placed but never
// recorded, gets that exact folder back emptied, so a retry never makes a
// numbered copy. Otherwise a new folder is claimed in the current root.
func (w *Worker) importFolder(ctx context.Context, row dbq.Request, rel prowlarr.Release, root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	folder := ""
	if row.ImportFolder.Valid && row.ImportFolder.String != "" {
		prev := row.ImportFolder.String
		ok, err := w.reusable(ctx, absRoot, prev, rel)
		if err != nil {
			return "", err
		}
		if ok {
			if err := os.RemoveAll(prev); err != nil {
				return "", err
			}
			if err := os.Mkdir(prev, 0o755); err == nil {
				folder = prev
			}
		} else {
			slog.Info("leaving an earlier import folder alone and importing into a new one", "request", row.ID, "folder", prev)
		}
	}
	if folder == "" {
		if folder, err = importer.ClaimFolder(absRoot, rel.Title); err != nil {
			return "", err
		}
	}
	n, err := w.q.SetRequestImportFolder(ctx, dbq.SetRequestImportFolderParams{
		ImportFolder: sql.NullString{String: folder, Valid: true}, ID: row.ID,
	})
	if err == nil && n == 0 {
		err = ErrConflict
	}
	if err != nil {
		os.Remove(folder) // still empty
		return "", err
	}
	return folder, nil
}

// reusable reports whether the folder an earlier attempt saved can be emptied
// and imported into again. It must be a clean path directly in the current
// root, a plain folder or gone, and not used by any library item. A folder
// from an older root is never touched. Moved (Usenet) files may be the only
// copy, so their folder is never emptied.
func (w *Worker) reusable(ctx context.Context, root, folder string, rel prowlarr.Release) (bool, error) {
	if rel.Protocol == "usenet" || filepath.Clean(folder) != folder || filepath.Dir(folder) != root || !fsutil.Within(root, folder) {
		return false, nil
	}
	fi, err := os.Lstat(folder)
	switch {
	case err == nil && !fi.IsDir():
		return false, nil // a file or a symlink
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return false, err
	}
	used, err := w.q.ListAvailableLibraryFolders(ctx)
	if err != nil {
		return false, err
	}
	for _, f := range used {
		if filepath.Clean(f) == folder {
			return false, nil
		}
	}
	return true, nil
}

func (w *Worker) setImportFolder(ctx context.Context, id int64, folder sql.NullString) {
	if _, err := w.q.SetRequestImportFolder(ctx, dbq.SetRequestImportFolderParams{ImportFolder: folder, ID: id}); err != nil {
		slog.Error("save import folder", "request", id, "err", err)
	}
}

// record creates the library item and marks the request available in one
// transaction, so an item never exists without its request pointing at it.
func (w *Worker) record(ctx context.Context, row dbq.Request, rel prowlarr.Release, res importer.Result) (Request, int64, error) {
	tx, err := w.svc.db.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, 0, err
	}
	defer tx.Rollback()
	q := w.q.WithTx(tx)
	itemID, err := library.CreateItem(ctx, q, row.ID, row.DisplayTitle, rel, res, w.now())
	if err != nil {
		return Request{}, 0, err
	}
	req, err := w.svc.apply(ctx, q, row.ID, ActImported, func(u *dbq.UpdateRequestParams) {
		u.LibraryItemID = sql.NullInt64{Int64: itemID, Valid: true}
		u.Progress = 1
		u.Error = ""
	})
	if err != nil {
		return Request{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return Request{}, 0, err
	}
	return req, itemID, nil
}
