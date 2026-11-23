// Package health runs softwarr's self checks: external services, folders and
// import behavior. Results feed the system health page and admin warnings.
package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/pathmap"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
)

const (
	LevelOK      = "ok"
	LevelWarning = "warning"
	LevelError   = "error"

	checkTimeout = 10 * time.Second
)

// Check is the result of one health check.
type Check struct {
	ID      string `json:"id"`
	Level   string `json:"level"` // "ok" | "warning" | "error"
	Message string `json:"message"`
}

type Checker struct {
	q           *dbq.Queries
	s           *settings.Store
	newProwlarr func(url, key string) *prowlarr.Client
	newClient   func(downloadclient.Config) (downloadclient.Client, error)
	n           *notify.Service
	b           *events.Broker

	mu   sync.Mutex
	prev map[string]string // check id to level from the previous loop run
}

// NewChecker builds a Checker. n and b may be nil.
func NewChecker(q *dbq.Queries, s *settings.Store, newProwlarr func(url, key string) *prowlarr.Client, newClient func(downloadclient.Config) (downloadclient.Client, error), n *notify.Service, b *events.Broker) *Checker {
	if newProwlarr == nil {
		newProwlarr = func(url, key string) *prowlarr.Client {
			return prowlarr.New(url, key, &http.Client{Timeout: checkTimeout})
		}
	}
	if newClient == nil {
		newClient = func(cfg downloadclient.Config) (downloadclient.Client, error) {
			return downloadclient.New(cfg, &http.Client{Timeout: checkTimeout})
		}
	}
	return &Checker{q: q, s: s, newProwlarr: newProwlarr, newClient: newClient, n: n, b: b}
}

var queryRe = regexp.MustCompile(`\?[^\s"'<>)]*`)

// scrub removes URL query strings, which can carry API keys, from error text.
func scrub(s string) string { return queryRe.ReplaceAllString(s, "") }

func ok(id, msg string) Check   { return Check{ID: id, Level: LevelOK, Message: msg} }
func warn(id, msg string) Check { return Check{ID: id, Level: LevelWarning, Message: msg} }
func fail(id, format string, a ...any) Check {
	return Check{ID: id, Level: LevelError, Message: scrub(fmt.Sprintf(format, a...))}
}

// Run executes every check concurrently, each with its own 10 second limit,
// and returns the results ordered by ID.
func (c *Checker) Run(ctx context.Context) []Check {
	var tasks []func(context.Context) []Check
	st, err := c.s.Get(ctx)
	if err != nil {
		return []Check{fail("settings", "Can't read the settings: %v", err)}
	}
	tasks = append(tasks,
		func(ctx context.Context) []Check { return []Check{c.prowlarr(ctx, st)} },
		func(ctx context.Context) []Check { return []Check{c.library(st)} },
		func(ctx context.Context) []Check { return c.hardlinks(ctx) },
		func(ctx context.Context) []Check { return c.oidc(ctx, st) },
	)
	clients, err := c.q.ListDownloadClients(ctx)
	if err != nil {
		tasks = append(tasks, func(context.Context) []Check {
			return []Check{fail("downloadclients", "Can't read the download clients: %v", err)}
		})
	}
	enabled := false
	for _, row := range clients {
		if row.Enabled != 0 {
			enabled = true
			tasks = append(tasks, func(ctx context.Context) []Check { return []Check{c.downloadClient(ctx, row)} })
		}
	}
	if err == nil {
		if enabled {
			tasks = append(tasks, func(context.Context) []Check { return []Check{ok("noclient", "A download client is enabled.")} })
		} else {
			tasks = append(tasks, func(context.Context) []Check {
				return []Check{warn("noclient", "No download client is enabled, so requests can't be downloaded.")}
			})
		}
	}
	tasks = append(tasks, func(ctx context.Context) []Check { return c.pathMappings(ctx) })

	results := make([][]Check, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = withTimeout(ctx, task)
		}()
	}
	wg.Wait()
	var out []Check
	for _, r := range results {
		out = append(out, r...)
	}
	slices.SortFunc(out, func(a, b Check) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	return out
}

// withTimeout runs task with a 10 second limit. A task that ignores its
// context is abandoned and reported as timed out.
func withTimeout(ctx context.Context, task func(context.Context) []Check) []Check {
	tctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	done := make(chan []Check, 1)
	go func() { done <- task(tctx) }()
	select {
	case r := <-done:
		return r
	case <-tctx.Done():
		// The task may have finished at the same moment.
		select {
		case r := <-done:
			return r
		default:
		}
		return []Check{fail("timeout", "A health check took too long and was stopped.")}
	}
}

func (c *Checker) prowlarr(ctx context.Context, st settings.Settings) Check {
	if st.Prowlarr.URL == "" {
		return warn("prowlarr", "Prowlarr isn't set up.")
	}
	if err := c.newProwlarr(st.Prowlarr.URL, st.Prowlarr.APIKey).Ping(ctx); err != nil {
		return fail("prowlarr", "Can't reach Prowlarr: %v", err)
	}
	return ok("prowlarr", "Prowlarr is reachable.")
}

func (c *Checker) downloadClient(ctx context.Context, row dbq.DownloadClient) Check {
	id := fmt.Sprintf("downloadclient:%d", row.ID)
	cl, err := c.newClient(downloadclient.ConfigFromRow(row))
	if err == nil {
		err = cl.Test(ctx)
	}
	if err != nil {
		return fail(id, "Can't reach download client %s: %v", row.Name, err)
	}
	return ok(id, fmt.Sprintf("Download client %s is reachable.", row.Name))
}

func (c *Checker) library(st settings.Settings) Check {
	root := st.LibraryRoot
	if root == "" {
		return warn("library", "No library folder is set.")
	}
	f, err := os.CreateTemp(root, ".softwarr-health-*")
	if err != nil {
		return fail("library", "The library folder %s doesn't exist or isn't writable.", root)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return ok("library", "The library folder is writable.")
}

func (c *Checker) pathMappings(ctx context.Context) []Check {
	ms, err := pathmap.Load(ctx, c.q)
	if err != nil {
		return []Check{fail("pathmappings", "Can't read the path mappings: %v", err)}
	}
	var out []Check
	for _, m := range ms {
		id := fmt.Sprintf("pathmapping:%d", m.ID)
		if _, err := os.ReadDir(m.LocalPath); err != nil {
			out = append(out, fail(id, "softwarr can't read %s (mapped from %s).", m.LocalPath, m.RemotePath))
			continue
		}
		out = append(out, ok(id, fmt.Sprintf("softwarr can read %s.", m.LocalPath)))
	}
	return out
}

func (c *Checker) hardlinks(ctx context.Context) []Check {
	mode, err := c.q.LatestTorrentImportMode(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return []Check{fail("hardlinks", "Can't read the last import: %v", err)}
	}
	if mode == "copy" {
		return []Check{warn("hardlinks", "The last import was copied instead of hardlinked, which uses extra disk space. Put the download and library folders on the same filesystem to fix this.")}
	}
	return []Check{ok("hardlinks", "The last torrent import used a hardlink.")}
}

func (c *Checker) oidc(ctx context.Context, st settings.Settings) []Check {
	if !st.OIDC.Enabled || st.OIDC.Issuer == "" {
		return nil
	}
	hc := &http.Client{Timeout: checkTimeout}
	if _, err := oidc.NewProvider(oidc.ClientContext(ctx, hc), st.OIDC.Issuer); err != nil {
		return []Check{fail("oidc", "Can't reach the OIDC provider: %v", err)}
	}
	return []Check{ok("oidc", "The OIDC provider is reachable.")}
}

// Loop runs the checks now and then every interval until ctx ends.
func (c *Checker) Loop(ctx context.Context, every time.Duration) {
	c.runOnce(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.runOnce(ctx)
		}
	}
}

// runOnce runs the checks, publishes the full list, and notifies admins about
// checks that were not failing on the previous run.
func (c *Checker) runOnce(ctx context.Context) {
	checks := c.Run(ctx)
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	prev := c.prev
	c.prev = make(map[string]string, len(checks))
	for _, ch := range checks {
		c.prev[ch.ID] = ch.Level
	}
	c.mu.Unlock()

	if c.b != nil {
		c.b.Publish(events.Message{Type: events.TypeHealth, Data: checks, Admins: true})
	}
	if c.n == nil {
		return
	}
	for _, ch := range checks {
		if ch.Level == LevelOK {
			continue
		}
		if l, seen := prev[ch.ID]; seen && l != LevelOK {
			continue
		}
		slog.Warn("health check failing", "check", ch.ID, "level", ch.Level)
		c.n.Emit(ctx, notify.Event{
			Type: notify.EvHealthWarning, Message: ch.Message, ToAdmins: true,
			Payload: map[string]any{"check": ch.ID, "level": ch.Level},
		})
	}
}
