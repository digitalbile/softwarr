package httpapi

import (
	"net/http"

	"github.com/digitalbile/softwarr/internal/auth"
)

func (s *server) registerNotificationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/notifications", requireUser(s.handleListNotifications))
	mux.HandleFunc("POST /api/v1/notifications/read", requireUser(s.handleMarkNotificationsRead))
}

func (s *server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	list, err := s.Notify.ListFor(r.Context(), p.UserID, r.URL.Query().Get("unread") == "1")
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	var err error
	if in.All {
		err = s.Notify.MarkAllRead(r.Context(), p.UserID)
	} else {
		err = s.Notify.MarkRead(r.Context(), p.UserID, in.IDs)
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
