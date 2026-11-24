package requests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/settings"
)

var (
	ErrConflict        = errors.New("This request was changed by someone else. Refresh and try again.")
	ErrDuplicate       = errors.New("This release has already been requested.")
	ErrInLibrary       = errors.New("This release is already in the library.")
	ErrCommentRequired = errors.New("Please say why you need this software.")
	ErrNoClient        = errors.New("No compatible download client")
	ErrNoLibraryRoot   = errors.New("Set a library folder in Settings > Library before approving requests.")
	ErrNotFound        = errors.New("Request not found.")
)

// inputError is a validation failure that is safe to show to the user.
type inputError string

func (e inputError) Error() string { return string(e) }

// IsInputError reports whether err is a release validation failure.
func IsInputError(err error) bool {
	var ie inputError
	return errors.As(err, &ie)
}

type Service struct {
	db  *sql.DB
	q   *dbq.Queries
	s   *settings.Store
	n   *notify.Service
	b   *events.Broker
	now func() time.Time
}

func NewService(db *sql.DB, q *dbq.Queries, s *settings.Store, n *notify.Service, b *events.Broker, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, q: q, s: s, n: n, b: b, now: now}
}

func validateRelease(r prowlarr.Release) error {
	if strings.TrimSpace(r.Title) == "" {
		return inputError("This release has no title.")
	}
	switch r.Protocol {
	case "torrent":
		if r.DownloadURL == "" && r.MagnetURL == "" {
			return inputError("This release has no download link.")
		}
	case "usenet":
	default:
		return inputError("This release has an unknown protocol.")
	}
	return nil
}

func (s *Service) checkClient(ctx context.Context, protocol string) error {
	_, ok, err := downloadclient.PickClient(ctx, s.q, protocol)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoClient
	}
	return nil
}

func (s *Service) checkNotInLibrary(ctx context.Context, key string) error {
	rows, err := s.q.ListAvailableLibraryKeys(ctx, []string{key})
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return ErrInLibrary
	}
	return nil
}

func (s *Service) Create(ctx context.Context, p auth.Principal, rel prowlarr.Release, comment string) (Request, error) {
	if err := validateRelease(rel); err != nil {
		return Request{}, err
	}
	comment = strings.TrimSpace(comment)
	st, err := s.s.Get(ctx)
	if err != nil {
		return Request{}, err
	}
	if st.CommentRequired && comment == "" {
		return Request{}, ErrCommentRequired
	}
	if err := s.checkClient(ctx, rel.Protocol); err != nil {
		return Request{}, err
	}
	key := rel.Key()
	if err := s.checkNotInLibrary(ctx, key); err != nil {
		return Request{}, err
	}
	raw, err := prowlarr.MarshalStored(rel)
	if err != nil {
		return Request{}, err
	}
	status := Pending
	var approvedBy sql.NullInt64
	// Without a library folder nothing could be imported, so wait for an admin.
	if (p.IsAdmin || p.AutoApprove) && st.LibraryRoot != "" {
		status = Approved
		approvedBy = sql.NullInt64{Int64: p.UserID, Valid: true}
	}
	ts := s.now().Unix()
	id, err := s.q.InsertRequest(ctx, dbq.InsertRequestParams{
		RequesterID: p.UserID, Status: string(status), Release: string(raw), ReleaseKey: key,
		Comment: comment, DisplayTitle: rel.Title, ApprovedBy: approvedBy, CreatedAt: ts, UpdatedAt: ts,
	})
	if db.IsUniqueViolation(err) {
		return Request{}, ErrDuplicate
	}
	if err != nil {
		return Request{}, err
	}
	req, err := s.load(ctx, id)
	if err != nil {
		return Request{}, err
	}
	s.publish(req)
	if status == Pending {
		s.emit(ctx, notify.Event{
			Type:      notify.EvRequestCreated,
			Message:   fmt.Sprintf("%s requested %s.", p.Username, req.Release.Title),
			RequestID: id, ToAdmins: true,
			Payload: map[string]any{"requestId": id, "title": req.Release.Title, "requester": p.Username},
		})
	}
	return forViewer(p, req), nil
}

func (s *Service) Approve(ctx context.Context, p auth.Principal, id int64, o ApproveOpts) (Request, error) {
	st, err := s.s.Get(ctx)
	if err != nil {
		return Request{}, err
	}
	if st.LibraryRoot == "" {
		return Request{}, ErrNoLibraryRoot
	}
	cur, err := s.load(ctx, id)
	if err != nil {
		return Request{}, err
	}
	// A failed request can be approved again only with another release;
	// retrying the same one is what Retry is for.
	action := ActApprove
	if cur.Status == Failed {
		if o.Replacement == nil {
			return Request{}, inputError("Choose a different release, or retry this request.")
		}
		action = ActReplace
	}
	rel := cur.Release
	var replacement []byte
	if o.Replacement != nil {
		if err := validateRelease(*o.Replacement); err != nil {
			return Request{}, err
		}
		rel = *o.Replacement
		if err := s.checkNotInLibrary(ctx, rel.Key()); err != nil {
			return Request{}, err
		}
		if replacement, err = prowlarr.MarshalStored(rel); err != nil {
			return Request{}, err
		}
	}
	if err := s.checkClient(ctx, rel.Protocol); err != nil {
		return Request{}, err
	}
	title := strings.TrimSpace(o.DisplayTitle)
	if title == "" {
		title = cur.DisplayTitle
	}
	if title == "" {
		title = rel.Title
	}
	req, err := s.transition(ctx, id, action, func(u *dbq.UpdateRequestParams) {
		if replacement != nil {
			// Keep the release the requester asked for as history.
			if !u.OriginalRelease.Valid {
				u.OriginalRelease = sql.NullString{String: u.Release, Valid: true}
			}
			u.Release = string(replacement)
			u.ReleaseKey = rel.Key()
		}
		if action == ActReplace {
			// Start over so the worker grabs the new release instead of
			// re-attaching to the old download.
			u.Error = ""
			u.ClientID = sql.NullInt64{}
			u.ClientItemID = ""
			u.Progress = 0
			u.LastProgressAt = sql.NullInt64{}
		}
		u.DisplayTitle = title
		u.ApprovedBy = sql.NullInt64{Int64: p.UserID, Valid: true}
	})
	if err != nil {
		return Request{}, conflictIfInvalid(err)
	}
	s.emit(ctx, notify.Event{
		Type:      notify.EvRequestApproved,
		Message:   fmt.Sprintf("Your request for %s was approved.", req.DisplayTitle),
		RequestID: id, ToUserIDs: []int64{req.RequesterID},
		Payload: map[string]any{"requestId": id, "title": req.DisplayTitle},
	})
	return req, nil
}

func (s *Service) Deny(ctx context.Context, p auth.Principal, id int64, reason string) (Request, error) {
	reason = strings.TrimSpace(reason)
	req, err := s.transition(ctx, id, ActDeny, func(u *dbq.UpdateRequestParams) { u.DenyReason = reason })
	if err != nil {
		return Request{}, conflictIfInvalid(err)
	}
	msg := fmt.Sprintf("Your request for %s was denied.", req.Release.Title)
	if reason != "" {
		msg = fmt.Sprintf("Your request for %s was denied: %s", req.Release.Title, reason)
	}
	s.emit(ctx, notify.Event{
		Type: notify.EvRequestDenied, Message: msg, RequestID: id, ToUserIDs: []int64{req.RequesterID},
		Payload: map[string]any{"requestId": id, "title": req.Release.Title, "reason": reason},
	})
	return req, nil
}

// Cancel withdraws the caller's own pending request. Anyone else gets
// ErrNotFound, as for any request they cannot see.
func (s *Service) Cancel(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	cur, err := s.load(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if cur.RequesterID != p.UserID {
		return Request{}, ErrNotFound
	}
	req, err := s.transition(ctx, id, ActCancel, nil)
	return forViewer(p, req), conflictIfInvalid(err)
}

func (s *Service) Abandon(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	req, err := s.transition(ctx, id, ActAbandon, nil)
	return req, conflictIfInvalid(err)
}

func (s *Service) Retry(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	req, err := s.transition(ctx, id, ActRetry, func(u *dbq.UpdateRequestParams) { u.Error = "" })
	return req, conflictIfInvalid(err)
}

func (s *Service) Get(ctx context.Context, p auth.Principal, id int64) (Request, error) {
	req, err := s.load(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if !p.IsAdmin && req.RequesterID != p.UserID {
		return Request{}, ErrNotFound
	}
	return forViewer(p, req), nil
}

// forViewer hides what only admins may see.
func forViewer(p auth.Principal, r Request) Request {
	if !p.IsAdmin {
		r.ErrorDetail = ""
	}
	return r
}

// List returns the caller's requests, or everyone's when all is set and the
// caller is an admin.
func (s *Service) List(ctx context.Context, p auth.Principal, all bool) ([]Request, error) {
	var rows []dbq.GetRequestRow
	if all && p.IsAdmin {
		list, err := s.q.ListAllRequests(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			rows = append(rows, dbq.GetRequestRow(r))
		}
	} else {
		list, err := s.q.ListRequestsByUser(ctx, p.UserID)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			rows = append(rows, dbq.GetRequestRow(r))
		}
	}
	out := make([]Request, 0, len(rows))
	for _, r := range rows {
		req, err := toRequest(r)
		if err != nil {
			return nil, err
		}
		out = append(out, forViewer(p, req))
	}
	return out, nil
}

// transition applies action a to request id with a compare-and-set on the
// status it read, so concurrent changes cannot both win. mutate may adjust any
// column before the write. A request that changed underneath the caller gives
// ErrConflict; one that is missing gives ErrNotFound.
func (s *Service) transition(ctx context.Context, id int64, a Action, mutate func(*dbq.UpdateRequestParams)) (Request, error) {
	req, err := s.apply(ctx, s.q, id, a, mutate)
	if err != nil {
		return Request{}, err
	}
	s.publish(req)
	return req, nil
}

// apply is transition without the publish, using q so that it can run inside
// a caller's transaction. The caller publishes once the change is committed.
func (s *Service) apply(ctx context.Context, q *dbq.Queries, id int64, a Action, mutate func(*dbq.UpdateRequestParams)) (Request, error) {
	row, err := q.GetRequest(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, err
	}
	to, err := Next(Status(row.Status), a)
	if err != nil {
		return Request{}, err
	}
	u := dbq.UpdateRequestParams{
		Status: string(to), Release: row.Release, ReleaseKey: row.ReleaseKey, OriginalRelease: row.OriginalRelease,
		DisplayTitle: row.DisplayTitle, DenyReason: row.DenyReason, Error: row.Error, ErrorDetail: row.ErrorDetail,
		ClientID: row.ClientID, ClientItemID: row.ClientItemID, Progress: row.Progress,
		LastProgressAt: row.LastProgressAt, ApprovedBy: row.ApprovedBy, LibraryItemID: row.LibraryItemID,
		UpdatedAt: s.now().Unix(), ID: id, FromStatus: row.Status,
	}
	if mutate != nil {
		mutate(&u)
	}
	if u.Error == "" {
		u.ErrorDetail = sql.NullString{} // a cleared error takes its detail with it
	}
	n, err := q.UpdateRequest(ctx, u)
	if db.IsUniqueViolation(err) {
		return Request{}, ErrDuplicate
	}
	if err != nil {
		return Request{}, err
	}
	if n == 0 {
		if _, err := q.GetRequest(ctx, id); errors.Is(err, sql.ErrNoRows) {
			return Request{}, ErrNotFound
		} else if err != nil {
			return Request{}, err
		}
		return Request{}, ErrConflict
	}
	return loadWith(ctx, q, id)
}

// conflictIfInvalid reports a request that is no longer in a state the action
// applies to as a conflict, which is what the person acting on stale data hit.
func conflictIfInvalid(err error) error {
	if errors.Is(err, ErrInvalidTransition) {
		return ErrConflict
	}
	return err
}

func (s *Service) load(ctx context.Context, id int64) (Request, error) {
	return loadWith(ctx, s.q, id)
}

func loadWith(ctx context.Context, q *dbq.Queries, id int64) (Request, error) {
	row, err := q.GetRequest(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, err
	}
	return toRequest(row)
}

func toRequest(r dbq.GetRequestRow) (Request, error) {
	out := Request{
		ID: r.ID, RequesterID: r.RequesterID, Requester: r.Requester, Status: Status(r.Status),
		Comment: r.Comment, DisplayTitle: r.DisplayTitle, DenyReason: r.DenyReason, Error: r.Error,
		ErrorDetail: r.ErrorDetail.String, Progress: r.Progress, CreatedAt: time.Unix(r.CreatedAt, 0).UTC(), UpdatedAt: time.Unix(r.UpdatedAt, 0).UTC(),
	}
	var err error
	if out.Release, err = prowlarr.UnmarshalStored([]byte(r.Release)); err != nil {
		return Request{}, fmt.Errorf("request %d release: %w", r.ID, err)
	}
	if r.OriginalRelease.Valid {
		o, err := prowlarr.UnmarshalStored([]byte(r.OriginalRelease.String))
		if err != nil {
			return Request{}, fmt.Errorf("request %d original release: %w", r.ID, err)
		}
		out.OriginalRelease = &o
	}
	if r.LibraryItemID.Valid {
		out.LibraryItemID = &r.LibraryItemID.Int64
	}
	return out, nil
}

// publish tells the requester and admins the request changed.
func (s *Service) publish(r Request) {
	s.b.Publish(events.Message{
		Type:    events.TypeRequestUpdated,
		Data:    map[string]any{"id": r.ID, "status": r.Status, "progress": r.Progress},
		UserIDs: []int64{r.RequesterID},
		Admins:  true,
	})
}

// emit sends a notification that outlives a cancelled HTTP request.
func (s *Service) emit(ctx context.Context, e notify.Event) {
	s.n.Emit(context.WithoutCancel(ctx), e)
}
