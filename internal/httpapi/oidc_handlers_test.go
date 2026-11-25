package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oauth2-proxy/mockoidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/settings"
)

func newMockOIDC(t *testing.T) *mockoidc.MockOIDC {
	t.Helper()
	m, err := mockoidc.Run()
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Shutdown() })
	return m
}

// enableOIDC points the server's settings at m. externalURL "" means the
// test server's own URL (plus its URL base).
func (ts *testServer) enableOIDC(m *mockoidc.MockOIDC, externalURL string, mutate func(*settings.OIDC)) {
	t := ts.t
	t.Helper()
	ctx := context.Background()
	st, err := ts.Deps.Settings.Get(ctx)
	require.NoError(t, err)
	if externalURL == "" {
		externalURL = ts.srv.URL + ts.Deps.URLBase
	}
	st.ExternalURL = externalURL
	st.OIDC.Enabled = true
	st.OIDC.Issuer = m.Issuer()
	st.OIDC.ClientID = m.ClientID
	st.OIDC.ClientSecret = m.ClientSecret
	st.OIDC.Scopes = []string{"openid", "email", "groups"}
	if mutate != nil {
		mutate(&st.OIDC)
	}
	require.NoError(t, ts.Deps.Settings.Save(ctx, st))
}

func (ts *testServer) ensureOwner() {
	ts.t.Helper()
	if ts.owner == nil {
		u, err := ts.Deps.Users.Setup(context.Background(), "owner", testPassword)
		require.NoError(ts.t, err)
		ts.owner = &u
	}
}

// get sends a GET with c's cookie jar to an absolute URL without following redirects.
func (c *client) get(rawURL string) *http.Response {
	t := c.ts.t
	t.Helper()
	resp, err := c.http.Get(rawURL)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// throughProvider follows an authorization URL through the mock provider and
// returns the callback URL it sends the browser back to.
func (c *client) throughProvider(authURL string) string {
	t := c.ts.t
	t.Helper()
	resp := c.get(authURL)
	require.Equal(t, http.StatusFound, resp.StatusCode, "provider authorize")
	loc := resp.Header.Get("Location")
	require.True(t, strings.HasPrefix(loc, c.ts.srv.URL+c.ts.Deps.URLBase+"/api/v1/auth/oidc/callback?"), loc)
	return loc
}

// ssoLogin runs the whole sign-in flow and returns the callback's response.
func (c *client) ssoLogin() *http.Response {
	t := c.ts.t
	t.Helper()
	resp := c.request("GET", c.ts.Deps.URLBase+"/api/v1/auth/oidc/login", nil, nil)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	return c.get(c.throughProvider(resp.Header.Get("Location")))
}

// setStateCookie gives c the browser-binding cookie for state.
func (c *client) setStateCookie(state string) {
	u, err := url.Parse(c.ts.srv.URL)
	require.NoError(c.ts.t, err)
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: oidcStateCookie, Value: state, Path: c.ts.Deps.URLBase + "/api/v1/auth/oidc/"}})
}

func hasSessionCookie(resp *http.Response) bool {
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie && ck.Value != "" {
			return true
		}
	}
	return false
}

func loginError(msg string) string { return "/login?error=" + url.QueryEscape(msg) }

func TestOIDCCallbackEndToEnd(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", func(o *settings.OIDC) {
		o.AdminClaim = "groups"
		o.AdminValue = "softwarr-admins"
		o.ButtonLabel = "Sign in with Example"
	})

	cfg := decode[map[string]any](t, ts.do("GET", "/api/v1/auth/oidc/config", nil))
	assert.Equal(t, map[string]any{"enabled": true, "buttonLabel": "Sign in with Example"}, cfg)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-jane", Email: "jane@example.com", EmailVerified: true, Groups: []string{"softwarr-admins"}})
	c := ts.newClient()
	resp := c.ssoLogin()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/", resp.Header.Get("Location"))
	require.True(t, hasSessionCookie(resp))

	me := c.do("GET", "/api/v1/auth/me", nil)
	require.Equal(t, http.StatusOK, me.StatusCode)
	user := decode[auth.User](t, me)
	assert.Equal(t, "jane", user.Username)
	assert.Equal(t, "jane@example.com", user.Email)
	assert.True(t, user.HasOIDC)
	assert.False(t, user.HasPassword)
	assert.True(t, user.IsAdmin)

	// The next login from the same identity reaches the same account, and the
	// admin group is synced again.
	m.QueueUser(&mockoidc.MockUser{Subject: "sub-jane", Email: "jane@example.com", EmailVerified: true})
	c2 := ts.newClient()
	resp = c2.ssoLogin()
	require.Equal(t, "/", resp.Header.Get("Location"))
	again := decode[auth.User](t, c2.do("GET", "/api/v1/auth/me", nil))
	assert.Equal(t, user.ID, again.ID)
	assert.False(t, again.IsAdmin)
}

func TestOIDCRedirectURIUsesExternalURL(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "https://softwarr.example.com", nil)

	req, err := http.NewRequest("GET", ts.srv.URL+"/api/v1/auth/oidc/login", nil)
	require.NoError(t, err)
	req.Host = "evil.example"
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("X-Forwarded-Proto", "http")
	resp, err := ts.http.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(loc.String(), m.AuthorizationEndpoint()), loc.String())
	qv := loc.Query()
	assert.Equal(t, "https://softwarr.example.com/api/v1/auth/oidc/callback", qv.Get("redirect_uri"))
	assert.Equal(t, m.ClientID, qv.Get("client_id"))
	assert.Equal(t, "S256", qv.Get("code_challenge_method"))
	assert.NotEmpty(t, qv.Get("code_challenge"))
	assert.NotEmpty(t, qv.Get("nonce"))
	assert.NotEmpty(t, qv.Get("state"))
	assert.Equal(t, "openid email groups", qv.Get("scope"))
}

func TestOIDCWithURLBase(t *testing.T) {
	ts := newTestServerBase(t, "/base")
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-1"})
	c := ts.newClient()
	resp := c.ssoLogin()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/base/", resp.Header.Get("Location"))
	assert.Equal(t, http.StatusOK, c.do("GET", "/base/api/v1/auth/me", nil).StatusCode)
}

func TestOIDCDisabled(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()

	cfg := decode[map[string]any](t, ts.do("GET", "/api/v1/auth/oidc/config", nil))
	assert.Equal(t, map[string]any{"enabled": false, "buttonLabel": "Sign in with SSO"}, cfg)

	resp := ts.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, loginError(auth.ErrOIDCDisabled.Error()), resp.Header.Get("Location"))

	resp = ts.request("GET", "/api/v1/auth/oidc/callback?state=x&code=y", nil, nil)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/login?error="))
	assert.False(t, hasSessionCookie(resp))

	c := ts.newClient()
	c.loginAsUser()
	resp = c.do("POST", "/api/v1/auth/oidc/link", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, auth.ErrOIDCDisabled.Error(), errorBody(t, resp))
}

func TestOIDCRefusedWhileSetupNeeded(t *testing.T) {
	ts := newTestServer(t)
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	cfg := decode[map[string]any](t, ts.do("GET", "/api/v1/auth/oidc/config", nil))
	assert.Equal(t, false, cfg["enabled"])

	resp := ts.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, loginError("Finish setup before signing in with SSO."), resp.Header.Get("Location"))

	// A callback with a valid state is refused too, before talking to the provider.
	c := ts.newClient()
	require.NoError(t, ts.Q.CreateOIDCState(context.Background(), dbq.CreateOIDCStateParams{
		State: "st", CodeVerifier: "v", Nonce: "n", ExpiresAt: ts.Clock.Now().Add(time.Minute).Unix(),
	}))
	c.setStateCookie("st")
	resp = c.request("GET", "/api/v1/auth/oidc/callback?state=st&code=abc", nil, nil)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, loginError("Finish setup before signing in with SSO."), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))

	n, err := ts.Q.CountUsers(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)
	needed := decode[map[string]bool](t, ts.do("GET", "/api/v1/setup", nil))
	assert.True(t, needed["needed"])
}

func TestOIDCPendingCreatesNoSession(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", func(o *settings.OIDC) { o.AutoProvision = false })

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-p"})
	c := ts.newClient()
	resp := c.ssoLogin()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, loginError(auth.ErrAwaitingApproval.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
	assert.Equal(t, http.StatusUnauthorized, c.do("GET", "/api/v1/auth/me", nil).StatusCode)

	var status string
	require.NoError(t, ts.DB.QueryRow("SELECT status FROM users WHERE username = 'user-sub-p'").Scan(&status))
	assert.Equal(t, "pending", status)
}

func TestOIDCDisabledAccountCreatesNoSession(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-d"})
	c := ts.newClient()
	require.Equal(t, "/", c.ssoLogin().Header.Get("Location"))
	me := decode[auth.User](t, c.do("GET", "/api/v1/auth/me", nil))
	disabled := "disabled"
	_, err := ts.Deps.Users.Update(context.Background(), me.ID, auth.UserPatch{Status: &disabled})
	require.NoError(t, err)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-d"})
	c2 := ts.newClient()
	resp := c2.ssoLogin()
	assert.Equal(t, loginError(auth.ErrAccountInactive.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
}

func TestOIDCEmailTakenRedirectsWithMessage(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)
	_, err := ts.Deps.Users.CreateLocal(context.Background(), "jane", "jane@example.com", testPassword, false)
	require.NoError(t, err)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-x", Email: "jane@example.com", EmailVerified: true})
	c := ts.newClient()
	resp := c.ssoLogin()
	assert.Equal(t, loginError(auth.ErrEmailTaken.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
}

func TestOIDCStateIsSingleUseAndExpires(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-1"})
	c := ts.newClient()
	resp := c.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	cb := c.throughProvider(resp.Header.Get("Location"))
	require.Equal(t, "/", c.get(cb).Header.Get("Location"))

	// Replaying the same callback fails, even with the state cookie.
	replay := ts.newClient()
	replay.setStateCookie(mustQuery(t, cb).Get("state"))
	resp = replay.get(cb)
	assert.Equal(t, loginError(auth.ErrOIDCStateInvalid.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))

	// A state older than ten minutes is rejected and removed.
	m.QueueUser(&mockoidc.MockUser{Subject: "sub-2"})
	c2 := ts.newClient()
	resp = c2.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	cb = c2.throughProvider(resp.Header.Get("Location"))
	ts.Clock.Advance(10*time.Minute + time.Second)
	resp = c2.get(cb)
	assert.Equal(t, loginError(auth.ErrOIDCStateInvalid.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
	var n int
	require.NoError(t, ts.DB.QueryRow("SELECT COUNT(*) FROM oidc_states").Scan(&n))
	assert.Zero(t, n)
}

func TestOIDCBeginPurgesExpiredStates(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	ts.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	ts.Clock.Advance(11 * time.Minute)
	ts.newClient().request("GET", "/api/v1/auth/oidc/login", nil, nil)
	var n int
	require.NoError(t, ts.DB.QueryRow("SELECT COUNT(*) FROM oidc_states").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestOIDCCallbackNeedsTheBrowserThatStarted(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	// An attacker starts a sign-in and hands the callback link to someone else.
	m.QueueUser(&mockoidc.MockUser{Subject: "sub-attacker"})
	attacker := ts.newClient()
	resp := attacker.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	cb := attacker.throughProvider(resp.Header.Get("Location"))

	victim := ts.newClient()
	resp = victim.get(cb)
	assert.Equal(t, loginError(auth.ErrOIDCStateInvalid.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))

	// The state was consumed, so the attacker can't use it afterwards either.
	resp = attacker.get(cb)
	assert.Equal(t, loginError(auth.ErrOIDCStateInvalid.Error()), resp.Header.Get("Location"))
}

func TestOIDCNonceMismatchRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-1"})
	c := ts.newClient()
	resp := c.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	cb := c.throughProvider(resp.Header.Get("Location"))
	_, err := ts.DB.Exec("UPDATE oidc_states SET nonce = 'tampered'")
	require.NoError(t, err)

	resp = c.get(cb)
	assert.Equal(t, loginError(auth.ErrOIDCFailed.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
	var n int
	require.NoError(t, ts.DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'user-sub-1'").Scan(&n))
	assert.Zero(t, n)
}

func TestOIDCProviderErrorRedirects(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	c := ts.newClient()
	resp := c.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	state := mustQuery(t, resp.Header.Get("Location")).Get("state")
	resp = c.request("GET", "/api/v1/auth/oidc/callback?error=access_denied&state="+url.QueryEscape(state), nil, nil)
	assert.Equal(t, loginError(auth.ErrOIDCFailed.Error()), resp.Header.Get("Location"))
	var n int
	require.NoError(t, ts.DB.QueryRow("SELECT COUNT(*) FROM oidc_states").Scan(&n))
	assert.Zero(t, n)
}

func TestOIDCProviderRebuiltWhenIssuerChanges(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	first := newMockOIDC(t)
	ts.enableOIDC(first, "", nil)
	resp := ts.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), first.AuthorizationEndpoint()))

	second := newMockOIDC(t)
	ts.enableOIDC(second, "", nil)
	second.QueueUser(&mockoidc.MockUser{Subject: "sub-2"})
	c := ts.newClient()
	resp = c.request("GET", "/api/v1/auth/oidc/login", nil, nil)
	require.True(t, strings.HasPrefix(resp.Header.Get("Location"), second.AuthorizationEndpoint()))
	require.Equal(t, "/", c.get(c.throughProvider(resp.Header.Get("Location"))).Header.Get("Location"))

	var issuer string
	require.NoError(t, ts.DB.QueryRow("SELECT issuer FROM user_identities").Scan(&issuer))
	assert.Equal(t, second.Issuer(), issuer)
}

func TestOIDCLinkFlow(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	c := ts.newClient()
	local := c.loginAsUser()
	resp := c.do("POST", "/api/v1/auth/oidc/link", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	redirect := decode[map[string]string](t, resp)["redirect"]
	require.True(t, strings.HasPrefix(redirect, m.AuthorizationEndpoint()), redirect)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-link", Email: "someone@example.com", EmailVerified: true})
	resp = c.get(c.throughProvider(redirect))
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/profile?linked=1", resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp), "linking must not start a new session")

	me := decode[auth.User](t, c.do("GET", "/api/v1/auth/me", nil))
	assert.Equal(t, local.ID, me.ID)
	assert.True(t, me.HasOIDC)
	assert.True(t, me.HasPassword)

	// The linked identity now signs in to the local account.
	m.QueueUser(&mockoidc.MockUser{Subject: "sub-link"})
	c2 := ts.newClient()
	require.Equal(t, "/", c2.ssoLogin().Header.Get("Location"))
	assert.Equal(t, local.ID, decode[auth.User](t, c2.do("GET", "/api/v1/auth/me", nil)).ID)

	// Linking again is refused up front.
	resp = c.do("POST", "/api/v1/auth/oidc/link", nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, auth.ErrAlreadyLinked.Error(), errorBody(t, resp))
}

func TestOIDCLinkCallbackWithoutSessionLinksButNeverSignsIn(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	c := ts.newClient()
	local := c.loginAsUser()
	redirect := decode[map[string]string](t, c.do("POST", "/api/v1/auth/oidc/link", nil))["redirect"]
	require.Equal(t, http.StatusNoContent, c.do("POST", "/api/v1/auth/logout", nil).StatusCode)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-link"})
	resp := c.get(c.throughProvider(redirect))
	assert.Equal(t, "/profile?linked=1", resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
	assert.Equal(t, http.StatusUnauthorized, c.do("GET", "/api/v1/auth/me", nil).StatusCode)

	got, err := ts.Deps.Users.Get(context.Background(), local.ID)
	require.NoError(t, err)
	assert.True(t, got.HasOIDC)
}

func TestOIDCLinkRejectsIdentityInUse(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-taken"})
	require.Equal(t, "/", ts.newClient().ssoLogin().Header.Get("Location"))

	c := ts.newClient()
	local := c.loginAsUser()
	redirect := decode[map[string]string](t, c.do("POST", "/api/v1/auth/oidc/link", nil))["redirect"]
	m.QueueUser(&mockoidc.MockUser{Subject: "sub-taken"})
	resp := c.get(c.throughProvider(redirect))
	assert.Equal(t, loginError(auth.ErrIdentityInUse.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
	me := decode[auth.User](t, c.do("GET", "/api/v1/auth/me", nil))
	assert.Equal(t, local.ID, me.ID)
	assert.False(t, me.HasOIDC)
}

func TestOIDCLinkEligibility(t *testing.T) {
	ts := newTestServer(t)
	m := newMockOIDC(t)
	owner := ts.newClient()
	owner.loginAsOwner()
	ts.enableOIDC(m, "", nil)

	resp := owner.do("POST", "/api/v1/auth/oidc/link", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, auth.ErrOwnerNoOIDC.Error(), errorBody(t, resp))

	st, err := ts.Deps.Settings.Get(context.Background())
	require.NoError(t, err)
	resp = ts.request("POST", "/api/v1/auth/oidc/link", nil, map[string]string{"X-Api-Key": st.APIKey})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, auth.ErrOwnerNoOIDC.Error(), errorBody(t, resp))

	assert.Equal(t, http.StatusUnauthorized, ts.newClient().do("POST", "/api/v1/auth/oidc/link", nil).StatusCode)

	// An account without a password (created by SSO) can't start a link.
	m.QueueUser(&mockoidc.MockUser{Subject: "sub-sso"})
	sso := ts.newClient()
	require.Equal(t, "/", sso.ssoLogin().Header.Get("Location"))
	resp = sso.do("POST", "/api/v1/auth/oidc/link", nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	var n int
	require.NoError(t, ts.DB.QueryRow("SELECT COUNT(*) FROM oidc_states WHERE link_user_id IS NOT NULL").Scan(&n))
	assert.Zero(t, n)
}

func TestOIDCLinkRecheckedAtCallback(t *testing.T) {
	ts := newTestServer(t)
	ts.ensureOwner()
	m := newMockOIDC(t)
	ts.enableOIDC(m, "", nil)

	c := ts.newClient()
	local := c.loginAsUser()
	redirect := decode[map[string]string](t, c.do("POST", "/api/v1/auth/oidc/link", nil))["redirect"]
	disabled := "disabled"
	_, err := ts.Deps.Users.Update(context.Background(), local.ID, auth.UserPatch{Status: &disabled})
	require.NoError(t, err)

	m.QueueUser(&mockoidc.MockUser{Subject: "sub-late"})
	resp := c.get(c.throughProvider(redirect))
	assert.Equal(t, loginError(auth.ErrAccountInactive.Error()), resp.Header.Get("Location"))
	assert.False(t, hasSessionCookie(resp))
	got, err := ts.Deps.Users.Get(context.Background(), local.ID)
	require.NoError(t, err)
	assert.False(t, got.HasOIDC)
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Query()
}
