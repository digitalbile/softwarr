package auth

import (
	"context"
	"testing"

	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUsers(t *testing.T) *Users {
	t.Helper()
	d, q := testutil.NewDB(t)
	return NewUsers(d, q)
}

func TestSetupCreatesOwnerOnce(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	need, err := u.NeedsSetup(ctx)
	require.NoError(t, err)
	assert.True(t, need)

	owner, err := u.Setup(ctx, "  alice ", "password1")
	require.NoError(t, err)
	assert.True(t, owner.IsOwner)
	assert.True(t, owner.IsAdmin)
	assert.Equal(t, "active", owner.Status)
	assert.Equal(t, "alice", owner.Username)
	assert.True(t, owner.HasPassword)
	assert.False(t, owner.HasOIDC)

	need, err = u.NeedsSetup(ctx)
	require.NoError(t, err)
	assert.False(t, need)

	_, err = u.Setup(ctx, "bob", "password1")
	assert.ErrorIs(t, err, ErrSetupDone)
}

func TestOwnerCannotBeDemotedDisabledOrDeleted(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	owner, err := u.Setup(ctx, "alice", "password1")
	require.NoError(t, err)

	f := false
	_, err = u.Update(ctx, owner.ID, UserPatch{IsAdmin: &f})
	assert.ErrorIs(t, err, ErrOwnerProtected)
	for _, s := range []string{"disabled", "pending"} {
		s := s
		_, err = u.Update(ctx, owner.ID, UserPatch{Status: &s})
		assert.ErrorIs(t, err, ErrOwnerProtected)
	}
	assert.ErrorIs(t, u.Delete(ctx, owner.ID), ErrOwnerProtected)

	// Harmless changes still work.
	tr, active, email := true, "active", "a@example.com"
	got, err := u.Update(ctx, owner.ID, UserPatch{IsAdmin: &tr, Status: &active, AutoApprove: &tr, Email: &email})
	require.NoError(t, err)
	assert.True(t, got.IsAdmin)
	assert.True(t, got.AutoApprove)
	assert.Equal(t, "a@example.com", got.Email)

	again, err := u.Get(ctx, owner.ID)
	require.NoError(t, err)
	assert.True(t, again.IsOwner)
	assert.Equal(t, "active", again.Status)
}

func TestUpdateAndDeleteNonOwner(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	_, err := u.Setup(ctx, "alice", "password1")
	require.NoError(t, err)
	bob, err := u.CreateLocal(ctx, "bob", "", "password1", false)
	require.NoError(t, err)

	f, dis, bad := false, "disabled", "bogus"
	got, err := u.Update(ctx, bob.ID, UserPatch{IsAdmin: &f, Status: &dis})
	require.NoError(t, err)
	assert.Equal(t, "disabled", got.Status)
	_, err = u.Update(ctx, bob.ID, UserPatch{Status: &bad})
	assert.Error(t, err)

	require.NoError(t, u.Delete(ctx, bob.ID))
	_, err = u.Get(ctx, bob.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	list, err := u.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestAuthenticateRejectsPendingAndDisabled(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	_, err := u.Setup(ctx, "alice", "password1")
	require.NoError(t, err)
	bob, err := u.CreateLocal(ctx, "bob", "", "password1", false)
	require.NoError(t, err)

	got, err := u.Authenticate(ctx, "BOB", "password1")
	require.NoError(t, err)
	assert.Equal(t, bob.ID, got.ID)

	_, err = u.Authenticate(ctx, "bob", "wrong")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	_, err = u.Authenticate(ctx, "nobody", "password1")
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	for _, s := range []string{"pending", "disabled"} {
		s := s
		_, err = u.Update(ctx, bob.ID, UserPatch{Status: &s})
		require.NoError(t, err)
		_, err = u.Authenticate(ctx, "bob", "password1")
		assert.ErrorIs(t, err, ErrAccountInactive)
		// Wrong password on an inactive account must not reveal its status.
		_, err = u.Authenticate(ctx, "bob", "wrong")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	}
}

func TestAuthenticateNoPasswordHash(t *testing.T) {
	d, q := testutil.NewDB(t)
	u := NewUsers(d, q)
	_, err := d.Exec(`INSERT INTO users (username, status, created_at) VALUES ('oidc', 'active', 1)`)
	require.NoError(t, err)
	_, err = u.Authenticate(context.Background(), "oidc", "")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	got, err := u.Get(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, got.HasPassword)
	assert.False(t, got.HasOIDC)

	_, err = d.Exec(`INSERT INTO user_identities (user_id, issuer, subject, created_at) VALUES (1, 'i', 's', 1)`)
	require.NoError(t, err)
	got, err = u.Get(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, got.HasOIDC)
}

func TestUsernamesCaseInsensitiveUnique(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	_, err := u.CreateLocal(ctx, "Alice", "a@example.com", "password1", false)
	require.NoError(t, err)
	_, err = u.CreateLocal(ctx, "alice", "", "password1", false)
	require.Error(t, err)
	assert.Equal(t, "That username is already taken.", err.Error())
	_, err = u.CreateLocal(ctx, "carol", "A@example.com", "password1", false)
	require.Error(t, err)
	assert.Equal(t, "That email is already in use.", err.Error())
	// Empty emails are stored as NULL, so several users can have none.
	_, err = u.CreateLocal(ctx, "d1", "", "password1", false)
	require.NoError(t, err)
	_, err = u.CreateLocal(ctx, "d2", "", "password1", false)
	require.NoError(t, err)
}

func TestUsernameValidation(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	_, err := u.CreateLocal(ctx, "   ", "", "password1", false)
	require.Error(t, err)
	assert.Equal(t, "A username is required.", err.Error())
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	_, err = u.CreateLocal(ctx, string(long), "", "password1", false)
	require.Error(t, err)
	assert.Equal(t, "Usernames can be at most 64 characters.", err.Error())
	_, err = u.CreateLocal(ctx, "ok", "", "short", false)
	require.Error(t, err)
	assert.Equal(t, "Passwords need at least 8 characters.", err.Error())
	_, err = u.Setup(ctx, "ok", "short")
	require.Error(t, err)
	assert.Equal(t, "Passwords need at least 8 characters.", err.Error())
}

func TestChangePasswordRequiresCurrent(t *testing.T) {
	d, q := testutil.NewDB(t)
	u := NewUsers(d, q)
	ctx := context.Background()
	bob, err := u.CreateLocal(ctx, "bob", "", "password1", false)
	require.NoError(t, err)
	_, err = d.Exec(`UPDATE users SET must_change_password = 1 WHERE id = ?`, bob.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, u.ChangePassword(ctx, bob.ID, "wrongwrong", "newpassword"), ErrInvalidCredentials)
	require.NoError(t, u.ChangePassword(ctx, bob.ID, "password1", "newpassword"))

	got, err := u.Authenticate(ctx, "bob", "newpassword")
	require.NoError(t, err)
	assert.False(t, got.MustChangePassword)
	_, err = u.Authenticate(ctx, "bob", "password1")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestChangePasswordMinLength(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	bob, err := u.CreateLocal(ctx, "bob", "", "password1", false)
	require.NoError(t, err)
	err = u.ChangePassword(ctx, bob.ID, "password1", "1234567")
	require.Error(t, err)
	assert.Equal(t, "Passwords need at least 8 characters.", err.Error())
}
