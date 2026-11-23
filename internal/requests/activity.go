package requests

import (
	"context"
	"time"
)

// Activity is one request the worker is handling, or one that failed and is
// waiting for an admin.
type Activity struct {
	RequestID int64     `json:"requestId"`
	Title     string    `json:"title"`
	Requester string    `json:"requester"`
	Status    Status    `json:"status"`
	Progress  float64   `json:"progress"`
	Stalled   bool      `json:"stalled"`
	Error     string    `json:"error"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Activity lists approved, downloading, importing, and failed requests, newest
// first. A download is stalled when it has made no progress for longer than
// the stall threshold in settings.
func (s *Service) Activity(ctx context.Context) ([]Activity, error) {
	st, err := s.s.Get(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListActivityRequests(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	threshold := time.Duration(st.StallHours) * time.Hour
	out := make([]Activity, 0, len(rows))
	for _, r := range rows {
		a := Activity{
			RequestID: r.ID, Title: r.DisplayTitle, Requester: r.Requester, Status: Status(r.Status),
			Progress: r.Progress, Error: r.Error, UpdatedAt: time.Unix(r.UpdatedAt, 0).UTC(),
		}
		if a.Status == Downloading && st.StallHours > 0 && r.LastProgressAt.Valid {
			a.Stalled = now.Sub(time.Unix(r.LastProgressAt.Int64, 0)) > threshold
		}
		out = append(out, a)
	}
	return out, nil
}
