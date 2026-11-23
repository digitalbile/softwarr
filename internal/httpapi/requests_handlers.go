package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/releasetoken"
	"github.com/digitalbile/softwarr/internal/requests"
)

func (s *server) registerRequestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/requests", requireUser(s.handleCreateRequest))
	mux.HandleFunc("GET /api/v1/requests", requireUser(s.handleListRequests))
	mux.HandleFunc("GET /api/v1/requests/{id}", requireUser(s.handleGetRequest))
	mux.HandleFunc("POST /api/v1/requests/{id}/approve", requireAdmin(s.handleApproveRequest))
	mux.HandleFunc("POST /api/v1/requests/{id}/deny", requireAdmin(s.handleDenyRequest))
	mux.HandleFunc("POST /api/v1/requests/{id}/cancel", requireUser(s.requestAction((*requests.Service).Cancel)))
	mux.HandleFunc("POST /api/v1/requests/{id}/abandon", requireAdmin(s.requestAction((*requests.Service).Abandon)))
	mux.HandleFunc("POST /api/v1/requests/{id}/retry", requireAdmin(s.requestAction((*requests.Service).Retry)))
}

func writeRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, requests.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, requests.ErrConflict), errors.Is(err, requests.ErrDuplicate),
		errors.Is(err, requests.ErrInLibrary), errors.Is(err, requests.ErrInvalidTransition),
		errors.Is(err, requests.ErrNoLibraryRoot):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, requests.ErrCommentRequired), errors.Is(err, requests.ErrNoClient),
		requests.IsInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, err)
	}
}

// openRelease returns the release sealed in a search result token issued to
// the caller. ok is false when it wrote an error response.
func (s *server) openRelease(w http.ResponseWriter, r *http.Request, token string) (rel prowlarr.Release, ok bool) {
	p, _ := auth.FromContext(r.Context())
	secret, err := s.Settings.ReleaseSecret(r.Context())
	if err != nil {
		serverError(w, err)
		return rel, false
	}
	rel, err = releasetoken.Open(secret, token, p.UserID, s.Now())
	if err != nil {
		if errors.Is(err, releasetoken.ErrInvalid) || errors.Is(err, releasetoken.ErrExpired) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			serverError(w, err)
		}
		return rel, false
	}
	return rel, true
}

func (s *server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token   string `json:"token"`
		Comment string `json:"comment"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	rel, ok := s.openRelease(w, r, in.Token)
	if !ok {
		return
	}
	p, _ := auth.FromContext(r.Context())
	req, err := s.Requests.Create(r.Context(), p, rel, in.Comment)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (s *server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	all := r.URL.Query().Get("all") == "1"
	list, err := s.Requests.List(r.Context(), p, all)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, requests.ErrNotFound.Error())
	if !ok {
		return
	}
	p, _ := auth.FromContext(r.Context())
	req, err := s.Requests.Get(r.Context(), p, id)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *server) handleApproveRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, requests.ErrNotFound.Error())
	if !ok {
		return
	}
	var in struct {
		DisplayTitle     string `json:"displayTitle"`
		ReplacementToken string `json:"replacementToken"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "That request wasn't valid.")
			return
		}
	}
	opts := requests.ApproveOpts{DisplayTitle: in.DisplayTitle}
	if in.ReplacementToken != "" {
		rel, ok := s.openRelease(w, r, in.ReplacementToken)
		if !ok {
			return
		}
		opts.Replacement = &rel
	}
	p, _ := auth.FromContext(r.Context())
	req, err := s.Requests.Approve(r.Context(), p, id, opts)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *server) handleDenyRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, requests.ErrNotFound.Error())
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "That request wasn't valid.")
			return
		}
	}
	p, _ := auth.FromContext(r.Context())
	req, err := s.Requests.Deny(r.Context(), p, id, in.Reason)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// requestAction builds a handler for a body-less action on one request.
func (s *server) requestAction(act func(*requests.Service, context.Context, auth.Principal, int64) (requests.Request, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, requests.ErrNotFound.Error())
		if !ok {
			return
		}
		p, _ := auth.FromContext(r.Context())
		req, err := act(s.Requests, r.Context(), p, id)
		if err != nil {
			writeRequestError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, req)
	}
}
