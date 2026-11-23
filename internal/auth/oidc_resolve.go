package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/settings"
)

// Claims is what softwarr reads from a verified ID token.
type Claims struct {
	Issuer, Subject, Email, PreferredUsername, Name string
	EmailVerified                                   bool
	Raw                                             map[string]any
}

// Outcome says what ResolveLogin did.
type Outcome int

const (
	OutcomeLogin   Outcome = iota // an existing account signs in
	OutcomeCreated                // a new active account was created
	OutcomePending                // the account exists but waits for approval
	OutcomeLinked                 // the identity was connected to a signed-in local account
)

var (
	ErrEmailTaken       = errors.New("An account with this email already exists. Sign in with your password, then connect SSO from your profile.")
	ErrAwaitingApproval = errors.New("Your account is waiting for an admin to approve it.")
	ErrOwnerNoOIDC      = errors.New("The owner account can't be connected to SSO.")
	ErrIdentityInUse    = errors.New("That SSO account is already connected to another user.")
	ErrAlreadyLinked    = errors.New("This account is already connected to SSO.")
	ErrNotLocal         = errors.New("Only accounts with a password can connect SSO.")
)

const maxUsernameTries = 1000

// ResolveLogin maps a verified identity to an account. With linkUserID set it
// connects the identity to that (signed-in, local) account instead. Email is
// only used to match an account when cfg.LinkVerifiedEmail is on, the
// provider says the email is verified, and the account isn't the owner.
//
// q should be bound to a transaction. When the outcome is OutcomePending the
// error is ErrAwaitingApproval and the new account must still be committed;
// any other error means nothing should be committed.
func ResolveLogin(ctx context.Context, q *dbq.Queries, cfg settings.OIDC, c Claims, linkUserID *int64) (userID int64, out Outcome, err error) {
	if c.Issuer == "" || c.Subject == "" {
		return 0, 0, errors.New("oidc: identity without issuer or subject")
	}
	if linkUserID != nil {
		return linkIdentity(ctx, q, c, *linkUserID)
	}

	ident, err := q.GetIdentity(ctx, dbq.GetIdentityParams{Issuer: c.Issuer, Subject: c.Subject})
	switch {
	case err == nil:
		return loginExisting(ctx, q, cfg, c, ident.UserID)
	case !errors.Is(err, sql.ErrNoRows):
		return 0, 0, err
	}

	if email := strings.TrimSpace(c.Email); email != "" {
		row, err := q.GetUserByEmail(ctx, sql.NullString{String: email, Valid: true})
		switch {
		case err == nil:
			return linkByEmail(ctx, q, cfg, c, row)
		case !errors.Is(err, sql.ErrNoRows):
			return 0, 0, err
		}
	}

	return provision(ctx, q, cfg, c)
}

func loginExisting(ctx context.Context, q *dbq.Queries, cfg settings.OIDC, c Claims, id int64) (int64, Outcome, error) {
	row, err := q.GetUserByID(ctx, id)
	if err != nil {
		return 0, 0, err
	}
	if row.IsOwner != 0 {
		// The owner can never hold an identity; refuse if one somehow exists.
		return 0, 0, ErrOwnerNoOIDC
	}
	switch row.Status {
	case "active":
	case "pending":
		return row.ID, OutcomePending, ErrAwaitingApproval
	default:
		return 0, 0, ErrAccountInactive
	}
	if err := syncAdmin(ctx, q, cfg, c, row.ID); err != nil {
		return 0, 0, err
	}
	return row.ID, OutcomeLogin, nil
}

func linkByEmail(ctx context.Context, q *dbq.Queries, cfg settings.OIDC, c Claims, row dbq.User) (int64, Outcome, error) {
	if !cfg.LinkVerifiedEmail || !c.EmailVerified || row.IsOwner != 0 {
		return 0, 0, ErrEmailTaken
	}
	linked, err := q.UserHasIdentity(ctx, row.ID)
	if err != nil {
		return 0, 0, err
	}
	if linked {
		return 0, 0, ErrEmailTaken
	}
	switch row.Status {
	case "active":
	case "pending":
		return 0, 0, ErrAwaitingApproval
	default:
		return 0, 0, ErrAccountInactive
	}
	if err := insertIdentity(ctx, q, row.ID, c); err != nil {
		return 0, 0, err
	}
	if err := syncAdmin(ctx, q, cfg, c, row.ID); err != nil {
		return 0, 0, err
	}
	return row.ID, OutcomeLogin, nil
}

func provision(ctx context.Context, q *dbq.Queries, cfg settings.OIDC, c Claims) (int64, Outcome, error) {
	name, err := pickUsername(ctx, q, c)
	if err != nil {
		return 0, 0, err
	}
	status := "pending"
	if cfg.AutoProvision {
		status = "active"
	}
	// Only keep an email the provider vouches for. A stored email is what
	// "link by verified email" and the email-taken check match on later.
	var email sql.NullString
	if c.EmailVerified {
		email = nullEmail(c.Email)
	}
	row, err := q.CreateUser(ctx, dbq.CreateUserParams{
		Username:  name,
		Email:     email,
		Status:    status,
		CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		return 0, 0, mapUnique(err)
	}
	if err := insertIdentity(ctx, q, row.ID, c); err != nil {
		return 0, 0, err
	}
	if status == "pending" {
		return row.ID, OutcomePending, ErrAwaitingApproval
	}
	if err := syncAdmin(ctx, q, cfg, c, row.ID); err != nil {
		return 0, 0, err
	}
	return row.ID, OutcomeCreated, nil
}

func linkIdentity(ctx context.Context, q *dbq.Queries, c Claims, userID int64) (int64, Outcome, error) {
	if _, err := checkLinkTarget(ctx, q, userID); err != nil {
		return 0, 0, err
	}
	_, err := q.GetIdentity(ctx, dbq.GetIdentityParams{Issuer: c.Issuer, Subject: c.Subject})
	switch {
	case err == nil:
		// checkLinkTarget guarantees the target has no identity, so this one
		// belongs to someone else.
		return 0, 0, ErrIdentityInUse
	case !errors.Is(err, sql.ErrNoRows):
		return 0, 0, err
	}
	if err := insertIdentity(ctx, q, userID, c); err != nil {
		return 0, 0, err
	}
	return userID, OutcomeLinked, nil
}

// checkLinkTarget checks that userID may connect an identity: an active,
// local account that isn't the owner and isn't connected yet.
func checkLinkTarget(ctx context.Context, q *dbq.Queries, userID int64) (dbq.User, error) {
	row, err := q.GetUserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return dbq.User{}, ErrNotFound
	}
	if err != nil {
		return dbq.User{}, err
	}
	if row.IsOwner != 0 {
		return dbq.User{}, ErrOwnerNoOIDC
	}
	if row.Status != "active" {
		return dbq.User{}, ErrAccountInactive
	}
	linked, err := q.UserHasIdentity(ctx, row.ID)
	if err != nil {
		return dbq.User{}, err
	}
	if linked {
		return dbq.User{}, ErrAlreadyLinked
	}
	if row.PasswordHash == "" {
		return dbq.User{}, ErrNotLocal
	}
	return row, nil
}

func insertIdentity(ctx context.Context, q *dbq.Queries, userID int64, c Claims) error {
	err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{
		UserID: userID, Issuer: c.Issuer, Subject: c.Subject, CreatedAt: time.Now().Unix(),
	})
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		if strings.Contains(err.Error(), "user_identities.user_id") {
			return ErrAlreadyLinked
		}
		return ErrIdentityInUse
	}
	return err
}

func syncAdmin(ctx context.Context, q *dbq.Queries, cfg settings.OIDC, c Claims, userID int64) error {
	isAdmin, applies := IsAdminByClaim(cfg, c.Raw)
	if !applies {
		return nil
	}
	return q.SetUserAdminFromClaim(ctx, dbq.SetUserAdminFromClaimParams{IsAdmin: b2i(isAdmin), ID: userID})
}

// IsAdminByClaim reports whether raw grants admin under the configured claim.
// The claim may hold a string or an array of strings. applies is false when
// no admin claim is configured.
func IsAdminByClaim(cfg settings.OIDC, raw map[string]any) (isAdmin bool, applies bool) {
	claim := strings.TrimSpace(cfg.AdminClaim)
	if claim == "" {
		return false, false
	}
	want := strings.TrimSpace(cfg.AdminValue)
	if want == "" {
		return false, true
	}
	switch v := raw[claim].(type) {
	case string:
		return v == want, true
	case []string:
		return slices.Contains(v, want), true
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true, true
			}
		}
	}
	return false, true
}

// pickUsername derives a free username from the claims: the preferred
// username, else the email's local part, else "user-" and the start of the
// subject, with "-2", "-3", … added on collision.
func pickUsername(ctx context.Context, q *dbq.Queries, c Claims) (string, error) {
	base := strings.TrimSpace(c.PreferredUsername)
	if base == "" {
		if local, _, ok := strings.Cut(strings.TrimSpace(c.Email), "@"); ok {
			base = strings.TrimSpace(local)
		}
	}
	if base == "" {
		base = "user-" + truncateRunes(c.Subject, 8)
	}
	for n := 1; n <= maxUsernameTries; n++ {
		name := truncateRunes(base, maxUsernameLen)
		if n > 1 {
			suffix := fmt.Sprintf("-%d", n)
			name = truncateRunes(base, maxUsernameLen-len(suffix)) + suffix
		}
		_, err := q.GetUserByUsername(ctx, name)
		if errors.Is(err, sql.ErrNoRows) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("oidc: no free username for %q", base)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
