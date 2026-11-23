package downloadclient

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"sync"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

type TypeInfo struct {
	Type     string `json:"type"`
	Protocol string `json:"protocol"`
	Name     string `json:"name"`
}

type registration struct {
	info    TypeInfo
	factory Factory
}

var (
	regMu sync.RWMutex
	reg   = map[string]registration{}
)

// Register adds a client type, using the type as its display name.
func Register(typ, protocol string, f Factory) { RegisterNamed(typ, typ, protocol, f) }

// RegisterNamed adds a client type with a display name for the UI.
func RegisterNamed(typ, name, protocol string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	reg[typ] = registration{info: TypeInfo{Type: typ, Protocol: protocol, Name: name}, factory: f}
}

// Lookup returns the registered type, if any.
func Lookup(typ string) (TypeInfo, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	r, ok := reg[typ]
	return r.info, ok
}

// New builds a client from cfg. A nil httpc means a default client.
func New(cfg Config, httpc *http.Client) (Client, error) {
	regMu.RLock()
	r, ok := reg[cfg.Type]
	regMu.RUnlock()
	if !ok {
		return nil, Invalid("Choose a download client type.")
	}
	if httpc == nil {
		httpc = &http.Client{Timeout: 30e9}
	}
	return r.factory(cfg, httpc)
}

func Types() []TypeInfo {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]TypeInfo, 0, len(reg))
	for _, r := range reg {
		out = append(out, r.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// ConfigFromRow converts a stored client to a Config.
func ConfigFromRow(c dbq.DownloadClient) Config {
	return Config{ID: c.ID, Name: c.Name, Type: c.Type, Enabled: c.Enabled != 0, Priority: int(c.Priority), Settings: []byte(c.Settings)}
}

// PickClient returns the enabled client for protocol with the lowest priority
// value, ties broken by id.
func PickClient(ctx context.Context, q *dbq.Queries, protocol string) (Config, bool, error) {
	row, err := q.PickDownloadClient(ctx, protocol)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	return ConfigFromRow(row), true, nil
}
