package logbuf

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogBufCapacity(t *testing.T) {
	h := New(3, nil)
	l := slog.New(h)
	for i := 1; i <= 5; i++ {
		l.Info(fmt.Sprintf("m%d", i))
	}
	es := h.Entries()
	require.Len(t, es, 3)
	assert.Equal(t, "m3", es[0].Message)
	assert.Equal(t, "m5", es[2].Message)
	assert.Equal(t, "INFO", es[0].Level)
}

func TestLogBufAttrsAndGroups(t *testing.T) {
	var out bytes.Buffer
	h := New(10, slog.NewTextHandler(&out, nil))
	l := slog.New(h).With("a", 1).WithGroup("g").With("b", 2)
	l.Warn("hi", "c", 3, slog.Group("sub", slog.String("d", "x")))
	es := h.Entries()
	require.Len(t, es, 1)
	assert.Equal(t, int64(1), es[0].Attrs["a"])
	assert.Equal(t, int64(2), es[0].Attrs["g.b"])
	assert.Equal(t, int64(3), es[0].Attrs["g.c"])
	assert.Equal(t, "x", es[0].Attrs["g.sub.d"])
	assert.Contains(t, out.String(), "msg=hi")
	assert.Contains(t, out.String(), "g.c=3")
}

func TestLogBufDoesNotShareAttrsBetweenDerivedHandlers(t *testing.T) {
	h := New(10, nil)
	slog.New(h).With("a", 1).Info("one")
	slog.New(h).Info("two")
	es := h.Entries()
	assert.Len(t, es[0].Attrs, 1)
	assert.Empty(t, es[1].Attrs)
}
