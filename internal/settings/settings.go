// Package settings stores the application settings as one JSON document.
package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

const settingsKey = "app"

type Settings struct {
	ExternalURL          string   `json:"externalUrl"`
	APIKey               string   `json:"apiKey"`
	CommentRequired      bool     `json:"commentRequired"`
	DefaultRetentionDays int      `json:"defaultRetentionDays"` // 0 = never
	StallHours           int      `json:"stallHours"`
	LibraryRoot          string   `json:"libraryRoot"`
	Prowlarr             Prowlarr `json:"prowlarr"`
	OIDC                 OIDC     `json:"oidc"`
}

type Prowlarr struct {
	URL        string `json:"url"`
	APIKey     string `json:"apiKey"`
	Categories []int  `json:"categories"`
}

type OIDC struct {
	Enabled           bool     `json:"enabled"`
	Issuer            string   `json:"issuer"`
	ClientID          string   `json:"clientId"`
	ClientSecret      string   `json:"clientSecret"`
	ButtonLabel       string   `json:"buttonLabel"`
	Scopes            []string `json:"scopes"`
	AutoProvision     bool     `json:"autoProvision"`
	LinkVerifiedEmail bool     `json:"linkVerifiedEmail"`
	AdminClaim        string   `json:"adminClaim"`
	AdminValue        string   `json:"adminValue"`
}

func defaults() Settings {
	return Settings{
		StallHours: 24,
		Prowlarr:   Prowlarr{Categories: []int{4000, 4010, 4020, 4030}},
		OIDC: OIDC{
			ButtonLabel:   "Sign in with SSO",
			Scopes:        []string{"openid", "profile", "email"},
			AutoProvision: true,
		},
	}
}

func (s Settings) clone() Settings {
	s.Prowlarr.Categories = slices.Clone(s.Prowlarr.Categories)
	s.OIDC.Scopes = slices.Clone(s.OIDC.Scopes)
	return s
}

// Store loads and saves settings, caching them in memory. It is safe for
// concurrent use.
type Store struct {
	q      *dbq.Queries
	mu     sync.Mutex
	cached *Settings
}

func NewStore(q *dbq.Queries) *Store { return &Store{q: q} }

// Get returns a copy of the current settings.
func (s *Store) Get(ctx context.Context) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached == nil {
		if err := s.load(ctx); err != nil {
			return Settings{}, err
		}
	}
	return s.cached.clone(), nil
}

// Save validates and persists settings, then updates the cache. An empty
// APIKey keeps the current one.
func (s *Store) Save(ctx context.Context, in Settings) error {
	in = in.clone()
	in.ExternalURL = strings.TrimRight(strings.TrimSpace(in.ExternalURL), "/")
	if err := validate(in); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.APIKey == "" {
		if s.cached == nil {
			if err := s.load(ctx); err != nil {
				return err
			}
		}
		in.APIKey = s.cached.APIKey
	}
	if err := s.persist(ctx, in); err != nil {
		return err
	}
	s.cached = &in
	return nil
}

// inputError is a validation failure that is safe to show to the user.
type inputError string

func (e inputError) Error() string { return string(e) }

func errInput(msg string) error { return inputError(msg) }

// IsInputError reports whether err is a validation failure from Save.
func IsInputError(err error) bool {
	var ie inputError
	return errors.As(err, &ie)
}

func validate(in Settings) error {
	if in.OIDC.Enabled && in.ExternalURL == "" {
		return errInput("Set the external URL before enabling OIDC.")
	}
	if in.DefaultRetentionDays < 0 {
		return errInput("Retention days cannot be negative.")
	}
	if in.StallHours < 0 {
		return errInput("Stall hours cannot be negative.")
	}
	return nil
}

// load reads stored settings over the defaults and generates the API key on
// first use. The caller holds s.mu.
func (s *Store) load(ctx context.Context) error {
	cur := defaults()
	raw, err := s.q.GetSetting(ctx, settingsKey)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal([]byte(raw), &cur); err != nil {
			return err
		}
	}
	if cur.APIKey == "" {
		key, err := newAPIKey()
		if err != nil {
			return err
		}
		cur.APIKey = key
		if err := s.persist(ctx, cur); err != nil {
			return err
		}
	}
	s.cached = &cur
	return nil
}

func (s *Store) persist(ctx context.Context, v Settings) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.q.UpsertSetting(ctx, dbq.UpsertSettingParams{Key: settingsKey, Value: string(b)})
}

// RegenerateAPIKey replaces the API key with a new random one and returns it.
func (s *Store) RegenerateAPIKey(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached == nil {
		if err := s.load(ctx); err != nil {
			return "", err
		}
	}
	key, err := newAPIKey()
	if err != nil {
		return "", err
	}
	next := s.cached.clone()
	next.APIKey = key
	if err := s.persist(ctx, next); err != nil {
		return "", err
	}
	s.cached = &next
	return key, nil
}

func newAPIKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
