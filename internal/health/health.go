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

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/library"
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
	// httpTimeout is a little under checkTimeout so a client's own error usually wins.
	httpTimeout = 9 * time.Second
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
	timeout     time.Duration

	mu   sync.Mutex
	prev map[string]string // check id to level from the previous loop run; nil before the first
}

// NewChecker builds a Checker. n and b may be nil.
func NewChecker(q *dbq.Queries, s *settings.Store, newProwlarr func(url, key string) *prowlarr.Client, newClient func(downloadclient.Config) (downloadclient.Client, error), n *notify.Service, b *events.Broker) *Checker {
	if newProwlarr == nil {
		newProwlarr = func(url, key string) *prowlarr.Client {
			return prowlarr.New(url, key, &http.Client{Timeout: httpTimeout})
		}
	}
	if newClient == nil {
		newClient = func(cfg downloadclient.Config) (downloadclient.Client, error) {
			return downloadclient.New(cfg, &http.Client{Timeout: httpTimeout})
		}
	}
	return &Checker{q: q, s: s, newProwlarr: newProwlarr, newClient: newClient, n: n, b: b, timeout: checkTimeout}
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
	var tasks []task
	st, err := c.s.Get(ctx)
	if err != nil {
		return []Check{fail("settings", "Can't read the settings: %v", err)}
	}
	tasks = append(tasks,
		task{"prowlarr", "Can't reach Prowlarr: it didn't respond within %s.", func(ctx context.Context) []Check { return []Check{c.prowlarr(ctx, st)} }},
		task{"library", "The library folder check didn't respond within %s.", func(ctx context.Context) []Check { return []Check{c.library(ctx, st)} }},
		task{"hardlinks", "The import check didn't respond within %s.", func(ctx context.Context) []Check { return c.hardlinks(ctx) }},
		task{"oidc", "Can't reach the OIDC provider: it didn't respond within %s.", func(ctx context.Context) []Check { return c.oidc(ctx, st) }},
	)
	clients, err := c.q.ListDownloadClients(ctx)
	if err != nil {
		tasks = append(tasks, task{"downloadclients", "", func(context.Context) []Check {
			return []Check{fail("downloadclients", "Can't read the download clients: %v", err)}
		}})
	}
	enabled := false
	for _, row := range clients {
		if row.Enabled != 0 {
			enabled = true
			tasks = append(tasks, task{fmt.Sprintf("downloadclient:%d", row.ID), "Can't reach download client " + row.Name + ": it didn't respond within %s.",
				func(ctx context.Context) []Check { return []Check{c.downloadClient(ctx, row)} }})
		}
	}
	if err == nil {
		if enabled {
			tasks = append(tasks, task{"noclient", "", func(context.Context) []Check { return []Check{ok("noclient", "A download client is enabled.")} }})
		} else {
			tasks = append(tasks, task{"noclient", "", func(context.Context) []Check {
				return []Check{warn("noclient", "No download client is enabled, so requests can't be downloaded.")}
			}})
		}
	}
	tasks = append(tasks, task{"pathmappings", "The path mapping check didn't respond within %s.", func(ctx context.Context) []Check { return c.pathMappings(ctx) }})

	results := make([][]Check, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = withTimeout(ctx, c.timeout, task)
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

// task is one check to run. timeoutMsg is a format taking the timeout; it is
// the message reported under id when the task runs too long.
type task struct {
	id         string
	timeoutMsg string
	run        func(context.Context) []Check
}

// withTimeout runs t with a time limit. A task that ignores its context is
// abandoned and reported as an error under its own ID.
func withTimeout(ctx context.Context, limit time.Duration, t task) []Check {
	tctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	done := make(chan []Check, 1)
	go func() { done <- t.run(tctx) }()
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
		format := t.timeoutMsg
		if format == "" {
			format = t.id + " didn't respond within %s."
		}
		return []Check{{ID: t.id, Level: LevelError, Message: fmt.Sprintf(format, humanDuration(limit))}}
	}
}

func humanDuration(d time.Duration) string {
	if d%time.Second == 0 {
		n := int(d / time.Second)
		if n == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", n)
	}
	return d.String()
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

func (c *Checker) library(ctx context.Context, st settings.Settings) Check {
	root := st.LibraryRoot
	if root == "" {
		return warn("library", "No library folder is set.")
	}
	// Missing or empty while items are recorded in it: most likely the
	// volume isn't mounted, so imports wait and items are kept.
	if n, err := library.ItemsIn(ctx, c.q, root); err == nil && n > 0 {
		if unmounted, err := library.RootUnmounted(ctx, c.q, root); err == nil && unmounted {
			return warn("library", fmt.Sprintf("The library folder %s looks unmounted. It is missing or empty, but the library still has items in it. Imports wait until it is back.", root))
		}
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

// oidc runs the same checks as the Test button on the saved settings.
func (c *Checker) oidc(ctx context.Context, st settings.Settings) []Check {
	if !st.OIDC.Enabled {
		return nil
	}
	hc := &http.Client{Timeout: httpTimeout}
	err := auth.CheckOIDC(ctx, hc, st.ExternalURL, st.OIDC.Issuer, st.OIDC.ClientID)
	if err == nil {
		return []Check{ok("oidc", "The OIDC provider responded and matches the settings.")}
	}
	ch := fail("oidc", "SSO sign-in won't work: %v", err)
	if auth.LocalAuthOff(st) {
		// Added after fail, which would scrub the query string.
		ch.Message += " Until it's fixed, only the owner can sign in, at /login?local=1."
	}
	return []Check{ch}
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
// checks that were not failing on the previous run. The first run after
// startup only sets the baseline, so a restart doesn't repeat warnings for
// checks that were already failing. The health page shows them either way.
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
	if c.n == nil || prev == nil {
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
