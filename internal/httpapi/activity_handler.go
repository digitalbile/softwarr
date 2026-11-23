package httpapi

import "net/http"

func (s *server) registerActivityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/activity", requireAdmin(s.handleActivity))
}

func (s *server) handleActivity(w http.ResponseWriter, r *http.Request) {
	list, err := s.Requests.Activity(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
