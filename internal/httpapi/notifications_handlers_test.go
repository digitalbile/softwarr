package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/stretchr/testify/require"
)

type notifJSON struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`
	Read    bool   `json:"read"`
}

func TestNotificationsOnlyOwn(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.loginAsOwner()
	userClient := ts.newClient()
	user := userClient.loginAsUser()
	ctx := context.Background()

	ts.Deps.Notify.Emit(ctx, notify.Event{Type: notify.EvItemAvailable, Message: "for owner", ToUserIDs: []int64{owner.ID}})
	ts.Deps.Notify.Emit(ctx, notify.Event{Type: notify.EvItemAvailable, Message: "for user", ToUserIDs: []int64{user.ID}})

	ownerList := decode[[]notifJSON](t, ts.do("GET", "/api/v1/notifications", nil))
	require.Len(t, ownerList, 1)
	require.Equal(t, "for owner", ownerList[0].Message)
	userList := decode[[]notifJSON](t, userClient.do("GET", "/api/v1/notifications", nil))
	require.Len(t, userList, 1)

	// The user tries to mark the owner's notification read: silently scoped away.
	resp := userClient.do("POST", "/api/v1/notifications/read", map[string]any{"ids": []int64{ownerList[0].ID}})
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	ownerList = decode[[]notifJSON](t, ts.do("GET", "/api/v1/notifications?unread=1", nil))
	require.Len(t, ownerList, 1)
	require.False(t, ownerList[0].Read)

	// Marking their own works.
	resp = userClient.do("POST", "/api/v1/notifications/read", map[string]any{"ids": []int64{userList[0].ID}})
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Empty(t, decode[[]notifJSON](t, userClient.do("GET", "/api/v1/notifications?unread=1", nil)))
	require.Len(t, decode[[]notifJSON](t, userClient.do("GET", "/api/v1/notifications", nil)), 1)
}

func TestNotificationsMarkAllRead(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.loginAsOwner()
	for i := 0; i < 3; i++ {
		ts.Deps.Notify.Emit(context.Background(), notify.Event{Type: notify.EvHealthWarning, Message: "m", ToUserIDs: []int64{owner.ID}})
	}
	require.Len(t, decode[[]notifJSON](t, ts.do("GET", "/api/v1/notifications?unread=1", nil)), 3)
	require.Equal(t, http.StatusNoContent, ts.do("POST", "/api/v1/notifications/read", map[string]any{"all": true}).StatusCode)
	require.Empty(t, decode[[]notifJSON](t, ts.do("GET", "/api/v1/notifications?unread=1", nil)))
}

func TestNotificationsRequireLogin(t *testing.T) {
	ts := newTestServer(t)
	require.Equal(t, http.StatusUnauthorized, ts.do("GET", "/api/v1/notifications", nil).StatusCode)
}

type hookJSON struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	HasSecret bool     `json:"hasSecret"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
}

func TestWebhookCRUDSecretHandling(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()

	resp := ts.do("POST", "/api/v1/webhooks", map[string]any{
		"name": "n", "url": "https://example.com/hook", "secret": "s", "events": []string{"item.available"}, "enabled": true,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	h := decode[map[string]any](t, resp)
	require.NotContains(t, h, "secret")
	require.Equal(t, true, h["hasSecret"])
	id := int64(h["id"].(float64))
	path := "/api/v1/webhooks/" + itoa(id)

	// Omitted secret keeps the existing one.
	resp = ts.do("PUT", path, map[string]any{"name": "n2", "url": "https://example.com/hook", "events": []string{"item.available"}, "enabled": false})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decode[hookJSON](t, resp)
	require.True(t, got.HasSecret)
	require.Equal(t, "n2", got.Name)
	require.False(t, got.Enabled)

	list := decode[[]hookJSON](t, ts.do("GET", "/api/v1/webhooks", nil))
	require.Len(t, list, 1)
	require.True(t, list[0].HasSecret)

	// clearSecret removes it.
	resp = ts.do("PUT", path, map[string]any{"name": "n2", "url": "https://example.com/hook", "clearSecret": true, "events": []string{}, "enabled": true})
	require.False(t, decode[hookJSON](t, resp).HasSecret)

	require.Equal(t, http.StatusNoContent, ts.do("DELETE", path, nil).StatusCode)
	require.Equal(t, http.StatusNotFound, ts.do("DELETE", path, nil).StatusCode)
	require.Empty(t, decode[[]hookJSON](t, ts.do("GET", "/api/v1/webhooks", nil)))
}

func TestWebhookValidationAndAdminOnly(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	for _, u := range []string{"", "ftp://x.com/a", "not a url", "/relative", "http://"} {
		resp := ts.do("POST", "/api/v1/webhooks", map[string]any{"name": "n", "url": u, "events": []string{}, "enabled": true})
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, u)
		require.Equal(t, "Enter a full http or https URL.", errorBody(t, resp))
	}
	resp := ts.do("POST", "/api/v1/webhooks", map[string]any{"name": "n", "url": "http://x.com", "events": []string{"bogus"}, "enabled": true})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	uc := ts.newClient()
	uc.loginAsUser()
	require.Equal(t, http.StatusForbidden, uc.do("GET", "/api/v1/webhooks", nil).StatusCode)
	require.Equal(t, http.StatusForbidden, uc.do("POST", "/api/v1/webhooks/1/test", nil).StatusCode)
}

func TestWebhookTestEndpoint(t *testing.T) {
	ts := newTestServer(t)
	ts.loginAsOwner()
	var gotEvent string
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = decodeJSON(r, &m)
		gotEvent, _ = m["event"].(string)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer recv.Close()
	h := decode[hookJSON](t, ts.do("POST", "/api/v1/webhooks", map[string]any{"name": "n", "url": recv.URL, "events": []string{}, "enabled": true}))

	res := decode[map[string]any](t, ts.do("POST", "/api/v1/webhooks/"+itoa(h.ID)+"/test", nil))
	require.Equal(t, "test", gotEvent)
	require.Equal(t, false, res["ok"])
	require.EqualValues(t, http.StatusTeapot, res["status"])

	recv.Close()
	res = decode[map[string]any](t, ts.do("POST", "/api/v1/webhooks/"+itoa(h.ID)+"/test", nil))
	require.Equal(t, false, res["ok"])
	require.NotEmpty(t, res["error"])

	require.Equal(t, http.StatusNotFound, ts.do("POST", "/api/v1/webhooks/999/test", nil).StatusCode)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
