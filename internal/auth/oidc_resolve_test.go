package auth

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIssuer = "https://idp.example.com"

func oidcCfg() settings.OIDC {
	return settings.OIDC{Enabled: true, Issuer: testIssuer, AutoProvision: true}
}

func claimsFor(sub string) Claims {
	return Claims{Issuer: testIssuer, Subject: sub, Raw: map[string]any{}}
}

func countIdentities(t *testing.T, u *Users) int {
	t.Helper()
	var n int
	require.NoError(t, u.db.QueryRow("SELECT COUNT(*) FROM user_identities").Scan(&n))
	return n
}

func setStatus(t *testing.T, u *Users, id int64, status string) {
	t.Helper()
	_, err := u.Update(context.Background(), id, UserPatch{Status: &status})
	require.NoError(t, err)
}

func TestResolveKnownIdentityLogsIn(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	c := claimsFor("sub-known")
	c.PreferredUsername = "jane"

	id, out, err := ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	require.NoError(t, err)
	require.Equal(t, OutcomeCreated, out)

	// A changed email or username at the provider doesn't matter: (issuer, sub) is the key.
	c.Email = "someone-else@example.com"
	c.PreferredUsername = "other"
	again, out, err := ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeLogin, out)
	assert.Equal(t, id, again)
	assert.Equal(t, 1, countIdentities(t, u))

	// Same subject from a different issuer is a different identity.
	other := c
	other.Issuer = "https://other-idp.example.com"
	otherID, out, err := ResolveLogin(ctx, u.q, oidcCfg(), other, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeCreated, out)
	assert.NotEqual(t, id, otherID)

	// Known identities must belong to an active account.
	setStatus(t, u, id, "disabled")
	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	assert.ErrorIs(t, err, ErrAccountInactive)
	setStatus(t, u, id, "pending")
	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	assert.ErrorIs(t, err, ErrAwaitingApproval)
}

func TestResolveNewIdentityAutoProvisions(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	c := claimsFor("sub-new")
	c.Email = "jane@example.com"
	c.EmailVerified = true
	c.PreferredUsername = "jane"

	id, out, err := ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeCreated, out)

	user, err := u.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "jane", user.Username)
	assert.Equal(t, "jane@example.com", user.Email)
	assert.Equal(t, "active", user.Status)
	assert.False(t, user.HasPassword)
	assert.True(t, user.HasOIDC)
	assert.False(t, user.IsAdmin)
	assert.False(t, user.IsOwner)
}

func TestResolveNewIdentityDropsUnverifiedEmail(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	for _, auto := range []bool{true, false} {
		cfg := oidcCfg()
		cfg.AutoProvision = auto
		c := claimsFor(fmt.Sprintf("sub-unverified-%v", auto))
		c.Email = fmt.Sprintf("victim-%v@example.com", auto)
		c.PreferredUsername = fmt.Sprintf("mallory-%v", auto)

		id, _, err := ResolveLogin(ctx, u.q, cfg, c, nil)
		if !auto {
			require.ErrorIs(t, err, ErrAwaitingApproval)
		} else {
			require.NoError(t, err)
		}
		user, err := u.Get(ctx, id)
		require.NoError(t, err)
		assert.Empty(t, user.Email, "an unverified email is not stored")
		var email sql.NullString
		require.NoError(t, u.db.QueryRow("SELECT email FROM users WHERE id = ?", id).Scan(&email))
		assert.False(t, email.Valid)
	}
}

func TestResolveNewIdentityPendingWhenProvisioningOff(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	cfg := oidcCfg()
	cfg.AutoProvision = false
	c := claimsFor("sub-pending")
	c.PreferredUsername = "pat"

	id, out, err := ResolveLogin(ctx, u.q, cfg, c, nil)
	assert.ErrorIs(t, err, ErrAwaitingApproval)
	assert.Equal(t, OutcomePending, out)
	require.NotZero(t, id)

	user, err := u.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "pending", user.Status)
	assert.True(t, user.HasOIDC)

	// Signing in again before approval is still refused, without a second account.
	again, _, err := ResolveLogin(ctx, u.q, cfg, c, nil)
	assert.ErrorIs(t, err, ErrAwaitingApproval)
	assert.Equal(t, id, again)
	users, err := u.List(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 1)

	// Once approved, the identity signs in.
	setStatus(t, u, id, "active")
	_, out, err = ResolveLogin(ctx, u.q, cfg, c, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeLogin, out)
}

func TestResolveEmailTakenRejected(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	_, err := u.CreateLocal(ctx, "jane", "jane@example.com", "password1", false)
	require.NoError(t, err)

	for _, tc := range []struct {
		name     string
		cfg      func(*settings.OIDC)
		verified bool
	}{
		{"link off, verified", func(*settings.OIDC) {}, true},
		{"link off, unverified", func(*settings.OIDC) {}, false},
		{"provisioning off", func(c *settings.OIDC) { c.AutoProvision = false }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := oidcCfg()
			tc.cfg(&cfg)
			c := claimsFor("sub-attacker")
			c.Email = "JANE@example.com" // emails match without regard to case
			c.EmailVerified = tc.verified
			_, _, err := ResolveLogin(ctx, u.q, cfg, c, nil)
			assert.ErrorIs(t, err, ErrEmailTaken)
			assert.Equal(t, 0, countIdentities(t, u))
		})
	}
	users, err := u.List(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 1)
}

func TestResolveVerifiedEmailLinksWhenEnabled(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	local, err := u.CreateLocal(ctx, "jane", "jane@example.com", "password1", false)
	require.NoError(t, err)
	cfg := oidcCfg()
	cfg.LinkVerifiedEmail = true
	c := claimsFor("sub-jane")
	c.Email = "jane@example.com"
	c.EmailVerified = true

	id, out, err := ResolveLogin(ctx, u.q, cfg, c, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeLogin, out)
	assert.Equal(t, local.ID, id)
	user, err := u.Get(ctx, id)
	require.NoError(t, err)
	assert.True(t, user.HasOIDC)

	// From now on the identity itself signs in.
	c.Email = ""
	again, out, err := ResolveLogin(ctx, u.q, cfg, c, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeLogin, out)
	assert.Equal(t, local.ID, again)
}

func TestResolveVerifiedEmailLinkRespectsStatus(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	local, err := u.CreateLocal(ctx, "jane", "jane@example.com", "password1", false)
	require.NoError(t, err)
	setStatus(t, u, local.ID, "disabled")
	cfg := oidcCfg()
	cfg.LinkVerifiedEmail = true
	c := claimsFor("sub-jane")
	c.Email = "jane@example.com"
	c.EmailVerified = true

	_, _, err = ResolveLogin(ctx, u.q, cfg, c, nil)
	assert.ErrorIs(t, err, ErrAccountInactive)
	assert.Equal(t, 0, countIdentities(t, u))
}

func TestResolveVerifiedEmailDoesNotReplaceExistingIdentity(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	cfg := oidcCfg()
	cfg.LinkVerifiedEmail = true
	first := claimsFor("sub-first")
	first.Email = "jane@example.com"
	first.EmailVerified = true
	id, _, err := ResolveLogin(ctx, u.q, cfg, first, nil)
	require.NoError(t, err)

	second := claimsFor("sub-second")
	second.Email = "jane@example.com"
	second.EmailVerified = true
	_, _, err = ResolveLogin(ctx, u.q, cfg, second, nil)
	assert.ErrorIs(t, err, ErrEmailTaken)
	assert.Equal(t, 1, countIdentities(t, u))
	again, _, err := ResolveLogin(ctx, u.q, cfg, first, nil)
	require.NoError(t, err)
	assert.Equal(t, id, again)
}

func TestResolveUnverifiedEmailNotLinkedEvenWhenEnabled(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	_, err := u.CreateLocal(ctx, "jane", "jane@example.com", "password1", false)
	require.NoError(t, err)
	cfg := oidcCfg()
	cfg.LinkVerifiedEmail = true
	c := claimsFor("sub-jane")
	c.Email = "jane@example.com"
	c.EmailVerified = false

	_, _, err = ResolveLogin(ctx, u.q, cfg, c, nil)
	assert.ErrorIs(t, err, ErrEmailTaken)
	assert.Equal(t, 0, countIdentities(t, u))
}

func TestResolveNeverLinksOwnerByEmail(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	owner, err := u.Setup(ctx, "owner", "password1")
	require.NoError(t, err)
	email := "owner@example.com"
	_, err = u.Update(ctx, owner.ID, UserPatch{Email: &email})
	require.NoError(t, err)
	cfg := oidcCfg()
	cfg.LinkVerifiedEmail = true
	c := claimsFor("sub-owner")
	c.Email = "owner@example.com"
	c.EmailVerified = true

	_, _, err = ResolveLogin(ctx, u.q, cfg, c, nil)
	assert.ErrorIs(t, err, ErrEmailTaken)
	assert.Equal(t, 0, countIdentities(t, u))
	got, err := u.Get(ctx, owner.ID)
	require.NoError(t, err)
	assert.False(t, got.HasOIDC)
}

func TestResolveLinkFromSessionRejectsOwner(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	owner, err := u.Setup(ctx, "owner", "password1")
	require.NoError(t, err)

	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-x"), &owner.ID)
	assert.ErrorIs(t, err, ErrOwnerNoOIDC)
	assert.Equal(t, 0, countIdentities(t, u))
}

func TestResolveLinkFromSession(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	local, err := u.CreateLocal(ctx, "bob", "bob@example.com", "password1", false)
	require.NoError(t, err)
	// The provider's email doesn't need to match: the session proves who it is.
	c := claimsFor("sub-bob")
	c.Email = "different@example.com"

	id, out, err := ResolveLogin(ctx, u.q, oidcCfg(), c, &local.ID)
	require.NoError(t, err)
	assert.Equal(t, OutcomeLinked, out)
	assert.Equal(t, local.ID, id)

	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-bob-2"), &local.ID)
	assert.ErrorIs(t, err, ErrAlreadyLinked)

	id, out, err = ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeLogin, out)
	assert.Equal(t, local.ID, id)
}

func TestResolveLinkTargetRules(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	disabled, err := u.CreateLocal(ctx, "dis", "", "password1", false)
	require.NoError(t, err)
	setStatus(t, u, disabled.ID, "disabled")
	ssoOnly, _, err := ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-sso"), nil)
	require.NoError(t, err)
	missing := int64(9999)
	noPassword, err := u.q.CreateUser(ctx, dbq.CreateUserParams{Username: "nopw", Status: "active", CreatedAt: 1})
	require.NoError(t, err)

	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-a"), &disabled.ID)
	assert.ErrorIs(t, err, ErrAccountInactive)
	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-b"), &ssoOnly)
	assert.ErrorIs(t, err, ErrAlreadyLinked)
	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-c"), &missing)
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-d"), &noPassword.ID)
	assert.ErrorIs(t, err, ErrNotLocal)
	assert.Equal(t, 1, countIdentities(t, u))
}

func TestResolveLinkRejectsIdentityInUse(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	other, _, err := ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-taken"), nil)
	require.NoError(t, err)
	local, err := u.CreateLocal(ctx, "bob", "", "password1", false)
	require.NoError(t, err)

	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-taken"), &local.ID)
	assert.ErrorIs(t, err, ErrIdentityInUse)
	got, err := u.Get(ctx, local.ID)
	require.NoError(t, err)
	assert.False(t, got.HasOIDC)
	id, _, err := ResolveLogin(ctx, u.q, oidcCfg(), claimsFor("sub-taken"), nil)
	require.NoError(t, err)
	assert.Equal(t, other, id)
}

func TestAdminClaimGrantsAndRevokes(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	cfg := oidcCfg()
	cfg.AdminClaim = "groups"
	cfg.AdminValue = "softwarr-admins"
	c := claimsFor("sub-admin")
	c.Raw = map[string]any{"groups": []any{"engineering", "softwarr-admins"}}

	id, _, err := ResolveLogin(ctx, u.q, cfg, c, nil)
	require.NoError(t, err)
	user, err := u.Get(ctx, id)
	require.NoError(t, err)
	assert.True(t, user.IsAdmin)

	c.Raw = map[string]any{"groups": []any{}}
	_, out, err := ResolveLogin(ctx, u.q, cfg, c, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeLogin, out)
	user, err = u.Get(ctx, id)
	require.NoError(t, err)
	assert.False(t, user.IsAdmin)

	// A single string value counts too.
	c.Raw = map[string]any{"groups": "softwarr-admins"}
	_, _, err = ResolveLogin(ctx, u.q, cfg, c, nil)
	require.NoError(t, err)
	user, err = u.Get(ctx, id)
	require.NoError(t, err)
	assert.True(t, user.IsAdmin)

	// A missing claim revokes.
	c.Raw = map[string]any{}
	_, _, err = ResolveLogin(ctx, u.q, cfg, c, nil)
	require.NoError(t, err)
	user, err = u.Get(ctx, id)
	require.NoError(t, err)
	assert.False(t, user.IsAdmin)
}

func TestAdminClaimIgnoredWhenUnset(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	c := claimsFor("sub-plain")
	c.Raw = map[string]any{"groups": []any{"softwarr-admins"}}

	id, _, err := ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	require.NoError(t, err)
	user, err := u.Get(ctx, id)
	require.NoError(t, err)
	assert.False(t, user.IsAdmin)

	// Admin status set in softwarr survives later logins.
	tr := true
	_, err = u.Update(ctx, id, UserPatch{IsAdmin: &tr})
	require.NoError(t, err)
	c.Raw = map[string]any{}
	_, _, err = ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
	require.NoError(t, err)
	user, err = u.Get(ctx, id)
	require.NoError(t, err)
	assert.True(t, user.IsAdmin)
}

func TestIsAdminByClaim(t *testing.T) {
	cfg := settings.OIDC{AdminClaim: "groups", AdminValue: "admins"}
	for _, tc := range []struct {
		name    string
		raw     map[string]any
		isAdmin bool
	}{
		{"string match", map[string]any{"groups": "admins"}, true},
		{"string mismatch", map[string]any{"groups": "admins-not"}, false},
		{"array match", map[string]any{"groups": []any{"x", "admins"}}, true},
		{"string slice match", map[string]any{"groups": []string{"admins"}}, true},
		{"array mismatch", map[string]any{"groups": []any{"x"}}, false},
		{"non-string values", map[string]any{"groups": []any{1, true}}, false},
		{"missing", map[string]any{}, false},
		{"nil map", nil, false},
		{"wrong type", map[string]any{"groups": true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isAdmin, applies := IsAdminByClaim(cfg, tc.raw)
			assert.True(t, applies)
			assert.Equal(t, tc.isAdmin, isAdmin)
		})
	}
	_, applies := IsAdminByClaim(settings.OIDC{}, map[string]any{"groups": "admins"})
	assert.False(t, applies)
}

func TestUsernameCollisionSuffix(t *testing.T) {
	u := newUsers(t)
	ctx := context.Background()
	_, err := u.CreateLocal(ctx, "Jane", "", "password1", false)
	require.NoError(t, err)

	name := func(c Claims) string {
		t.Helper()
		id, _, err := ResolveLogin(ctx, u.q, oidcCfg(), c, nil)
		require.NoError(t, err)
		user, err := u.Get(ctx, id)
		require.NoError(t, err)
		return user.Username
	}

	c := claimsFor("sub-1")
	c.PreferredUsername = "jane"
	assert.Equal(t, "jane-2", name(c))
	c = claimsFor("sub-2")
	c.PreferredUsername = "jane"
	assert.Equal(t, "jane-3", name(c))

	// Falls back to the email local part, then to the subject.
	c = claimsFor("sub-3")
	c.Email = "jane@example.com"
	assert.Equal(t, "jane-4", name(c))
	c = claimsFor("abcdef0123456789")
	assert.Equal(t, "user-abcdef01", name(c))
	c = claimsFor("abcdef0199999999")
	assert.Equal(t, "user-abcdef01-2", name(c))

	// Overlong names are cut to fit the username limit.
	c = claimsFor("sub-long")
	long := ""
	for range 70 {
		long += "x"
	}
	c.PreferredUsername = long
	got := name(c)
	assert.Len(t, got, maxUsernameLen)
	c = claimsFor("sub-long-2")
	c.PreferredUsername = long
	got = name(c)
	assert.Len(t, got, maxUsernameLen)
	assert.Contains(t, got, "-2")
}
