package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/fsutil"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/settings"
)

var (
	ErrNotFound         = errors.New("That library item doesn't exist.")
	ErrInvalidRetention = errors.New("Retention days can't be negative.")
	// ErrUnsafePath is returned when a stored folder or file path resolves
	// outside the place it must live in. Nothing is read or removed.
	ErrUnsafePath = errors.New("library: path is outside the library")
	// ErrFolderMissing means the item's folder isn't on disk and neither is
	// anything else in the library root, most likely because the library
	// volume isn't mounted. The item is left as it is.
	ErrFolderMissing = errors.New("The library folder for this item is missing. Check that the library path is mounted.")
)

// File is one file of an item, relative to the item's folder.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Item is an imported release available for download.
type Item struct {
	ID               int64      `json:"id"`
	Title            string     `json:"title"`
	ReleaseName      string     `json:"releaseName"`
	TotalSize        int64      `json:"totalSize"`
	Protocol         string     `json:"protocol"`
	Indexer          string     `json:"indexer"`
	Requester        string     `json:"requester"`
	Comment          string     `json:"comment"`
	Approver         string     `json:"approver"`
	ImportMode       string     `json:"importMode"`
	RetentionDays    *int       `json:"retentionDays"`
	ImportedAt       time.Time  `json:"importedAt"`
	LastDownloadedAt *time.Time `json:"lastDownloadedAt"`
	DownloadCount    int        `json:"downloadCount"`
	Files            []File     `json:"files"`
	ExpiresAt        *time.Time `json:"expiresAt"`

	// Folder and RequesterID are for the server only.
	Folder      string `json:"-"`
	RequesterID int64  `json:"-"`
}

// Service reads and changes library items and applies retention.
type Service struct {
	q   *dbq.Queries
	set *settings.Store
	n   *notify.Service
	b   *events.Broker
	now func() time.Time
}

func NewService(q *dbq.Queries, s *settings.Store, n *notify.Service, b *events.Broker, now func() time.Time) *Service {
	return &Service{q: q, set: s, n: n, b: b, now: now}
}

func toItem(r dbq.ListAvailableLibraryItemsRow, files []File, defaultDays int) Item {
	it := Item{
		ID: r.ID, Title: r.DisplayTitle, ReleaseName: r.ReleaseName, TotalSize: r.TotalSize,
		Protocol: r.Protocol, Indexer: r.Indexer, Requester: r.Requester, Comment: r.Comment,
		Approver: r.Approver, ImportMode: r.ImportMode, DownloadCount: int(r.DownloadCount),
		ImportedAt: time.Unix(r.ImportedAt, 0), Files: files, Folder: r.Folder, RequesterID: r.RequesterID,
	}
	if it.Files == nil {
		it.Files = []File{}
	}
	if r.RetentionDays.Valid {
		d := int(r.RetentionDays.Int64)
		it.RetentionDays = &d
	}
	if r.LastDownloadedAt.Valid {
		t := time.Unix(r.LastDownloadedAt.Int64, 0)
		it.LastDownloadedAt = &t
	}
	it.ExpiresAt = ExpiresAt(it, defaultDays)
	return it
}

// ExpiresAt returns when the item expires, or nil when it never does. The
// clock starts at the later of the import and the last download.
func ExpiresAt(it Item, defaultDays int) *time.Time {
	days := defaultDays
	if it.RetentionDays != nil {
		days = *it.RetentionDays
	}
	if days <= 0 {
		return nil
	}
	ref := it.ImportedAt
	if it.LastDownloadedAt != nil && it.LastDownloadedAt.After(ref) {
		ref = *it.LastDownloadedAt
	}
	t := ref.Add(time.Duration(days) * 24 * time.Hour)
	return &t
}

// List returns available items, newest first. A non-empty filter keeps items
// whose title or release name contains it, ignoring case.
func (s *Service) List(ctx context.Context, filter string) ([]Item, error) {
	set, err := s.set.Get(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListAvailableLibraryItems(ctx)
	if err != nil {
		return nil, err
	}
	fileRows, err := s.q.ListAllAvailableLibraryFiles(ctx)
	if err != nil {
		return nil, err
	}
	files := map[int64][]File{}
	for _, f := range fileRows {
		files[f.ItemID] = append(files[f.ItemID], File{Path: f.RelPath, Size: f.Size})
	}
	needle := strings.ToLower(strings.TrimSpace(filter))
	items := []Item{}
	for _, r := range rows {
		if needle != "" && !strings.Contains(strings.ToLower(r.DisplayTitle), needle) &&
			!strings.Contains(strings.ToLower(r.ReleaseName), needle) {
			continue
		}
		items = append(items, toItem(r, files[r.ID], set.DefaultRetentionDays))
	}
	return items, nil
}

// Get returns one available item.
func (s *Service) Get(ctx context.Context, id int64) (Item, error) {
	set, err := s.set.Get(ctx)
	if err != nil {
		return Item{}, err
	}
	row, err := s.q.GetAvailableLibraryItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	fr, err := s.q.ListLibraryFiles(ctx, id)
	if err != nil {
		return Item{}, err
	}
	files := make([]File, 0, len(fr))
	for _, f := range fr {
		files = append(files, File{Path: f.RelPath, Size: f.Size})
	}
	return toItem(dbq.ListAvailableLibraryItemsRow(row), files, set.DefaultRetentionDays), nil
}

// Update changes an item's title and retention. A nil title leaves it alone,
// and an empty one restores the release name. A nil retentionDays leaves it
// alone, and a pointer to nil means use the default.
func (s *Service) Update(ctx context.Context, id int64, title *string, retentionDays **int) (Item, error) {
	it, err := s.Get(ctx, id)
	if err != nil {
		return Item{}, err
	}
	newTitle := it.Title
	if title != nil {
		newTitle = strings.TrimSpace(*title)
		if newTitle == "" {
			newTitle = it.ReleaseName
		}
	}
	days := it.RetentionDays
	if retentionDays != nil {
		days = *retentionDays
		if days != nil && *days < 0 {
			return Item{}, ErrInvalidRetention
		}
	}
	var nd sql.NullInt64
	if days != nil {
		nd = sql.NullInt64{Int64: int64(*days), Valid: true}
	}
	if err := s.q.UpdateLibraryItemMeta(ctx, dbq.UpdateLibraryItemMetaParams{DisplayTitle: newTitle, RetentionDays: nd, ID: id}); err != nil {
		return Item{}, err
	}
	s.publish(id)
	return s.Get(ctx, id)
}

// Delete removes the item's library folder and marks it deleted. It never
// touches the download client's files.
func (s *Service) Delete(ctx context.Context, id int64) error {
	it, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.removeFolder(ctx, it); err != nil {
		return err
	}
	n, err := s.q.SetLibraryItemStatus(ctx, dbq.SetLibraryItemStatusParams{Status: "deleted", ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	s.publish(id)
	return nil
}

func (s *Service) publish(id int64) {
	s.b.Publish(events.Message{Type: events.TypeLibraryUpdated, Data: map[string]any{"id": id}, All: true})
}

// itemFolder returns the current library root and where the item's folder is
// in it. The importer always creates root/<name>, so the folder is found by
// its name in the current root: an admin can move the root along with its
// contents and change the setting.
func (s *Service) itemFolder(ctx context.Context, it Item) (root, folder string, err error) {
	set, err := s.set.Get(ctx)
	if err != nil {
		return "", "", err
	}
	root = set.LibraryRoot
	name := filepath.Base(it.Folder)
	if it.Folder == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "", "", fmt.Errorf("%w: %q", ErrUnsafePath, it.Folder)
	}
	folder = filepath.Join(root, name)
	if err := folderInRoot(root, folder); err != nil {
		return "", "", err
	}
	return root, folder, nil
}

// folderInRoot verifies folder is a direct child of root and not a symlink.
func folderInRoot(root, folder string) error {
	if root == "" || folder == "" {
		return fmt.Errorf("%w: %q", ErrUnsafePath, folder)
	}
	if !fsutil.Within(root, folder) {
		return fmt.Errorf("%w: %q", ErrUnsafePath, folder)
	}
	// The importer always creates root/<name>, so the folder must be a direct
	// child of the root, and must not be a symlink that leads elsewhere.
	absRoot, err1 := filepath.Abs(root)
	absFolder, err2 := filepath.Abs(folder)
	if err1 != nil || err2 != nil || filepath.Dir(absFolder) != absRoot {
		return fmt.Errorf("%w: %q", ErrUnsafePath, folder)
	}
	if fi, err := os.Lstat(absFolder); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %q is a symlink", ErrUnsafePath, folder)
	}
	return nil
}

// removeFolder deletes an item's library folder. A missing folder only counts
// as removed when the library root looks mounted: os.RemoveAll would report
// nothing wrong either way, and an unmounted volume still holds the files.
func (s *Service) removeFolder(ctx context.Context, it Item) error {
	root, folder, err := s.itemFolder(ctx, it)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(folder); errors.Is(err, os.ErrNotExist) {
		if rootMounted(root) {
			return nil // someone removed it by hand; the item can go
		}
		return ErrFolderMissing
	} else if err != nil {
		return err
	}
	// After the root moves, a folder of the same name could be something
	// else. Only remove it if it still holds one of the item's files.
	if folder != filepath.Clean(it.Folder) && !holdsAnyFile(folder, it.Files) {
		return fmt.Errorf("%w: %q doesn't hold this item's files", ErrUnsafePath, folder)
	}
	return os.RemoveAll(folder)
}

// rootMounted reports whether root is a readable directory with something in
// it. An empty root counts as unmounted: Docker creates an empty directory for
// a bind mount whose host path is missing.
func rootMounted(root string) bool {
	f, err := os.Open(root)
	if err != nil {
		return false
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	return err == nil && len(names) > 0
}

func holdsAnyFile(folder string, files []File) bool {
	for _, f := range files {
		p := filepath.Join(folder, filepath.FromSlash(f.Path))
		if !fsutil.Within(folder, p) {
			continue
		}
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// ApplyRetention expires items past their retention and returns how many.
func (s *Service) ApplyRetention(ctx context.Context) (int, error) {
	items, err := s.List(ctx, "")
	if err != nil {
		return 0, err
	}
	now := s.now()
	expired := 0
	for _, it := range items {
		if it.ExpiresAt == nil || !now.After(*it.ExpiresAt) {
			continue
		}
		if err := s.removeFolder(ctx, it); err != nil {
			slog.Error("library: expire item", "id", it.ID, "err", err)
			continue
		}
		n, err := s.q.SetLibraryItemStatus(ctx, dbq.SetLibraryItemStatusParams{Status: "expired", ID: it.ID})
		if err != nil {
			slog.Error("library: mark item expired", "id", it.ID, "err", err)
			continue
		}
		if n == 0 {
			continue
		}
		expired++
		var to []int64
		if it.RequesterID != 0 {
			to = []int64{it.RequesterID}
		}
		s.n.Emit(context.WithoutCancel(ctx), notify.Event{
			Type:      notify.EvItemExpired,
			Message:   fmt.Sprintf("%s was removed from the library because it passed its retention period.", it.Title),
			ItemID:    it.ID,
			ToUserIDs: to,
			ToAdmins:  true,
			Payload:   map[string]any{"itemId": it.ID, "title": it.Title},
		})
		s.publish(it.ID)
	}
	return expired, nil
}

func nullTime(unix int64) sql.NullInt64 { return sql.NullInt64{Int64: unix, Valid: true} }
