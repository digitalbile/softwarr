// Package fsutil holds small filesystem helpers shared by the importer and
// the library.
package fsutil

import (
	"context"
	"io"
	"path/filepath"
	"strings"
)

// ContextReader returns a reader that fails with ctx's error once ctx is
// done, so a long copy stops between reads.
func ContextReader(ctx context.Context, r io.Reader) io.Reader {
	return &ctxReader{ctx: ctx, r: r}
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Within reports whether target is strictly inside base. base itself is not
// inside base. Both are compared as cleaned absolute paths.
func Within(base, target string) bool {
	rel, ok := relative(base, target)
	return ok && rel != "."
}

// WithinOrEqual reports whether target is base or inside it.
func WithinOrEqual(base, target string) bool {
	_, ok := relative(base, target)
	return ok
}

// relative returns target relative to base, and whether it stays inside base.
func relative(base, target string) (string, bool) {
	b, err := filepath.Abs(base)
	if err != nil {
		return "", false
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(b, t)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}
