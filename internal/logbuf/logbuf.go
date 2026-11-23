// Package logbuf keeps recent log records in memory for the system logs page.
package logbuf

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Entry is one captured log record.
type Entry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

type buffer struct {
	mu      sync.Mutex
	entries []Entry // ring storage
	start   int
	n       int
}

func (b *buffer) add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.n < len(b.entries) {
		b.entries[(b.start+b.n)%len(b.entries)] = e
		b.n++
		return
	}
	b.entries[b.start] = e
	b.start = (b.start + 1) % len(b.entries)
}

// Handler is a slog.Handler that records entries and forwards them to next.
type Handler struct {
	buf    *buffer
	next   slog.Handler
	attrs  map[string]any
	groups []string
}

// New returns a handler holding the last capacity records. next may be nil.
func New(capacity int, next slog.Handler) *Handler {
	if capacity < 1 {
		capacity = 1
	}
	return &Handler{buf: &buffer{entries: make([]Entry, capacity)}, next: next}
}

// Entries returns the buffered records, oldest first.
func (h *Handler) Entries() []Entry {
	h.buf.mu.Lock()
	defer h.buf.mu.Unlock()
	out := make([]Entry, 0, h.buf.n)
	for i := 0; i < h.buf.n; i++ {
		out = append(out, h.buf.entries[(h.buf.start+i)%len(h.buf.entries)])
	}
	return out
}

func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	if h.next == nil {
		return true
	}
	return h.next.Enabled(ctx, l)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	e := Entry{Time: r.Time, Level: r.Level.String(), Message: r.Message}
	if len(h.attrs) > 0 || r.NumAttrs() > 0 {
		e.Attrs = make(map[string]any, len(h.attrs)+r.NumAttrs())
		for k, v := range h.attrs {
			e.Attrs[k] = v
		}
		r.Attrs(func(a slog.Attr) bool {
			addAttr(e.Attrs, h.groups, a)
			return true
		})
	}
	h.buf.add(e)
	if h.next != nil {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = make(map[string]any, len(h.attrs)+len(attrs))
	for k, v := range h.attrs {
		c.attrs[k] = v
	}
	for _, a := range attrs {
		addAttr(c.attrs, h.groups, a)
	}
	if h.next != nil {
		c.next = h.next.WithAttrs(attrs)
	}
	return &c
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.groups = append(append([]string(nil), h.groups...), name)
	if h.next != nil {
		c.next = h.next.WithGroup(name)
	}
	return &c
}

func addAttr(dst map[string]any, groups []string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		sub := groups
		if a.Key != "" {
			sub = append(append([]string(nil), groups...), a.Key)
		}
		for _, ga := range a.Value.Group() {
			addAttr(dst, sub, ga)
		}
		return
	}
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	v := a.Value.Any()
	if err, ok := v.(error); ok {
		v = err.Error()
	}
	dst[key] = v
}
