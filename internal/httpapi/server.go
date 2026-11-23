// Package httpapi wires softwarr's HTTP routes and middleware.
package httpapi

import (
	"database/sql"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/health"
	"github.com/digitalbile/softwarr/internal/library"
	"github.com/digitalbile/softwarr/internal/logbuf"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/requests"
	"github.com/digitalbile/softwarr/internal/settings"
	"github.com/digitalbile/softwarr/web"
)

type Deps struct {
	DB       *sql.DB
	Q        *dbq.Queries
	Settings *settings.Store
	Users    *auth.Users
	Sessions *auth.Sessions
	OIDC     *auth.OIDC
	Broker   *events.Broker
	Notify   *notify.Service
	Requests *requests.Service
	Library  *library.Service
	Health   *health.Checker
	Logs     *logbuf.Handler
	// NewProwlarr builds a Prowlarr client; tests inject a short timeout.
	NewProwlarr func(url, key string) *prowlarr.Client
	// Done, when closed, ends open SSE streams so server shutdown can drain.
	Done <-chan struct{}
	// PingInterval is the SSE keepalive period; zero means 25s.
	PingInterval time.Duration
	// UI is the built frontend; nil means the embedded web/dist.
	UI        fs.FS
	URLBase   string
	DataDir   string
	StartedAt time.Time
	Version   string
	Now       func() time.Time
}

type server struct {
	Deps
}

// New builds the full handler, mounted under d.URLBase.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.PingInterval == 0 {
		d.PingInterval = defaultPingInterval
	}
	if d.Broker == nil {
		d.Broker = events.NewBroker()
	}
	if d.UI == nil {
		ui, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			panic(err) // the embedded path is a constant
		}
		d.UI = ui
	}
	s := &server{Deps: d}
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
	})
	s.registerAuthRoutes(api)
	s.registerOIDCRoutes(api)
	s.registerUserRoutes(api)
	s.registerSettingsRoutes(api)
	s.registerEventRoutes(api)
	s.registerNotificationRoutes(api)
	s.registerWebhookRoutes(api)
	s.registerSearchRoutes(api)
	s.registerDownloadClientRoutes(api)
	s.registerPathMappingRoutes(api)
	s.registerRequestRoutes(api)
	s.registerActivityRoutes(api)
	s.registerLibraryRoutes(api)
	s.registerSystemRoutes(api)

	spa := spaHandler(d.UI, d.URLBase)
	inner := recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.authenticate(apiHandler(api)).ServeHTTP(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	}))
	if d.URLBase == "" {
		return securityHeaders(inner)
	}
	root := http.NewServeMux()
	root.Handle(d.URLBase+"/", http.StripPrefix(d.URLBase, inner))
	return securityHeaders(root)
}

// securityHeaders stops other sites framing softwarr and browsers sniffing
// content types, for the UI and the API alike.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// apiHandler serves mux, but turns the mux's plain-text 404 for an unknown
// path into the JSON error shape. Wrong-method 405 responses (with Allow)
// pass through untouched.
func apiHandler(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve through the mux itself so path wildcards get populated.
		if _, pattern := mux.Handler(r); pattern == "" {
			w = &jsonNotFound{ResponseWriter: w}
		}
		mux.ServeHTTP(w, r)
	})
}

// jsonNotFound rewrites a 404 written by the mux's default handler.
type jsonNotFound struct {
	http.ResponseWriter
	rewritten bool
}

func (j *jsonNotFound) WriteHeader(code int) {
	if code == http.StatusNotFound {
		j.rewritten = true
		writeError(j.ResponseWriter, code, "Not found.")
		return
	}
	j.ResponseWriter.WriteHeader(code)
}

func (j *jsonNotFound) Write(b []byte) (int, error) {
	if j.rewritten {
		return len(b), nil
	}
	return j.ResponseWriter.Write(b)
}
