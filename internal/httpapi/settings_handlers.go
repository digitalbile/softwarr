package httpapi

import (
	"net/http"

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

func (s *server) handleRegenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.Settings.RegenerateAPIKey(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"apiKey": key})
}
