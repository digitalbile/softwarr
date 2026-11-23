package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
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

// Lookup returns the session's user and slides the expiry forward when less
// than half the lifetime remains.
func (s *Sessions) Lookup(ctx context.Context, token string) (int64, error) {
	h := hashToken(token)
	row, err := s.q.GetSession(ctx, h)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoSession
	}
	if err != nil {
		return 0, err
	}
	now := s.now()
	expires := time.Unix(row.ExpiresAt, 0)
	if !expires.After(now) {
		_ = s.q.DeleteSession(ctx, h)
		return 0, ErrNoSession
	}
	if expires.Sub(now) < sessionExtendAt {
		if err := s.q.ExtendSession(ctx, dbq.ExtendSessionParams{ExpiresAt: now.Add(sessionLifetime).Unix(), TokenHash: h}); err != nil {
			return 0, err
		}
	}
	return row.UserID, nil
}

func (s *Sessions) Delete(ctx context.Context, token string) error {
	return s.q.DeleteSession(ctx, hashToken(token))
}

func (s *Sessions) DeleteForUser(ctx context.Context, userID int64) error {
	return s.q.DeleteSessionsForUser(ctx, userID)
}
