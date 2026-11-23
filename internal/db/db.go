// Package db opens the SQLite database and applies migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the SQLite database at path with WAL, foreign keys, and a busy
// timeout. Transactions begin IMMEDIATE, taking the write lock up front, so a
// transaction that reads and then writes can't fail with BUSY_SNAPSHOT.
func Open(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Set("_txlock", "immediate")
	// SQLite reads a file: URI, so escape the path: a ? or # in it would
	// otherwise end the file name early.
	name := (&url.URL{Path: path}).EscapedPath()
	d, err := sql.Open("sqlite", "file:"+name+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return d, nil
}

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, d *sql.DB) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, d, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
