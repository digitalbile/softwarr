package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

const (
	sessionLifetime = 30 * 24 * time.Hour
	sessionExtendAt = 15 * 24 * time.Hour
)

// ErrNoSession means the token is unknown or expired.
var ErrNoSession = errors.New("no such session")

type Sessions struct {
	q   *dbq.Queries
	now func() time.Time
}

func NewSessions(q *dbq.Queries, now func() time.Time) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{q: q, now: now}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Sessions) Create(ctx context.Context, userID int64) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := s.now()
	expires := now.Add(sessionLifetime)
	err := s.q.CreateSession(ctx, dbq.CreateSessionParams{
		TokenHash: hashToken(token), UserID: userID, CreatedAt: now.Unix(), ExpiresAt: expires.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Lookup returns the session's user and expiry. It slides the expiry forward
// when less than half the lifetime remains, reporting that as extended.
// Unknown and expired tokens return ErrNoSession.
func (s *Sessions) Lookup(ctx context.Context, token string) (userID int64, expires time.Time, extended bool, err error) {
	h := hashToken(token)
	row, err := s.q.GetSession(ctx, h)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false, ErrNoSession
	}
	if err != nil {
		return 0, time.Time{}, false, err
	}
	now := s.now()
	expires = time.Unix(row.ExpiresAt, 0)
	if !expires.After(now) {
		_ = s.q.DeleteSession(ctx, h)
		return 0, time.Time{}, false, ErrNoSession
	}
	if expires.Sub(now) < sessionExtendAt {
		expires = now.Add(sessionLifetime)
		if err := s.q.ExtendSession(ctx, dbq.ExtendSessionParams{ExpiresAt: expires.Unix(), TokenHash: h}); err != nil {
			return 0, time.Time{}, false, err
		}
		extended = true
	}
	return row.UserID, expires, extended, nil
}

func (s *Sessions) Delete(ctx context.Context, token string) error {
	return s.q.DeleteSession(ctx, hashToken(token))
}

func (s *Sessions) DeleteForUser(ctx context.Context, userID int64) error {
	return s.q.DeleteSessionsForUser(ctx, userID)
}

// DeleteExpired removes sessions past their expiry. Lookup already refuses
// them; this keeps the table from growing.
func (s *Sessions) DeleteExpired(ctx context.Context) error {
	return s.q.DeleteExpiredSessions(ctx, s.now().Unix())
}

// RunPurge deletes expired sessions now and then every interval until ctx is
// done.
func (s *Sessions) RunPurge(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.DeleteExpired(ctx); err != nil && ctx.Err() == nil {
			slog.Error("delete expired sessions", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
