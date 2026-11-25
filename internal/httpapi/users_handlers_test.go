package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/auth"
)

func TestUsersEndpointsAdminOnly(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.loginAsOwner()
	c := ts.newClient()
	u := c.loginAsUser()
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/v1/users"},
		{"POST", "/api/v1/users"},
		{"PATCH", fmt.Sprintf("/api/v1/users/%d", owner.ID)},
		{"DELETE", fmt.Sprintf("/api/v1/users/%d", u.ID)},
		{"POST", fmt.Sprintf("/api/v1/users/%d/approve", u.ID)},
		{"GET", "/api/v1/settings"},
		{"PUT", "/api/v1/settings"},
		{"POST", "/api/v1/settings/apikey"},
	} {
		resp := c.do(rt.method, rt.path, map[string]any{})
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, rt.method+" "+rt.path)
	}
}

func TestUserCRUD(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()

	resp := ts.do("POST", "/api/v1/users", map[string]any{"username": "bob", "email": "bob@example.com", "password": "longenough", "isAdmin": false})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	bob := decode[auth.User](t, resp)
	assert.Equal(t, "bob", bob.Username)

	resp = ts.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "longenough"})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "That username is already taken.", errorBody(t, resp))

	resp = ts.do("GET", "/api/v1/users", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, decode[[]auth.User](t, resp), 2)

	resp = ts.do("PATCH", fmt.Sprintf("/api/v1/users/%d", bob.ID), map[string]any{"isAdmin": true, "autoApprove": true})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decode[auth.User](t, resp)
	assert.True(t, got.IsAdmin)
	assert.True(t, got.AutoApprove)

	resp = ts.do("PATCH", "/api/v1/users/9999", map[string]any{"isAdmin": true})
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = ts.do("DELETE", fmt.Sprintf("/api/v1/users/%d", bob.ID), nil)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestCannotDemoteOrDeleteOwnerViaAPI(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.loginAsOwner()
	// A second admin acts on the owner.
	_, err := ts.Deps.Users.CreateLocal(t.Context(), "admin", "", testPassword, true)
	require.NoError(t, err)
	c := ts.newClient()
	c.login("admin", testPassword)

	path := fmt.Sprintf("/api/v1/users/%d", owner.ID)
	resp := c.do("PATCH", path, map[string]any{"isAdmin": false})
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	resp = c.do("PATCH", path, map[string]any{"status": "disabled"})
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	resp = c.do("DELETE", path, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestAdminCannotDisableOrDeleteSelf(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	admin, err := ts.Deps.Users.CreateLocal(t.Context(), "admin", "", testPassword, true)
	require.NoError(t, err)
	c := ts.newClient()
	c.login("admin", testPassword)

	path := fmt.Sprintf("/api/v1/users/%d", admin.ID)
	resp := c.do("PATCH", path, map[string]any{"status": "disabled"})
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "You can't disable or delete your own account.", errorBody(t, resp))
	resp = c.do("DELETE", path, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "You can't disable or delete your own account.", errorBody(t, resp))
}

func TestDisableAndDeleteEndSessions(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	uc := ts.newClient()
	u := uc.loginAsUser()
	require.Equal(t, http.StatusOK, uc.do("GET", "/api/v1/auth/me", nil).StatusCode)

	resp := ts.do("PATCH", fmt.Sprintf("/api/v1/users/%d", u.ID), map[string]any{"status": "disabled"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, http.StatusUnauthorized, uc.do("GET", "/api/v1/auth/me", nil).StatusCode)

	var n int
	require.NoError(t, ts.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, u.ID).Scan(&n))
	assert.Zero(t, n)

	resp = ts.do("PATCH", fmt.Sprintf("/api/v1/users/%d", u.ID), map[string]any{"status": "active"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	uc.login("user", testPassword)
	resp = ts.do("DELETE", fmt.Sprintf("/api/v1/users/%d", u.ID), nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, http.StatusUnauthorized, uc.do("GET", "/api/v1/auth/me", nil).StatusCode)
}

func TestDeleteUserWithRequestsIsConflict(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	u, err := ts.Deps.Users.CreateLocal(t.Context(), "bob", "", testPassword, false)
	require.NoError(t, err)
	_, err = ts.DB.Exec(`INSERT INTO requests (requester_id, status, release, release_key, created_at, updated_at)
		VALUES (?, 'pending', '{}', 'k1', 1, 1)`, u.ID)
	require.NoError(t, err)

	resp := ts.do("DELETE", fmt.Sprintf("/api/v1/users/%d", u.ID), nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "This user has requests. Disable the account instead.", errorBody(t, resp))
	_, err = ts.Deps.Users.Get(t.Context(), u.ID)
	assert.NoError(t, err)
}

func TestApprovePendingUser(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	u, err := ts.Deps.Users.CreateLocal(t.Context(), "bob", "", testPassword, false)
	require.NoError(t, err)

	resp := ts.do("POST", fmt.Sprintf("/api/v1/users/%d/approve", u.ID), nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "This account isn't waiting for approval.", errorBody(t, resp))

	pending := "pending"
	_, err = ts.Deps.Users.Update(t.Context(), u.ID, auth.UserPatch{Status: &pending})
	require.NoError(t, err)
	resp = ts.do("POST", fmt.Sprintf("/api/v1/users/%d/approve", u.ID), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "active", decode[auth.User](t, resp).Status)

	resp = ts.do("POST", "/api/v1/users/9999/approve", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
