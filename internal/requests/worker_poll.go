package requests

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
)

// Polling: following a download until the client finishes it.

func (w *Worker) poll(ctx context.Context, row dbq.Request) error {
	c, err := w.clientByID(ctx, row.ClientID.Int64)
	if errors.Is(err, errClientGone) {
		return w.fail(ctx, row.ID, ActClientError, errClientGone.Error(), "")
	}
	if err != nil {
		return err
	}
	it, err := c.Get(ctx, row.ClientItemID)
	if err != nil {
		return err
	}
	now := w.now().Unix()
	switch it.State {
	case downloadclient.StateError, downloadclient.StateMissing:
		msg, detail := clientMessage(it)
		return w.fail(ctx, row.ID, ActClientError, msg, detail)
	case downloadclient.StateCompleted:
		_, err := w.svc.transition(ctx, row.ID, ActCompleted, func(u *dbq.UpdateRequestParams) {
			if u.Progress < 1 {
				u.Progress = 1
				u.LastProgressAt = sql.NullInt64{Int64: now, Valid: true}
			}
		})
		return skipChanged(err)
	}
	if it.Progress <= row.Progress {
		return nil
	}
	// A narrow write, not a transition: it only touches progress and only
	// while still downloading, so it can't undo an admin's abandon.
	n, err := w.q.UpdateRequestProgress(ctx, dbq.UpdateRequestProgressParams{
		Progress: it.Progress, LastProgressAt: sql.NullInt64{Int64: now, Valid: true}, UpdatedAt: now, ID: row.ID,
	})
	if err != nil || n == 0 {
		return err
	}
	data := map[string]any{"id": row.ID, "status": Downloading, "progress": it.Progress}
	w.svc.b.Publish(events.Message{Type: events.TypeActivity, Data: data, Admins: true})
	w.svc.b.Publish(events.Message{Type: events.TypeRequestUpdated, Data: data, UserIDs: []int64{row.RequesterID}})
	return nil
}

// clientMessage returns the message for a download the client reports as
// failed or gone, and the client's own words as the admin detail.
func clientMessage(it downloadclient.Item) (msg, detail string) {
	msg = "The download client reported an error."
	if it.State == downloadclient.StateMissing {
		msg = "The download client no longer has this download."
	}
	if d := strings.TrimSpace(it.Message); d != "" {
		detail = d
		if it.State != downloadclient.StateMissing {
			msg = "The download client reported an error. An admin has the details."
		}
	}
	return msg, detail
}
