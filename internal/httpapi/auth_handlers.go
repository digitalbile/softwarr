package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/digitalbile/softwarr/internal/auth"
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *server) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/setup", s.handleSetupNeeded)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", requireUser(s.handleLogout))
	mux.HandleFunc("GET /api/v1/auth/me", requireUser(s.handleMe))
	mux.HandleFunc("POST /api/v1/auth/password", requireUser(s.handleChangePassword))
}

func serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong.")
}

func (s *server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, expires, err := s.Sessions.Create(r.Context(), userID)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, r, token, expires)
	return nil
}

func (s *server) handleSetupNeeded(w http.ResponseWriter, r *http.Request) {
	needed, err := s.Users.NeedsSetup(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needed": needed})
}

func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	user, err := s.Users.Setup(r.Context(), in.Username, in.Password)
	switch {
	case errors.Is(err, auth.ErrSetupDone):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil && auth.IsInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		serverError(w, err)
		return
	}
	if err := s.startSession(w, r, user.ID); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	user, err := s.Users.Authenticate(r.Context(), in.Username, in.Password)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	case errors.Is(err, auth.ErrAccountInactive):
		writeError(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		serverError(w, err)
		return
	}
	if err := s.startSession(w, r, user.ID); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Sessions.Delete(r.Context(), c.Value); err != nil {
			serverError(w, err)
			return
		}
		s.clearSessionCookie(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	user, err := s.Users.Get(r.Context(), p.UserID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	p, _ := auth.FromContext(r.Context())
	err := s.Users.ChangePassword(r.Context(), p.UserID, in.Current, in.New)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusBadRequest, "Your current password is wrong.")
		return
	case err != nil && auth.IsInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		serverError(w, err)
		return
	}
	// A password change signs out every other device and keeps this one.
	if err := s.Sessions.DeleteForUser(r.Context(), p.UserID); err != nil {
		serverError(w, err)
		return
	}
	if !p.ViaAPIKey {
		if err := s.startSession(w, r, p.UserID); err != nil {
			serverError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
