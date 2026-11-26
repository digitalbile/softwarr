package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/settings"
)

// CallbackPath is the OIDC redirect path, appended to the external URL.
const CallbackPath = "/api/v1/auth/oidc/callback"

const (
	oidcStateLifetime = 10 * time.Minute
	oidcHTTPTimeout   = 15 * time.Second
)

var (
	ErrOIDCDisabled     = errors.New("SSO sign-in isn't turned on.")
	ErrSetupNeeded      = errors.New("Finish setup before signing in with SSO.")
	ErrOIDCStateInvalid = errors.New("This sign-in attempt expired or was already used. Try again.")
	ErrOIDCFailed       = errors.New("Signing in with SSO didn't work. Try again.")
	ErrOIDCUnavailable  = errors.New("Can't reach the SSO provider right now. Try again later.")

	ErrPasswordSignInOff = errors.New("Password sign-in is turned off. Sign in with SSO.")
)

// OIDCErrorMessage returns the message to show for an error from Begin or
// Callback, and whether err is one of the expected, user-facing outcomes.
// Anything else should be logged and shown as ErrOIDCFailed.
func OIDCErrorMessage(err error) (string, bool) {
	for _, e := range []error{
		ErrOIDCDisabled, ErrSetupNeeded, ErrOIDCStateInvalid, ErrOIDCFailed, ErrOIDCUnavailable, ErrPasswordSignInOff,
		ErrEmailTaken, ErrAwaitingApproval, ErrOwnerNoOIDC, ErrIdentityInUse, ErrAlreadyLinked,
		ErrNotLocal, ErrAccountInactive, ErrNotFound,
	} {
		if errors.Is(err, e) {
			return e.Error(), true
		}
	}
	return ErrOIDCFailed.Error(), false
}

// OIDC runs the authorization code flow with PKCE against the configured
// provider.
type OIDC struct {
	settings *settings.Store
	q        *dbq.Queries
	db       *sql.DB
	now      func() time.Time
	client   *http.Client

	mu       sync.Mutex
	issuer   string
	provider *oidc.Provider
}

func NewOIDC(st *settings.Store, q *dbq.Queries, db *sql.DB, now func() time.Time) *OIDC {
	if now == nil {
		now = time.Now
	}
	return &OIDC{settings: st, q: q, db: db, now: now, client: &http.Client{Timeout: oidcHTTPTimeout}}
}

// config returns the settings when OIDC is usable: enabled, configured, and
// setup finished.
func (o *OIDC) config(ctx context.Context) (settings.Settings, error) {
	st, err := o.settings.Get(ctx)
	if err != nil {
		return settings.Settings{}, err
	}
	if !st.OIDC.Enabled || st.OIDC.Issuer == "" || st.ExternalURL == "" {
		return settings.Settings{}, ErrOIDCDisabled
	}
	n, err := o.q.CountUsers(ctx)
	if err != nil {
		return settings.Settings{}, err
	}
	if n == 0 {
		return settings.Settings{}, ErrSetupNeeded
	}
	return st, nil
}

// localAuthOff reports whether "Disable local auth" is on and OIDC is
// configured well enough for people to sign in with it.
func localAuthOff(st settings.Settings) bool {
	return st.OIDC.Enabled && st.OIDC.Issuer != "" && st.ExternalURL != "" && st.OIDC.DisableLocalAuth
}

// LocalAuthDisabled reports whether password sign-in is limited to the owner.
// It is false whenever SSO can't be used, so nobody is left without a way in.
func (o *OIDC) LocalAuthDisabled(ctx context.Context) (bool, error) {
	st, err := o.config(ctx)
	if errors.Is(err, ErrOIDCDisabled) || errors.Is(err, ErrSetupNeeded) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.OIDC.DisableLocalAuth, nil
}

// CheckPasswordLogin returns ErrPasswordSignInOff when u, having given the
// right password, may still not sign in with it. Only the owner always can.
func (o *OIDC) CheckPasswordLogin(ctx context.Context, u User) error {
	if u.IsOwner {
		return nil
	}
	off, err := o.LocalAuthDisabled(ctx)
	if err != nil {
		return err
	}
	if off {
		return ErrPasswordSignInOff
	}
	return nil
}

// SessionAllowed reports whether u's session may continue. While local auth
// is disabled, accounts other than the owner need an SSO identity, so a
// password session can't outlive the switch. It reads only cached settings.
func (o *OIDC) SessionAllowed(ctx context.Context, u User) (bool, error) {
	if u.IsOwner || u.HasOIDC {
		return true, nil
	}
	st, err := o.settings.Get(ctx)
	if err != nil {
		return false, err
	}
	return !localAuthOff(st), nil
}

// providerFor returns the discovered provider for issuer, reusing the cached
// one unless the issuer changed.
func (o *OIDC) providerFor(ctx context.Context, issuer string) (*oidc.Provider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil && o.issuer == issuer {
		return o.provider, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, o.client), issuer)
	if err != nil {
		return nil, fmt.Errorf("discover provider: %v: %w", err, ErrOIDCUnavailable)
	}
	o.issuer, o.provider = issuer, p
	return p, nil
}

func oauthConfig(st settings.Settings, p *oidc.Provider) *oauth2.Config {
	scopes := slices.Clone(st.OIDC.Scopes)
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return &oauth2.Config{
		ClientID:     st.OIDC.ClientID,
		ClientSecret: st.OIDC.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  st.ExternalURL + CallbackPath,
		Scopes:       scopes,
	}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Begin starts a sign-in, or with linkUserID a link for that signed-in user,
// and returns the provider URL to send the browser to. The state, PKCE
// verifier and nonce are stored for ten minutes; the state is the "state"
// query parameter of the returned URL.
func (o *OIDC) Begin(ctx context.Context, linkUserID *int64) (authURL string, err error) {
	st, err := o.config(ctx)
	if err != nil {
		return "", err
	}
	link := sql.NullInt64{}
	if linkUserID != nil {
		if _, err := checkLinkTarget(ctx, o.q, *linkUserID); err != nil {
			return "", err
		}
		link = sql.NullInt64{Int64: *linkUserID, Valid: true}
	}
	p, err := o.providerFor(ctx, st.OIDC.Issuer)
	if err != nil {
		return "", err
	}
	now := o.now()
	if err := o.q.DeleteExpiredOIDCStates(ctx, now.Unix()); err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	err = o.q.CreateOIDCState(ctx, dbq.CreateOIDCStateParams{
		State: state, CodeVerifier: verifier, Nonce: nonce, LinkUserID: link,
		ExpiresAt: now.Add(oidcStateLifetime).Unix(),
	})
	if err != nil {
		return "", err
	}
	return oauthConfig(st, p).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)), nil
}

// Discard removes a pending state without completing it.
func (o *OIDC) Discard(ctx context.Context, state string) error {
	if state == "" {
		return nil
	}
	_, err := o.q.ConsumeOIDCState(ctx, state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

type idTokenClaims struct {
	Email             string `json:"email"`
	EmailVerified     any    `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
}

// emailVerified accepts a boolean or the string "true", which some providers send.
func emailVerified(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

// Callback completes a sign-in: it consumes the state (single use), exchanges
// the code with the PKCE verifier, verifies the ID token and its nonce, and
// resolves the account in a transaction. OutcomeLinked means the state's link
// user was connected; the caller must not start a session for it.
func (o *OIDC) Callback(ctx context.Context, state, code string) (userID int64, out Outcome, err error) {
	if state == "" {
		return 0, 0, ErrOIDCStateInvalid
	}
	row, err := o.q.ConsumeOIDCState(ctx, state)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrOIDCStateInvalid
	}
	if err != nil {
		return 0, 0, err
	}
	if row.ExpiresAt <= o.now().Unix() {
		return 0, 0, ErrOIDCStateInvalid
	}
	st, err := o.config(ctx)
	if err != nil {
		return 0, 0, err
	}
	if code == "" {
		return 0, 0, ErrOIDCFailed
	}
	p, err := o.providerFor(ctx, st.OIDC.Issuer)
	if err != nil {
		return 0, 0, err
	}

	cctx := oidc.ClientContext(ctx, o.client)
	tok, err := oauthConfig(st, p).Exchange(cctx, code, oauth2.VerifierOption(row.CodeVerifier))
	if err != nil {
		return 0, 0, fmt.Errorf("exchange code: %v: %w", err, ErrOIDCFailed)
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		return 0, 0, fmt.Errorf("no id_token in token response: %w", ErrOIDCFailed)
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: st.OIDC.ClientID}).Verify(cctx, rawID)
	if err != nil {
		return 0, 0, fmt.Errorf("verify id_token: %v: %w", err, ErrOIDCFailed)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(row.Nonce)) != 1 {
		return 0, 0, fmt.Errorf("id_token nonce mismatch: %w", ErrOIDCFailed)
	}
	var known idTokenClaims
	raw := map[string]any{}
	if err := idt.Claims(&known); err != nil {
		return 0, 0, fmt.Errorf("read claims: %v: %w", err, ErrOIDCFailed)
	}
	if err := idt.Claims(&raw); err != nil {
		return 0, 0, fmt.Errorf("read claims: %v: %w", err, ErrOIDCFailed)
	}
	if idt.Subject == "" {
		return 0, 0, fmt.Errorf("id_token without sub: %w", ErrOIDCFailed)
	}
	c := Claims{
		Issuer: idt.Issuer, Subject: idt.Subject, Email: known.Email,
		PreferredUsername: known.PreferredUsername, Name: known.Name,
		EmailVerified: emailVerified(known.EmailVerified), Raw: raw,
	}
	var link *int64
	if row.LinkUserID.Valid {
		link = &row.LinkUserID.Int64
	}

	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	userID, out, err = ResolveLogin(ctx, o.q.WithTx(tx), st.OIDC, c, link)
	if err != nil && out != OutcomePending {
		return 0, 0, err
	}
	if cerr := tx.Commit(); cerr != nil {
		return 0, 0, cerr
	}
	return userID, out, err
}
