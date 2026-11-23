package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type activityBody struct {
	RequestID int64   `json:"requestId"`
	Title     string  `json:"title"`
	Requester string  `json:"requester"`
	Status    string  `json:"status"`
	Progress  float64 `json:"progress"`
	Stalled   bool    `json:"stalled"`
	Error     string  `json:"error"`
	UpdatedAt string  `json:"updatedAt"`
}

func TestActivityEndpoint(t *testing.T) {
	ts := newTestServer(t)
	seedClient(t, ts)
	owner := ts.newClient()
	owner.loginAsOwner()
	user := ts.newClient()
	user.loginAsUser()

	resp := user.do("POST", "/api/v1/requests", map[string]any{"release": relBody("a")})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	a := decode[requestBody](t, resp)
	resp = user.do("POST", "/api/v1/requests", map[string]any{"release": relBody("b")})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	b := decode[requestBody](t, resp)
	stale := ts.Clock.Now().Add(-48 * time.Hour).Unix()
	_, err := ts.DB.Exec("UPDATE requests SET status = 'downloading', progress = 0.25, last_progress_at = ? WHERE id = ?", stale, a.ID)
	require.NoError(t, err)

	resp = user.do("GET", "/api/v1/activity", nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp = owner.do("GET", "/api/v1/activity", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decode[[]activityBody](t, resp)
	require.Len(t, list, 1, "pending request %d is not activity", b.ID)
	got := list[0]
	assert.Equal(t, a.ID, got.RequestID)
	assert.Equal(t, "a", got.Title)
	assert.Equal(t, "user", got.Requester)
	assert.Equal(t, "downloading", got.Status)
	assert.InDelta(t, 0.25, got.Progress, 1e-9)
	assert.True(t, got.Stalled)
	assert.NotEmpty(t, got.UpdatedAt)
}
