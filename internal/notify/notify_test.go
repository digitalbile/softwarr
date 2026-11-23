package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/require"
)

var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type env struct {
	svc *Service
	q   *dbq.Queries
	b   *events.Broker
	st  *settings.Store
}

func newEnv(t *testing.T) env {
	t.Helper()
	_, q := testutil.NewDB(t)
	b := events.NewBroker()
	st := settings.NewStore(q)
	return env{New(q, b, st, http.DefaultClient, func() time.Time { return fixedNow }), q, b, st}
}

func (e env) addUser(t *testing.T, name string, admin bool, status string) int64 {
	t.Helper()
	isAdmin := int64(0)
	if admin {
		isAdmin = 1
	}
	u, err := e.q.CreateUser(context.Background(), dbq.CreateUserParams{
		Username: name, PasswordHash: "x", IsAdmin: isAdmin, Status: status, CreatedAt: 1,
	})
	require.NoError(t, err)
	return u.ID
}

func recvMsg(t *testing.T, ch <-chan events.Message) (events.Message, bool) {
	t.Helper()
	select {
	case m := <-ch:
		return m, true
	case <-time.After(50 * time.Millisecond):
		return events.Message{}, false
	}
}

func TestEmitToUserCreatesRowAndPublishes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.addUser(t, "alice", false, "active")
	other := e.addUser(t, "bob", false, "active")
	ch, cancel := e.b.Subscribe(uid, false)
	defer cancel()
	och, ocancel := e.b.Subscribe(other, false)
	defer ocancel()

	e.svc.Emit(ctx, Event{Type: EvItemAvailable, Message: "Foo is ready", RequestID: 7, ItemID: 9, ToUserIDs: []int64{uid}})

	rows, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: uid, Column2: 0})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Foo is ready", rows[0].Message)
	require.Equal(t, EvItemAvailable, rows[0].Type)
	require.Equal(t, int64(7), rows[0].RequestID.Int64)
	require.Equal(t, int64(9), rows[0].ItemID.Int64)
	require.Equal(t, fixedNow.Unix(), rows[0].CreatedAt)

	m, ok := recvMsg(t, ch)
	require.True(t, ok)
	require.Equal(t, events.TypeNotification, m.Type)
	b, err := json.Marshal(m.Data)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, "Foo is ready", got["message"])
	require.Equal(t, false, got["read"])
	require.EqualValues(t, rows[0].ID, got["id"])
	require.EqualValues(t, 7, got["requestId"])
	require.EqualValues(t, 9, got["itemId"])
	require.Contains(t, got, "createdAt")

	_, ok = recvMsg(t, och)
	require.False(t, ok)
}

func TestEmitToAdminsFansOut(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a1 := e.addUser(t, "admin1", true, "active")
	a2 := e.addUser(t, "admin2", true, "active")
	disabled := e.addUser(t, "admin3", true, "disabled")
	plain := e.addUser(t, "plain", false, "active")

	e.svc.Emit(ctx, Event{Type: EvRequestCreated, Message: "New request", RequestID: 1, ToAdmins: true})

	count := func(id int64) int {
		rows, err := e.q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: id, Column2: 0})
		require.NoError(t, err)
		return len(rows)
	}
	require.Equal(t, 1, count(a1))
	require.Equal(t, 1, count(a2))
	require.Equal(t, 0, count(disabled))
	require.Equal(t, 0, count(plain))
}

func addHook(t *testing.T, e env, url, secret string, evs []string) {
	t.Helper()
	b, _ := json.Marshal(evs)
	_, err := e.q.CreateWebhook(context.Background(), dbq.CreateWebhookParams{Name: "h", Url: url, Secret: secret, Events: string(b), Enabled: 1})
	require.NoError(t, err)
}

func TestWebhookOnlySubscribedEvents(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		bodies = append(bodies, m)
		mu.Unlock()
	}))
	defer srv.Close()
	addHook(t, e, srv.URL, "", []string{EvItemAvailable})
	cur, err := e.st.Get(context.Background())
	require.NoError(t, err)
	cur.ExternalURL = "https://sw.example"
	require.NoError(t, e.st.Save(context.Background(), cur))

	e.svc.Emit(context.Background(), Event{Type: EvRequestDenied, Message: "no", RequestID: 1})
	e.svc.Emit(context.Background(), Event{Type: EvItemAvailable, Message: "yes", ItemID: 4, Payload: map[string]any{"name": "Foo"}})
	e.svc.wait()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	b := bodies[0]
	require.Equal(t, EvItemAvailable, b["event"])
	require.Equal(t, "yes", b["message"])
	require.Equal(t, fixedNow.Format(time.RFC3339), b["timestamp"])
	require.Nil(t, b["requestId"])
	require.EqualValues(t, 4, b["itemId"])
	require.Equal(t, "https://sw.example/library/4", b["url"])
	require.Equal(t, "Foo", b["name"])
}

func TestWebhookSignature(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotSig, gotBody = r.Header.Get("X-Softwarr-Signature"), b
		mu.Unlock()
	}))
	defer srv.Close()
	addHook(t, e, srv.URL, "s3cret", []string{EvRequestApproved})

	e.svc.Emit(context.Background(), Event{Type: EvRequestApproved, Message: "ok", RequestID: 2})
	e.svc.wait()

	mu.Lock()
	defer mu.Unlock()
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(gotBody)
	require.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), gotSig)
	var m map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &m))
	require.Nil(t, m["url"], "url is null without an external URL")
}
