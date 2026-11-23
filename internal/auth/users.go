package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

var (
	ErrOwnerProtected     = errors.New("The owner account can't be changed this way.")
	ErrSetupDone          = errors.New("Setup has already been completed.")
	ErrInvalidCredentials = errors.New("Wrong username or password.")
	ErrAccountInactive    = errors.New("This account isn't active. Ask an admin for help.")
	ErrNotFound           = errors.New("That user doesn't exist.")

	errUsernameRequired = errors.New("A username is required.")
	errUsernameTooLong  = errors.New("Usernames can be at most 64 characters.")
	errPasswordTooShort = errors.New("Passwords need at least 8 characters.")
	errUsernameTaken    = errors.New("That username is already taken.")
	errEmailTaken       = errors.New("That email is already in use.")
	errBadStatus        = errors.New("Status must be active, pending, or disabled.")
)

const (
	minPasswordLen = 8
	maxUsernameLen = 64
)

type User struct {
	ID                                                                      int64
	Username, Email                                                         string
	IsOwner, IsAdmin, AutoApprove, MustChangePassword, HasPassword, HasOIDC bool
	Status                                                                  string
}

type UserPatch struct {
	IsAdmin, AutoApprove *bool
	Status               *string
	Email                *string
}

type Users struct {
	db *sql.DB
	q  *dbq.Queries
}

func NewUsers(db *sql.DB, q *dbq.Queries) *Users { return &Users{db: db, q: q} }

var (
	dummyOnce sync.Once
	dummyHash string
)

// burnPassword spends roughly the time of a real verification so that
// unknown usernames can't be told apart by response time.
func burnPassword(pw string) {
	dummyOnce.Do(func() { dummyHash, _ = HashPassword("softwarr-dummy") })
	VerifyPassword(dummyHash, pw)
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (u *Users) toUser(ctx context.Context, q *dbq.Queries, r dbq.User) (User, error) {
	hasOIDC, err := q.UserHasIdentity(ctx, r.ID)
	if err != nil {
		return User{}, err
	}
	return User{
		ID:                 r.ID,
		Username:           r.Username,
		Email:              r.Email.String,
		IsOwner:            r.IsOwner != 0,
		IsAdmin:            r.IsAdmin != 0,
		AutoApprove:        r.AutoApprove != 0,
		MustChangePassword: r.MustChangePassword != 0,
		HasPassword:        r.PasswordHash != "",
		HasOIDC:            hasOIDC,
		Status:             r.Status,
	}, nil
}

func validateUsername(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errUsernameRequired
	}
	if utf8.RuneCountInString(name) > maxUsernameLen {
		return "", errUsernameTooLong
	}
	return name, nil
}

func validatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < minPasswordLen {
		return errPasswordTooShort
	}
	return nil
}

func nullEmail(email string) sql.NullString {
	email = strings.TrimSpace(email)
	return sql.NullString{String: email, Valid: email != ""}
}

// mapUnique turns UNIQUE constraint failures into user-facing errors.
func mapUnique(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "UNIQUE constraint failed") {
		return err
	}
	switch {
	case strings.Contains(msg, "users.email") || strings.Contains(msg, "users_email_unique"):
		return errEmailTaken
	case strings.Contains(msg, "users.username"):
		return errUsernameTaken
	case strings.Contains(msg, "is_owner") || strings.Contains(msg, "users_single_owner"):
		return ErrSetupDone
	}
	return err
}

func (u *Users) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := u.q.CountUsers(ctx)
	return n == 0, err
}

func (u *Users) create(ctx context.Context, q *dbq.Queries, username, email, password string, owner, admin bool) (User, error) {
	name, err := validateUsername(username)
	if err != nil {
		return User{}, err
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	row, err := q.CreateUser(ctx, dbq.CreateUserParams{
		Username:     name,
		Email:        nullEmail(email),
		PasswordHash: hash,
		IsOwner:      b2i(owner),
		IsAdmin:      b2i(admin),
		Status:       "active",
		CreatedAt:    time.Now().Unix(),
	})
	if err != nil {
		return User{}, mapUnique(err)
	}
	return u.toUser(ctx, q, row)
}

// Setup creates the owner account. It only succeeds while no users exist.
func (u *Users) Setup(ctx context.Context, username, password string) (User, error) {
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	q := u.q.WithTx(tx)
	n, err := q.CountUsers(ctx)
	if err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, ErrSetupDone
	}
	user, err := u.create(ctx, q, username, "", password, true, true)
	if err != nil {
		return User{}, err
	}
	return user, tx.Commit()
}

func (u *Users) CreateLocal(ctx context.Context, username, email, password string, isAdmin bool) (User, error) {
	return u.create(ctx, u.q, username, email, password, false, isAdmin)
}

func (u *Users) Authenticate(ctx context.Context, username, password string) (User, error) {
	row, err := u.q.GetUserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, sql.ErrNoRows) {
		burnPassword(password)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if row.PasswordHash == "" {
		burnPassword(password)
		return User{}, ErrInvalidCredentials
	}
	if !VerifyPassword(row.PasswordHash, password) {
		return User{}, ErrInvalidCredentials
	}
	if row.Status != "active" {
		return User{}, ErrAccountInactive
	}
	return u.toUser(ctx, u.q, row)
}

func (u *Users) Get(ctx context.Context, id int64) (User, error) {
	row, err := u.q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u.toUser(ctx, u.q, row)
}

func (u *Users) List(ctx context.Context) ([]User, error) {
	rows, err := u.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(rows))
	for _, r := range rows {
		user, err := u.toUser(ctx, u.q, r)
		if err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, nil
}

// Update applies a patch. The owner can't lose admin or leave the active status.
func (u *Users) Update(ctx context.Context, id int64, p UserPatch) (User, error) {
	if p.Status != nil {
		switch *p.Status {
		case "active", "pending", "disabled":
		default:
			return User{}, errBadStatus
		}
	}
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	q := u.q.WithTx(tx)
	row, err := q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if row.IsOwner != 0 {
		if (p.IsAdmin != nil && !*p.IsAdmin) || (p.Status != nil && *p.Status != "active") {
			return User{}, ErrOwnerProtected
		}
	}
	args := dbq.UpdateUserFieldsParams{
		ID: id, IsAdmin: row.IsAdmin, AutoApprove: row.AutoApprove, Status: row.Status, Email: row.Email,
	}
	if p.IsAdmin != nil {
		args.IsAdmin = b2i(*p.IsAdmin)
	}
	if p.AutoApprove != nil {
		args.AutoApprove = b2i(*p.AutoApprove)
	}
	if p.Status != nil {
		args.Status = *p.Status
	}
	if p.Email != nil {
		args.Email = nullEmail(*p.Email)
	}
	if err := q.UpdateUserFields(ctx, args); err != nil {
		return User{}, mapUnique(err)
	}
	row, err = q.GetUserByID(ctx, id)
	if err != nil {
		return User{}, err
	}
	user, err := u.toUser(ctx, q, row)
	if err != nil {
		return User{}, err
	}
	return user, tx.Commit()
}

func (u *Users) Delete(ctx context.Context, id int64) error {
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := u.q.WithTx(tx)
	row, err := q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.IsOwner != 0 {
		return ErrOwnerProtected
	}
	if err := q.DeleteUser(ctx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (u *Users) ChangePassword(ctx context.Context, id int64, current, next string) error {
	row, err := u.q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.PasswordHash == "" || !VerifyPassword(row.PasswordHash, current) {
		return ErrInvalidCredentials
	}
	if err := validatePassword(next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return u.q.SetUserPassword(ctx, dbq.SetUserPasswordParams{PasswordHash: hash, ID: id})
}
