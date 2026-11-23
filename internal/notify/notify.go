// Package notify records in-app notifications and delivers webhooks.
package notify

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/settings"
)

const (
	EvRequestCreated  = "request.created"
	EvRequestApproved = "request.approved"
	EvRequestDenied   = "request.denied"
	EvRequestFailed   = "request.failed"
	EvItemAvailable   = "item.available"
	EvItemExpired     = "item.expired"
	EvHealthWarning   = "health.warning"
)

// AllEvents lists every event type a webhook can subscribe to.
var AllEvents = []string{
	EvRequestCreated, EvRequestApproved, EvRequestDenied, EvRequestFailed,
	EvItemAvailable, EvItemExpired, EvHealthWarning,
}

// Event is something worth telling people about.
type Event struct {
	Type      string
	Message   string
	RequestID int64 // 0 means none
	ItemID    int64 // 0 means none
	ToUserIDs []int64
	ToAdmins  bool
	Payload   map[string]any // extra webhook body fields
}

// Notification is the JSON shape of a stored notification, also sent over SSE.
type Notification struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	RequestID *int64 `json:"requestId"`
	ItemID    *int64 `json:"itemId"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"createdAt"`
}

func toNotification(n dbq.Notification) Notification {
	out := Notification{ID: n.ID, Type: n.Type, Message: n.Message, Read: n.ReadAt.Valid, CreatedAt: n.CreatedAt}
	if n.RequestID.Valid {
		out.RequestID = &n.RequestID.Int64
	}
	if n.ItemID.Valid {
		out.ItemID = &n.ItemID.Int64
	}
	return out
}

type Service struct {
	q     *dbq.Queries
	b     *events.Broker
	s     *settings.Store
	httpc *http.Client
	now   func() time.Time
	wg    sync.WaitGroup
}

func New(q *dbq.Queries, b *events.Broker, s *settings.Store, httpc *http.Client, now func() time.Time) *Service {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	if now == nil {
		now = time.Now
	}
	return &Service{q: q, b: b, s: s, httpc: httpc, now: now}
}

// wait blocks until in-flight webhook sends finish. Tests use it.
func (s *Service) wait() { s.wg.Wait() }

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// Emit stores a notification for each recipient, pushes it over SSE and sends
// matching webhooks in the background. Failures are logged.
func (s *Service) Emit(ctx context.Context, e Event) {
	recipients := slices.Clone(e.ToUserIDs)
	if e.ToAdmins {
		ids, err := s.q.ListActiveAdminIDs(ctx)
		if err != nil {
			slog.Error("notify: list admins", "err", err)
		}
		recipients = append(recipients, ids...)
	}
	slices.Sort(recipients)
	recipients = slices.Compact(recipients)

	for _, uid := range recipients {
		row, err := s.q.InsertNotification(ctx, dbq.InsertNotificationParams{
			UserID: uid, Type: e.Type, Message: e.Message,
			RequestID: nullID(e.RequestID), ItemID: nullID(e.ItemID),
			CreatedAt: s.now().Unix(),
		})
		if err != nil {
			slog.Error("notify: insert notification", "user", uid, "err", err)
			continue
		}
		s.b.Publish(events.Message{Type: events.TypeNotification, Data: toNotification(row), UserIDs: []int64{uid}})
	}

	s.sendWebhooks(ctx, e)
}
