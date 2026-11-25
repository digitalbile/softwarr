// Package pathmap translates paths reported by a download client into paths
// visible inside the softwarr container.
package pathmap

import (
	"context"
	"strings"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

// Mapping says that RemotePath on Host is LocalPath here.
type Mapping struct {
	ID         int64  `json:"id"`
	Host       string `json:"host"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
}

// Load reads every mapping from the database.
func Load(ctx context.Context, q *dbq.Queries) ([]Mapping, error) {
	rows, err := q.ListPathMappings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Mapping, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMapping(r))
	}
	return out, nil
}

// IsWindows reports whether p looks like a Windows path (drive letter or
// backslashes).
func IsWindows(p string) bool {
	if strings.Contains(p, `\`) {
		return true
	}
	return len(p) >= 2 && p[1] == ':' && (p[0]|0x20) >= 'a' && (p[0]|0x20) <= 'z'
}

// Map returns the local path for a remote path reported by host, using the
// longest matching RemotePath prefix. It matches whole path segments only.
// Windows-style remote paths (either separator) match case-insensitively;
// POSIX ones match case-sensitively. With no match, remote is returned as is.
func Map(ms []Mapping, host, remote string) string {
	best := -1
	var bestRest, bestLocal string
	for _, m := range ms {
		if !strings.EqualFold(m.Host, host) {
			continue
		}
		win := IsWindows(m.RemotePath)
		prefix := strings.TrimRight(norm(m.RemotePath, win), "/")
		r := norm(remote, win)
		var rest string
		switch {
		case foldEq(r, prefix, win):
			rest = ""
		case len(r) > len(prefix) && r[len(prefix)] == '/' && foldEq(r[:len(prefix)], prefix, win):
			rest = r[len(prefix)+1:]
		default:
			continue
		}
		if len(prefix) > best {
			best, bestRest, bestLocal = len(prefix), rest, m.LocalPath
		}
	}
	if best < 0 {
		return remote
	}
	local := strings.TrimRight(bestLocal, "/")
	if bestRest == "" {
		if local == "" {
			return "/"
		}
		return local
	}
	return local + "/" + bestRest
}

func norm(p string, win bool) string {
	if win {
		return strings.ReplaceAll(p, `\`, "/")
	}
	return p
}

func foldEq(a, b string, fold bool) bool {
	if fold {
		return strings.EqualFold(a, b)
	}
	return a == b
}
