package auth

import (
	"context"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetOwnerPassword(t *testing.T) {
	d, q := testutil.NewDB(t)
	ctx := context.Background()
	u := NewUsers(d, q)
	s := NewSessions(q, time.Now)

	owner, err := u.Setup(ctx, "alice", "password1")
	require.NoError(t, err)
	other, err := u.CreateLocal(ctx, "bob", "", "password2", false)
	require.NoError(t, err)
	ownerTok, _, err := s.Create(ctx, owner.ID)
	require.NoError(t, err)
	otherTok, _, err := s.Create(ctx, other.ID)
	require.NoError(t, err)

	pw, err := ResetOwnerPassword(ctx, d, q)
	require.NoError(t, err)
	assert.Regexp(t, `^[A-Za-z0-9]{20}$`, pw)

	got, err := u.Authenticate(ctx, "alice", pw)
	require.NoError(t, err)
	assert.True(t, got.MustChangePassword)
	_, err = u.Authenticate(ctx, "alice", "password1")
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	_, _, _, err = s.Lookup(ctx, ownerTok)
	assert.ErrorIs(t, err, ErrNoSession)
	_, _, _, err = s.Lookup(ctx, otherTok)
	assert.NoError(t, err)
}

func TestResetWithoutOwner(t *testing.T) {
	d, q := testutil.NewDB(t)
	_, err := ResetOwnerPassword(context.Background(), d, q)
	assert.ErrorIs(t, err, ErrNoOwner)
}
