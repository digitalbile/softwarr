// Package testutil holds shared test helpers.
package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/digitalbile/softwarr/internal/db"
	"github.com/digitalbile/softwarr/internal/db/dbq"
)

// NewDB returns a migrated SQLite database in a temp dir, closed on cleanup.
func NewDB(t *testing.T) (*sql.DB, *dbq.Queries) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d, dbq.New(d)
}
