package settings

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetReturnsDefaultsAndGeneratesAPIKey(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	ctx := context.Background()

	got, err := s.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int{4000, 4010, 4020, 4030}, got.Prowlarr.Categories)
	assert.Equal(t, 24, got.StallHours)
	assert.True(t, got.OIDC.AutoProvision)
	assert.False(t, got.OIDC.LinkVerifiedEmail)
	assert.Equal(t, "Sign in with SSO", got.OIDC.ButtonLabel)
	assert.Equal(t, []string{"openid", "profile", "email"}, got.OIDC.Scopes)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), got.APIKey)

	again, err := s.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.APIKey, again.APIKey)

	// A fresh store on the same DB keeps the persisted key.
	reloaded, err := NewStore(q).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.APIKey, reloaded.APIKey)
}

func TestGetReturnsCopy(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	got, err := s.Get(context.Background())
	require.NoError(t, err)
	got.Prowlarr.Categories[0] = 1
	got.OIDC.Scopes[0] = "x"
	again, err := s.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 4000, again.Prowlarr.Categories[0])
	assert.Equal(t, "openid", again.OIDC.Scopes[0])
}

func TestSaveRoundTrips(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	ctx := context.Background()
	in, err := s.Get(ctx)
	require.NoError(t, err)
	in.ExternalURL = "https://soft.example.com/"
	in.CommentRequired = true
	in.DefaultRetentionDays = 30
	in.LibraryRoot = "/library"
	in.Prowlarr.URL = "http://prowlarr:9696"
	in.Prowlarr.APIKey = "abc"
	in.Prowlarr.Categories = []int{4000}
	in.OIDC.Enabled = true
	in.OIDC.Issuer = "https://idp.example.com"
	in.OIDC.AdminClaim = "groups"
	in.OIDC.AdminValue = "admin"
	require.NoError(t, s.Save(ctx, in))

	want := in
	want.ExternalURL = "https://soft.example.com"
	got, err := s.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	reloaded, err := NewStore(q).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, reloaded)
}

func TestSaveRejectsOIDCWithoutExternalURL(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	in, err := s.Get(context.Background())
	require.NoError(t, err)
	in.OIDC.Enabled = true
	err = s.Save(context.Background(), in)
	require.EqualError(t, err, "Set the external URL before enabling OIDC.")
}

func TestSaveRejectsNegativeRetention(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	in, err := s.Get(context.Background())
	require.NoError(t, err)
	in.DefaultRetentionDays = -1
	require.Error(t, s.Save(context.Background(), in))
}

func TestRegenerateAPIKey(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	ctx := context.Background()
	before, err := s.Get(ctx)
	require.NoError(t, err)

	key, err := s.RegenerateAPIKey(ctx)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), key)
	assert.NotEqual(t, before.APIKey, key)

	after, err := s.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, key, after.APIKey)
	reloaded, err := NewStore(q).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, key, reloaded.APIKey)
}

func TestReleaseSecretGeneratedPersistedAndKeptOutOfSettings(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	ctx := context.Background()

	st, err := s.Get(ctx)
	require.NoError(t, err)
	// Generated on first load, like the API key.
	raw, err := q.GetSetting(ctx, releaseSecretKey)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{64}$`), raw)

	key, err := s.ReleaseSecret(ctx)
	require.NoError(t, err)
	assert.Len(t, key, 32)
	assert.Equal(t, raw, hex.EncodeToString(key))

	again, err := NewStore(q).ReleaseSecret(ctx)
	require.NoError(t, err)
	assert.Equal(t, key, again)

	// The secret is not part of Settings, so it can't be read or written
	// through the settings API.
	b, err := json.Marshal(st)
	require.NoError(t, err)
	assert.NotContains(t, string(b), raw)
	require.NoError(t, s.Save(ctx, st))
	after, err := s.ReleaseSecret(ctx)
	require.NoError(t, err)
	assert.Equal(t, key, after)
}

func TestAdminClaimAndValueGoTogether(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	ctx := context.Background()
	const msg = "Set both the admin claim and the admin value, or leave both empty."
	for _, tc := range []struct{ claim, value string }{{"groups", ""}, {"groups", "   "}, {"", "admins"}} {
		in, err := s.Get(ctx)
		require.NoError(t, err)
		in.OIDC.AdminClaim, in.OIDC.AdminValue = tc.claim, tc.value
		err = s.Save(ctx, in)
		require.Error(t, err, "%+v", tc)
		assert.True(t, IsInputError(err))
		assert.Equal(t, msg, err.Error())
	}
	in, err := s.Get(ctx)
	require.NoError(t, err)
	in.OIDC.AdminClaim, in.OIDC.AdminValue = "groups", "admins"
	require.NoError(t, s.Save(ctx, in))
	in.OIDC.AdminClaim, in.OIDC.AdminValue = "", ""
	require.NoError(t, s.Save(ctx, in))
}

func TestLibraryRootIsNormalized(t *testing.T) {
	_, q := testutil.NewDB(t)
	s := NewStore(q)
	ctx := context.Background()
	in, err := s.Get(ctx)
	require.NoError(t, err)
	in.LibraryRoot = "  /library/apps/ \t"
	require.NoError(t, s.Save(ctx, in))
	got, err := s.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "/library/apps", got.LibraryRoot)

	in.LibraryRoot = "   "
	require.NoError(t, s.Save(ctx, in))
	got, err = s.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", got.LibraryRoot)

	// A value saved before normalization existed is cleaned on load.
	raw, err := json.Marshal(map[string]any{"apiKey": "k", "libraryRoot": "/library/ "})
	require.NoError(t, err)
	require.NoError(t, q.UpsertSetting(ctx, dbq.UpsertSettingParams{Key: settingsKey, Value: string(raw)}))
	got, err = NewStore(q).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "/library", got.LibraryRoot)

	assert.Equal(t, "", CleanLibraryRoot(""))
	assert.Equal(t, "/a/b", CleanLibraryRoot(" /a//b/ "))
}
