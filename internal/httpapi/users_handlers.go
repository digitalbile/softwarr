package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/digitalbile/softwarr/internal/auth"
)

var errSelfLockout = errors.New("You can't disable or delete your own account.")

var errNotPending = errors.New("This account isn't waiting for approval.")

func (s *server) registerUserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users", requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", requireAdmin(s.handleCreateUser))
	mux.HandleFunc("PATCH /api/v1/users/{id}", requireAdmin(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", requireAdmin(s.handleDeleteUser))
	mux.HandleFunc("POST /api/v1/users/{id}/approve", requireAdmin(s.handleApproveUser))
}

// writeUserError maps user service errors to responses.
func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, auth.ErrOwnerProtected), errors.Is(err, auth.ErrHasRequests),
		errors.Is(err, errSelfLockout), errors.Is(err, errNotPending):
		writeError(w, http.StatusConflict, err.Error())
	case auth.IsInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, err)
	}
}

func userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, auth.ErrNotFound.Error())
		return 0, false
	}
	return id, true
}

func (s *server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Users.List(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"isAdmin"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	user, err := s.Users.CreateLocal(r.Context(), in.Username, in.Email, in.Password, in.IsAdmin)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	var patch auth.UserPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	p, _ := auth.FromContext(r.Context())
	if id == p.UserID && patch.Status != nil && *patch.Status != "active" {
		writeUserError(w, errSelfLockout)
		return
	}
	user, err := s.Users.Update(r.Context(), id, patch)
	if err != nil {
		writeUserError(w, err)
		return
	}
	if user.Status == "disabled" {
		if err := s.Sessions.DeleteForUser(r.Context(), id); err != nil {
			serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if p, _ := auth.FromContext(r.Context()); id == p.UserID {
		writeUserError(w, errSelfLockout)
		return
	}
	if err := s.Users.Delete(r.Context(), id); err != nil {
		writeUserError(w, err)
		return
	}
	if err := s.Sessions.DeleteForUser(r.Context(), id); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleApproveUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	user, err := s.Users.Get(r.Context(), id)
	if err != nil {
		writeUserError(w, err)
		return
	}
	if user.Status != "pending" {
		writeUserError(w, errNotPending)
		return
	}
	active := "active"
	user, err = s.Users.Update(r.Context(), id, auth.UserPatch{Status: &active})
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}
