package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupFlow(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.do("GET", "/api/v1/setup", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, map[string]bool{"needed": true}, decode[map[string]bool](t, resp))

	body := map[string]string{"username": "owner", "password": testPassword}
	resp = ts.do("POST", "/api/v1/setup", body)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == "softwarr_session" {
			found = true
			assert.True(t, c.HttpOnly)
			assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
			assert.Equal(t, "/", c.Path)
		}
	}
	assert.True(t, found, "session cookie set")

	me := ts.do("GET", "/api/v1/auth/me", nil)
	assert.Equal(t, http.StatusOK, me.StatusCode)

	resp = ts.do("GET", "/api/v1/setup", nil)
	assert.Equal(t, map[string]bool{"needed": false}, decode[map[string]bool](t, resp))

	resp = ts.do("POST", "/api/v1/setup", body)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "Setup has already been completed.", errorBody(t, resp))
}

func TestSetupValidation400(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.do("POST", "/api/v1/setup", map[string]string{"username": "owner", "password": "short"})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Passwords need at least 8 characters.", errorBody(t, resp))
}

func TestLoginLogoutMe(t *testing.T) {
	ts := newTestServer(t)
	u := ts.loginAsOwner()
	assert.True(t, u.IsOwner)

	resp := ts.do("GET", "/api/v1/auth/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	me := decode[map[string]any](t, resp)
	assert.Equal(t, "owner", me["username"])
	assert.Equal(t, true, me["isOwner"])
	assert.Equal(t, "active", me["status"])
	assert.Contains(t, me, "mustChangePassword")

	resp = ts.do("POST", "/api/v1/auth/logout", nil)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp = ts.do("GET", "/api/v1/auth/me", nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "Sign in to continue.", errorBody(t, resp))
}

func TestLoginWrongPassword401(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	c := ts.newClient()
	resp := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "owner", "password": "nope nope nope"})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "Wrong username or password.", errorBody(t, resp))
	assert.Empty(t, resp.Cookies())
}

func TestMutationWithoutCSRFHeader403(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	resp := ts.request("POST", "/api/v1/auth/logout", nil, nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "Missing CSRF header.", errorBody(t, resp))
	// Safe methods don't need it.
	resp = ts.request("GET", "/api/v1/auth/me", nil, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestLoginAndSetupNeedCSRFHeader(t *testing.T) {
	ts := newTestServer(t)
	// A cross-site form can post text/plain but can't add a custom header.
	form := map[string]string{"Content-Type": "text/plain"}
	resp := ts.request("POST", "/api/v1/setup", map[string]string{"username": "evil", "password": testPassword}, form)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "Missing CSRF header.", errorBody(t, resp))
	var n int
	require.NoError(t, ts.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&n))
	assert.Zero(t, n)

	ts.loginAsOwner()
	c := ts.newClient()
	resp = c.request("POST", "/api/v1/auth/login", map[string]string{"username": "owner", "password": testPassword}, form)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "Missing CSRF header.", errorBody(t, resp))
	assert.Empty(t, resp.Cookies(), "no session for a forged login")

	// A wrong API key doesn't excuse the header either.
	resp = c.request("POST", "/api/v1/auth/login", map[string]string{"username": "owner", "password": testPassword}, map[string]string{"X-Api-Key": "wrong"})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	c.login("owner", testPassword)
}

func TestAPIKeyActsAsAdmin(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	st, err := ts.Deps.Settings.Get(context.Background())
	require.NoError(t, err)

	// A fresh client has no cookie; the API key alone authenticates it.
	c := ts.newClient()
	resp := c.request("POST", "/api/v1/auth/logout", nil, map[string]string{"X-Api-Key": st.APIKey})
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "no CSRF header needed")
	assert.Empty(t, resp.Cookies(), "no cookie for API key requests")

	resp = c.request("GET", "/api/v1/auth/me", nil, map[string]string{"X-Api-Key": st.APIKey})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "owner", decode[map[string]any](t, resp)["username"])

	resp = c.request("GET", "/api/v1/auth/me", nil, map[string]string{"X-Api-Key": "wrong"})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestRequireAdmin(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	admin := requireAdmin(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	// Exercise through the real middleware chain with a throwaway server.
	s := &server{Deps: ts.Deps}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin", admin)
	handler := s.authenticate(mux)
	srv := newHandlerServer(t, handler)

	u := ts.newClient()
	u.loginAsUser()
	resp := doOn(t, srv, u, "/admin")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "Admins only.", errorBody(t, resp))

	o := ts.newClient()
	o.loginAsOwner()
	assert.Equal(t, http.StatusOK, doOn(t, srv, o, "/admin").StatusCode)

	// An API key counts as admin and needs no CSRF header.
	st, err := ts.Deps.Settings.Get(context.Background())
	require.NoError(t, err)
	req, _ := http.NewRequest("GET", srv.URL+"/admin", nil)
	req.Header.Set("X-Api-Key", st.APIKey)
	resp, err = ts.newClient().http.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMustChangePasswordBlocksOtherRoutes(t *testing.T) {
	ts := newTestServer(t)
	u := ts.loginAsUser()
	_, err := ts.DB.Exec(`UPDATE users SET must_change_password = 1 WHERE id = ?`, u.ID)
	require.NoError(t, err)

	resp := ts.do("GET", "/api/v1/requests", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "password_change_required", errorBody(t, resp))
	resp = ts.do("GET", "/api/v1/events", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// The web app asks /setup before /auth/me, so it has to keep working.
	resp = ts.do("GET", "/api/v1/setup", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, false, decode[map[string]bool](t, resp)["needed"])
	resp = ts.do("GET", "/api/v1/auth/oidc/config", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp = ts.do("GET", "/api/v1/auth/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, true, decode[map[string]any](t, resp)["mustChangePassword"])

	resp = ts.do("POST", "/api/v1/auth/password", map[string]string{"current": testPassword, "new": "a brand new one"})
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp = ts.do("GET", "/api/v1/setup", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// The old password no longer works; the new one does.
	c := ts.newClient()
	resp = c.do("POST", "/api/v1/auth/login", map[string]string{"username": "user", "password": testPassword})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	c.login("user", "a brand new one")
}

func TestChangePasswordWrongCurrent400(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsUser()
	resp := ts.do("POST", "/api/v1/auth/password", map[string]string{"current": "not it at all", "new": "a brand new one"})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Your current password is wrong.", errorBody(t, resp))
}

func TestDisabledUserLosesSessionImmediately(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	u := ts.newClient()
	user := u.loginAsUser()
	assert.Equal(t, http.StatusOK, u.do("GET", "/api/v1/auth/me", nil).StatusCode)

	_, err := ts.DB.Exec(`UPDATE users SET status = 'disabled' WHERE id = ?`, user.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, u.do("GET", "/api/v1/auth/me", nil).StatusCode)

	_, err = ts.DB.Exec(`UPDATE users SET status = 'active' WHERE id = ?`, user.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, u.do("GET", "/api/v1/auth/me", nil).StatusCode, "session was deleted")
}

func TestSessionExpires(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	assert.Equal(t, http.StatusOK, ts.do("GET", "/api/v1/auth/me", nil).StatusCode)
	ts.Clock.Advance(31 * 24 * time.Hour)
	assert.Equal(t, http.StatusUnauthorized, ts.do("GET", "/api/v1/auth/me", nil).StatusCode)
}

func TestSessionSlidesWhenHalfUsed(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	ts.Clock.Advance(20 * 24 * time.Hour)
	assert.Equal(t, http.StatusOK, ts.do("GET", "/api/v1/auth/me", nil).StatusCode) // extends
	ts.Clock.Advance(20 * 24 * time.Hour)
	assert.Equal(t, http.StatusOK, ts.do("GET", "/api/v1/auth/me", nil).StatusCode)
}

func TestURLBasePrefix(t *testing.T) {
	ts := newTestServerBase(t, "/sw")
	resp := ts.do("GET", "/sw/api/v1/ping", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "test", decode[map[string]string](t, resp)["version"])
	assert.Equal(t, http.StatusNotFound, ts.do("GET", "/api/v1/ping", nil).StatusCode)

	// The session cookie is scoped to the base path.
	body := map[string]string{"username": "owner", "password": testPassword}
	resp = ts.do("POST", "/sw/api/v1/setup", body)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.NotEmpty(t, resp.Cookies())
	assert.Equal(t, "/sw/", resp.Cookies()[0].Path)
}

func sessionCookies(resp *http.Response) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "softwarr_session" {
			out = append(out, c)
		}
	}
	return out
}

func TestTransientErrorKeepsSession(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	count := func() int {
		var n int
		require.NoError(t, ts.DB.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n))
		return n
	}
	require.Equal(t, 1, count())

	// Break a table that Users.Get reads after the user row loads.
	_, err := ts.DB.Exec(`ALTER TABLE user_identities RENAME TO user_identities_off`)
	require.NoError(t, err)
	resp := ts.do("GET", "/api/v1/auth/me", nil)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "Something went wrong.", errorBody(t, resp))
	assert.Equal(t, 1, count(), "session survives a database error")

	_, err = ts.DB.Exec(`ALTER TABLE user_identities_off RENAME TO user_identities`)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, ts.do("GET", "/api/v1/auth/me", nil).StatusCode)
}

func TestCookieReissuedOnlyWhenExtended(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()

	resp := ts.do("GET", "/api/v1/auth/me", nil)
	assert.Empty(t, sessionCookies(resp), "plenty of time left, no new cookie")

	ts.Clock.Advance(20 * 24 * time.Hour)
	resp = ts.do("GET", "/api/v1/auth/me", nil)
	cs := sessionCookies(resp)
	require.Len(t, cs, 1)
	want := ts.Clock.Now().Add(30 * 24 * time.Hour)
	assert.WithinDuration(t, want, cs[0].Expires, 2*time.Second)

	resp = ts.do("GET", "/api/v1/auth/me", nil)
	assert.Empty(t, sessionCookies(resp), "just extended, no new cookie")
}

func TestLogoutSendsOneSessionCookie(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	ts.Clock.Advance(20 * 24 * time.Hour) // forces an extension on this request
	resp := ts.do("POST", "/api/v1/auth/logout", nil)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	cs := sessionCookies(resp)
	require.Len(t, cs, 1)
	assert.Equal(t, -1, cs[0].MaxAge)
}

func TestMeAndLoginIncludeCommentRequired(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()

	resp := ts.do("GET", "/api/v1/auth/me", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	me := decode[map[string]any](t, resp)
	assert.Equal(t, false, me["commentRequired"])

	st, err := ts.Deps.Settings.Get(context.Background())
	require.NoError(t, err)
	st.CommentRequired = true
	require.NoError(t, ts.Deps.Settings.Save(context.Background(), st))

	resp = ts.do("GET", "/api/v1/auth/me", nil)
	me = decode[map[string]any](t, resp)
	assert.Equal(t, true, me["commentRequired"])

	resp = ts.newClient().do("POST", "/api/v1/auth/login", map[string]string{"username": "owner", "password": testPassword})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, true, decode[map[string]any](t, resp)["commentRequired"])
}
