package httpapi

import (
	"errors"
	"net/http"

	"github.com/digitalbile/softwarr/internal/pathmap"
)

func (s *server) registerPathMappingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/pathmappings", requireAdmin(s.handleListPathMappings))
	mux.HandleFunc("POST /api/v1/pathmappings", requireAdmin(s.handleCreatePathMapping))
	mux.HandleFunc("PUT /api/v1/pathmappings/{id}", requireAdmin(s.handleUpdatePathMapping))
	mux.HandleFunc("DELETE /api/v1/pathmappings/{id}", requireAdmin(s.handleDeletePathMapping))
}

func writePathMappingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pathmap.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case pathmap.IsInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, err)
	}
}

func (s *server) handleListPathMappings(w http.ResponseWriter, r *http.Request) {
	list, err := s.PathMappings.List(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) handleCreatePathMapping(w http.ResponseWriter, r *http.Request) {
	var in pathmap.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	m, err := s.PathMappings.Create(r.Context(), in)
	if err != nil {
		writePathMappingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *server) handleUpdatePathMapping(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, pathmap.ErrNotFound.Error())
	if !ok {
		return
	}
	var in pathmap.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	m, err := s.PathMappings.Update(r.Context(), id, in)
	if err != nil {
		writePathMappingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) handleDeletePathMapping(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, pathmap.ErrNotFound.Error())
	if !ok {
		return
	}
	if err := s.PathMappings.Delete(r.Context(), id); err != nil {
		writePathMappingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
