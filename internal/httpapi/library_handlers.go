package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/library"
)

func (s *server) registerLibraryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/library", requireUser(s.handleListLibrary))
	mux.HandleFunc("GET /api/v1/library/{id}", requireUser(s.handleGetLibraryItem))
	mux.HandleFunc("GET /api/v1/library/{id}/download", requireUser(s.handleDownloadLibraryItem))
	mux.HandleFunc("PATCH /api/v1/library/{id}", requireAdmin(s.handleUpdateLibraryItem))
	mux.HandleFunc("DELETE /api/v1/library/{id}", requireAdmin(s.handleDeleteLibraryItem))
}

func writeLibraryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, library.ErrInvalidRetention):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, library.ErrFilesMissing):
		writeError(w, http.StatusGone, err.Error())
	case errors.Is(err, library.ErrFolderMissing):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, library.ErrUnsafePath):
		slog.Warn("library item folder refused", "err", err)
		writeError(w, http.StatusConflict, "The folder for this item isn't a plain folder inside the library folder, so softwarr left it alone. Check the library folder in Settings.")
	default:
		serverError(w, err)
	}
}

// hideComment clears the requester's comment unless the viewer is an admin or
// the requester.
func hideComment(p auth.Principal, it *library.Item) {
	if !p.IsAdmin && p.UserID != it.RequesterID {
		it.Comment = ""
	}
}

func (s *server) handleListLibrary(w http.ResponseWriter, r *http.Request) {
	items, err := s.Library.List(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		serverError(w, err)
		return
	}
	p, _ := auth.FromContext(r.Context())
	for i := range items {
		hideComment(p, &items[i])
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *server) handleGetLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, library.ErrNotFound.Error())
	if !ok {
		return
	}
	it, err := s.Library.Get(r.Context(), id)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	p, _ := auth.FromContext(r.Context())
	hideComment(p, &it)
	writeJSON(w, http.StatusOK, it)
}

func (s *server) handleDownloadLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, library.ErrNotFound.Error())
	if !ok {
		return
	}
	it, err := s.Library.Get(r.Context(), id)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	if err := s.Library.Download(w, r, it); err != nil {
		if errors.Is(err, library.ErrAborted) {
			slog.Warn("library download aborted", "id", id, "err", err)
			// Reset the connection so the client sees a failed download, not
			// a complete-looking response with a broken zip.
			panic(http.ErrAbortHandler)
		}
		writeLibraryError(w, err)
	}
}

func (s *server) handleUpdateLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, library.ErrNotFound.Error())
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	var title *string
	var days **int
	for k, raw := range body {
		switch k {
		case "title":
			var t *string
			if err := json.Unmarshal(raw, &t); err != nil {
				writeError(w, http.StatusBadRequest, "That request wasn't valid.")
				return
			}
			if t == nil {
				empty := ""
				t = &empty
			}
			title = t
		case "retentionDays":
			var d *int
			if err := json.Unmarshal(raw, &d); err != nil {
				writeError(w, http.StatusBadRequest, "That request wasn't valid.")
				return
			}
			days = &d
		default:
			writeError(w, http.StatusBadRequest, "That request wasn't valid.")
			return
		}
	}
	it, err := s.Library.Update(r.Context(), id, title, days)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *server) handleDeleteLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, library.ErrNotFound.Error())
	if !ok {
		return
	}
	if err := s.Library.Delete(r.Context(), id); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
