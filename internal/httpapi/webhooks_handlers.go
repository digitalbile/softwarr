package httpapi

import (
	"errors"
	"net/http"

	"github.com/digitalbile/softwarr/internal/notify"
)

func (s *server) registerWebhookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/webhooks", requireAdmin(s.handleListWebhooks))
	mux.HandleFunc("POST /api/v1/webhooks", requireAdmin(s.handleCreateWebhook))
	mux.HandleFunc("PUT /api/v1/webhooks/{id}", requireAdmin(s.handleUpdateWebhook))
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", requireAdmin(s.handleDeleteWebhook))
	mux.HandleFunc("POST /api/v1/webhooks/{id}/test", requireAdmin(s.handleTestWebhook))
}

func writeWebhookError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, notify.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case notify.IsInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, err)
	}
}

func (s *server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	list, err := s.Notify.ListWebhooks(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var in notify.WebhookInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	h, err := s.Notify.CreateWebhook(r.Context(), in)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, h)
}

func (s *server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	var in notify.WebhookInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	h, err := s.Notify.UpdateWebhook(r.Context(), id, in)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if err := s.Notify.DeleteWebhook(r.Context(), id); err != nil {
		writeWebhookError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	res, err := s.Notify.TestWebhook(r.Context(), id)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
