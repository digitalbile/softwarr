// Package library holds the items softwarr has imported and delivers them.
package library

import (
	"context"
	"database/sql"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/importer"
	"github.com/digitalbile/softwarr/internal/prowlarr"
)

// CreateItem records an imported release and its files and returns the new
// item's id. It makes several writes, so pass a q bound to a transaction to
// keep the item and its files together.
func CreateItem(ctx context.Context, q *dbq.Queries, requestID int64, displayTitle string, rel prowlarr.Release, res importer.Result, now time.Time) (int64, error) {
	id, err := q.InsertLibraryItem(ctx, dbq.InsertLibraryItemParams{
		RequestID:    sql.NullInt64{Int64: requestID, Valid: requestID != 0},
		DisplayTitle: displayTitle,
		ReleaseName:  rel.Title,
		ReleaseKey:   rel.Key(),
		Folder:       res.Folder,
		TotalSize:    res.TotalSize,
		Protocol:     rel.Protocol,
		Indexer:      rel.Indexer,
		ImportMode:   string(res.Mode),
		ImportedAt:   now.Unix(),
	})
	if err != nil {
		return 0, err
	}
	for _, f := range res.Files {
		if err := q.InsertLibraryFile(ctx, dbq.InsertLibraryFileParams{ItemID: id, RelPath: f.Path, Size: f.Size}); err != nil {
			return 0, err
		}
	}
	return id, nil
}
