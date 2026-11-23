// Package httpapi wires softwarr's HTTP routes and middleware.
package httpapi

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/settings"
)

type Deps struct {
	DB       *sql.DB
	Q        *dbq.Queries
	Settings *settings.Store
	Users    *auth.Users
	Sessions *auth.Sessions
	URLBase  string
	Version  string
	Now      func() time.Time
}

type server struct {
	Deps
}

// New builds the full handler, mounted under d.URLBase.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{Deps: d}
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
	})
	s.registerAuthRoutes(api)

	inner := recoverer(s.authenticate(api))
	if d.URLBase == "" {
		return inner
	}
	root := http.NewServeMux()
	root.Handle(d.URLBase+"/", http.StripPrefix(d.URLBase, inner))
	return root
}
