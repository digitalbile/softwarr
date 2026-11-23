package httpapi

import (
	"net/http"
	"runtime"
	"slices"
	"time"

	"github.com/digitalbile/softwarr/internal/health"
	"github.com/digitalbile/softwarr/internal/logbuf"
)

const maxLogEntries = 500

func (s *server) registerSystemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/system/health", requireAdmin(s.handleSystemHealth))
	mux.HandleFunc("GET /api/v1/system/logs", requireAdmin(s.handleSystemLogs))
	mux.HandleFunc("GET /api/v1/system/status", requireAdmin(s.handleSystemStatus))
}

func (s *server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	checks := []health.Check{}
	if s.Health != nil {
		checks = s.Health.Run(r.Context())
	}
	writeJSON(w, http.StatusOK, checks)
}

// handleSystemLogs returns the last 500 log entries, newest first.
func (s *server) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	entries := []logbuf.Entry{}
	if s.Logs != nil {
		entries = s.Logs.Entries()
		slices.Reverse(entries)
		if len(entries) > maxLogEntries {
			entries = entries[:maxLogEntries]
		}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":   s.Version,
		"goVersion": runtime.Version(),
		"dataDir":   s.DataDir,
		"urlBase":   s.URLBase,
		"startedAt": s.StartedAt.UTC().Format(time.RFC3339),
	})
}
