package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

var ErrNoOwner = errors.New("No owner account exists yet. Open softwarr in a browser to run setup.")

const (
	resetPasswordLen      = 20
	resetPasswordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

func randomPassword() (string, error) {
	max := big.NewInt(int64(len(resetPasswordAlphabet)))
	out := make([]byte, resetPasswordLen)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = resetPasswordAlphabet[n.Int64()]
	}
	return string(out), nil
}

// ResetOwnerPassword gives the owner a new random password, requires a change
// at next sign-in and signs the owner out everywhere, all in one transaction.
func ResetOwnerPassword(ctx context.Context, db *sql.DB, q *dbq.Queries) (string, error) {
	pw, err := randomPassword()
	if err != nil {
		return "", err
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	tq := q.WithTx(tx)
	owner, err := tq.GetOwner(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoOwner
	}
	if err != nil {
		return "", err
	}
	if err := tq.SetUserTemporaryPassword(ctx, dbq.SetUserTemporaryPasswordParams{PasswordHash: hash, ID: owner.ID}); err != nil {
		return "", err
	}
	if err := tq.DeleteSessionsForUser(ctx, owner.ID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return pw, nil
}
