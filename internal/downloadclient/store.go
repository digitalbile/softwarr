package downloadclient

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/digitalbile/softwarr/internal/db"
	"github.com/digitalbile/softwarr/internal/db/dbq"
)

var ErrNotFound = errors.New("That download client doesn't exist.")

// PasswordMask stands in for a saved password in the API. Saving it back
// keeps the stored password.
const PasswordMask = "********"

// Saved is the API view of a stored client, with any password masked.
type Saved struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Protocol string         `json:"protocol"`
	Enabled  bool           `json:"enabled"`
	Priority int64          `json:"priority"`
	Settings map[string]any `json:"settings"`
}

// Input is the body for creating, updating, or testing a client. Enabled
// defaults to true.
type Input struct {
	ID       int64           `json:"id"` // only used when testing
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Enabled  *bool           `json:"enabled"`
	Priority int             `json:"priority"`
	Settings json.RawMessage `json:"settings"`
}

// Store reads and writes the saved clients.
type Store struct{ q *dbq.Queries }

func NewStore(q *dbq.Queries) *Store { return &Store{q: q} }

func toSaved(c dbq.DownloadClient) Saved {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(c.Settings), &m)
	if pw, _ := m["password"].(string); pw != "" {
		m["password"] = PasswordMask
	}
	return Saved{ID: c.ID, Name: c.Name, Type: c.Type, Protocol: c.Protocol, Enabled: c.Enabled != 0, Priority: c.Priority, Settings: m}
}

func (s *Store) List(ctx context.Context) ([]Saved, error) {
	rows, err := s.q.ListDownloadClients(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Saved, 0, len(rows))
	for _, c := range rows {
		out = append(out, toSaved(c))
	}
	return out, nil
}

func (s *Store) Create(ctx context.Context, in Input) (Saved, error) {
	p, err := s.prepare(ctx, in, 0)
	if err != nil {
		return Saved{}, err
	}
	row, err := s.q.CreateDownloadClient(ctx, p)
	if err != nil {
		return Saved{}, err
	}
	return toSaved(row), nil
}

func (s *Store) Update(ctx context.Context, id int64, in Input) (Saved, error) {
	if _, err := s.q.GetDownloadClient(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return Saved{}, ErrNotFound
	} else if err != nil {
		return Saved{}, err
	}
	p, err := s.prepare(ctx, in, id)
	if err != nil {
		return Saved{}, err
	}
	row, err := s.q.UpdateDownloadClient(ctx, dbq.UpdateDownloadClientParams{
		Name: p.Name, Type: p.Type, Protocol: p.Protocol, Enabled: p.Enabled, Priority: p.Priority, Settings: p.Settings, ID: id,
	})
	if err != nil {
		return Saved{}, err
	}
	return toSaved(row), nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	n, err := s.q.DeleteDownloadClient(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Resolve validates an unsaved client and returns its config. A masked
// password resolves against the saved client in.ID.
func (s *Store) Resolve(ctx context.Context, in Input) (Config, error) {
	p, err := s.prepare(ctx, in, in.ID)
	if err != nil {
		return Config{}, err
	}
	return Config{Name: p.Name, Type: p.Type, Settings: []byte(p.Settings)}, nil
}

// prepare validates the input and returns the row to store. When the
// settings carry the password mask, the stored password for storedID is kept.
func (s *Store) prepare(ctx context.Context, in Input, storedID int64) (dbq.CreateDownloadClientParams, error) {
	var p dbq.CreateDownloadClientParams
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return p, Invalid("Enter a name.")
	}
	info, ok := Lookup(in.Type)
	if !ok {
		return p, Invalid("Choose a download client type.")
	}
	if in.Priority < 0 {
		return p, Invalid("Priority can't be negative.")
	}
	settings := map[string]any{}
	if len(in.Settings) > 0 {
		if err := json.Unmarshal(in.Settings, &settings); err != nil || settings == nil {
			return p, Invalid("The settings aren't valid.")
		}
	}
	if pw, _ := settings["password"].(string); pw == PasswordMask && storedID != 0 {
		row, err := s.q.GetDownloadClient(ctx, storedID)
		if err == nil {
			var old map[string]any
			_ = json.Unmarshal([]byte(row.Settings), &old)
			settings["password"], _ = old["password"].(string)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return p, err
		}
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return p, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	cfg := Config{Name: in.Name, Type: in.Type, Enabled: enabled, Priority: in.Priority, Settings: raw}
	if _, err := New(cfg, nil); err != nil {
		return p, err
	}
	return dbq.CreateDownloadClientParams{
		Name: in.Name, Type: in.Type, Protocol: info.Protocol, Enabled: db.BoolInt(enabled), Priority: int64(in.Priority), Settings: string(raw),
	}, nil
}
