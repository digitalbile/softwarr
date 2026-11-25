package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/testutil"
)

func TestSessionsDeleteExpiredKeepsLiveOnes(t *testing.T) {
	d, q := testutil.NewDB(t)
	ctx := context.Background()
	u, err := NewUsers(d, q).CreateLocal(ctx, "sam", "", "correct horse", false)
	require.NoError(t, err)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	s := NewSessions(q, clock)
	old, _, err := s.Create(ctx, u.ID)
	require.NoError(t, err)
	now = now.Add(20 * 24 * time.Hour)
	live, _, err := s.Create(ctx, u.ID)
	require.NoError(t, err)
	now = now.Add(11 * 24 * time.Hour) // old is 31 days in, live is 11

	require.NoError(t, s.DeleteExpired(ctx))
	var n int
	require.NoError(t, d.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n))
	assert.Equal(t, 1, n)
	_, _, _, err = s.Lookup(ctx, live)
	assert.NoError(t, err)
	_, _, _, err = s.Lookup(ctx, old)
	assert.ErrorIs(t, err, ErrNoSession)
}

func TestSessionsCheckDoesNotSlide(t *testing.T) {
	d, q := testutil.NewDB(t)
	ctx := context.Background()
	u, err := NewUsers(d, q).CreateLocal(ctx, "sam", "", "correct horse", false)
	require.NoError(t, err)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewSessions(q, func() time.Time { return now })
	tok, expires, err := s.Create(ctx, u.ID)
	require.NoError(t, err)
	now = now.Add(20 * 24 * time.Hour) // past the point Lookup would slide

	id, err := s.Check(ctx, tok)
	require.NoError(t, err)
	assert.Equal(t, u.ID, id)
	var stored int64
	require.NoError(t, d.QueryRow(`SELECT expires_at FROM sessions`).Scan(&stored))
	assert.Equal(t, expires.Unix(), stored)

	now = now.Add(11 * 24 * time.Hour)
	_, err = s.Check(ctx, tok)
	assert.ErrorIs(t, err, ErrNoSession)
	_, err = s.Check(ctx, "nope")
	assert.ErrorIs(t, err, ErrNoSession)
}

func TestSessionsPurgeRunsAtStart(t *testing.T) {
	d, q := testutil.NewDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	u, err := NewUsers(d, q).CreateLocal(ctx, "sam", "", "correct horse", false)
	require.NoError(t, err)
	_, err = d.Exec(`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES ('h', ?, 1, 2)`, u.ID)
	require.NoError(t, err)
	s := NewSessions(q, time.Now)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.RunPurge(ctx, time.Hour)
	}()
	require.Eventually(t, func() bool {
		var n int
		require.NoError(t, d.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n))
		return n == 0
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	<-done
}
