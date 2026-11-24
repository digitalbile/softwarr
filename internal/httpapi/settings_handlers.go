package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/digitalbile/softwarr/internal/importer"
	"github.com/digitalbile/softwarr/internal/settings"
)

func (s *server) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings", requireAdmin(s.handleGetSettings))
	mux.HandleFunc("PUT /api/v1/settings", requireAdmin(s.handlePutSettings))
	mux.HandleFunc("POST /api/v1/settings/apikey", requireAdmin(s.handleRegenerateAPIKey))
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.Settings.Get(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in settings.Settings
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	// The API key only changes through the regenerate endpoint, so a stale
	// copy in the form can't overwrite it.
	in.APIKey = ""
	if !s.prepareLibraryRoot(w, r, in.LibraryRoot) {
		return
	}
	if err := s.Settings.Save(r.Context(), in); err != nil {
		if settings.IsInputError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		serverError(w, err)
		return
	}
	s.handleGetSettings(w, r)
}

// prepareLibraryRoot creates a newly set library root and writes its marker,
// so softwarr can later tell the mounted folder from an empty mount point. An
// unchanged root is left alone: writing the marker then could mark a bare
// mount point while the volume is away. ok is false when it wrote a response.
func (s *server) prepareLibraryRoot(w http.ResponseWriter, r *http.Request, raw string) bool {
	root := settings.CleanLibraryRoot(raw)
	if root == "" || !filepath.IsAbs(root) {
		return true // Save reports a relative path
	}
	cur, err := s.Settings.Get(r.Context())
	if err != nil {
		serverError(w, err)
		return false
	}
	if root == cur.LibraryRoot {
		return true
	}
	if err := importer.PrepareRoot(root); err != nil {
		slog.Warn("prepare library folder", "root", root, "err", err)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("softwarr couldn't create the library folder %s or write to it. Check the path and that softwarr has permission to write there.", root))
		return false
	}
	return true
}

func (s *server) handleRegenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.Settings.RegenerateAPIKey(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"apiKey": key})
}
