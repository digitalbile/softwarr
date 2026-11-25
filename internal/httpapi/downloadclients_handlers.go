package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/digitalbile/softwarr/internal/downloadclient"
)

const clientTestTimeout = 20 * time.Second

func (s *server) registerDownloadClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/downloadclients", requireAdmin(s.handleListDownloadClients))
	mux.HandleFunc("GET /api/v1/downloadclients/types", requireAdmin(s.handleDownloadClientTypes))
	mux.HandleFunc("POST /api/v1/downloadclients", requireAdmin(s.handleCreateDownloadClient))
	mux.HandleFunc("POST /api/v1/downloadclients/test", requireAdmin(s.handleTestDownloadClient))
	mux.HandleFunc("PUT /api/v1/downloadclients/{id}", requireAdmin(s.handleUpdateDownloadClient))
	mux.HandleFunc("DELETE /api/v1/downloadclients/{id}", requireAdmin(s.handleDeleteDownloadClient))
}

func writeDownloadClientError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, downloadclient.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case downloadclient.IsInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, err)
	}
}

func (s *server) handleDownloadClientTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, downloadclient.Types())
}

func (s *server) handleListDownloadClients(w http.ResponseWriter, r *http.Request) {
	list, err := s.DownloadClients.List(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) handleCreateDownloadClient(w http.ResponseWriter, r *http.Request) {
	var in downloadclient.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	c, err := s.DownloadClients.Create(r.Context(), in)
	if err != nil {
		writeDownloadClientError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *server) handleUpdateDownloadClient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, downloadclient.ErrNotFound.Error())
	if !ok {
		return
	}
	var in downloadclient.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	c, err := s.DownloadClients.Update(r.Context(), id, in)
	if err != nil {
		writeDownloadClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) handleDeleteDownloadClient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, downloadclient.ErrNotFound.Error())
	if !ok {
		return
	}
	if err := s.DownloadClients.Delete(r.Context(), id); err != nil {
		writeDownloadClientError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestDownloadClient checks an unsaved config. A body id lets a masked
// password resolve against that saved client.
func (s *server) handleTestDownloadClient(w http.ResponseWriter, r *http.Request) {
	var in downloadclient.Input
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	type result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if in.Name == "" {
		in.Name = "test"
	}
	cfg, err := s.DownloadClients.Resolve(r.Context(), in)
	if err != nil {
		if downloadclient.IsInputError(err) {
			writeJSON(w, http.StatusOK, result{Error: err.Error()})
			return
		}
		serverError(w, err)
		return
	}
	c, err := downloadclient.New(cfg, &http.Client{Timeout: clientTestTimeout})
	if err != nil {
		writeJSON(w, http.StatusOK, result{Error: err.Error()})
		return
	}
	if err := c.Test(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, result{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result{OK: true})
}
