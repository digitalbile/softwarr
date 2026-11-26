package httpapi

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/digitalbile/softwarr/internal/auth"
)

// oidcStateCookie binds a sign-in's state to the browser that started it, so a
// callback link can't be completed in someone else's browser.
const oidcStateCookie = "softwarr_oidc_state"

func (s *server) registerOIDCRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/oidc/config", s.handleOIDCConfig)
	mux.HandleFunc("GET /api/v1/auth/oidc/login", s.handleOIDCLogin)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", s.handleOIDCCallback)
	mux.HandleFunc("POST /api/v1/auth/oidc/link", requireUser(s.handleOIDCLink))
}

func (s *server) setOIDCStateCookie(w http.ResponseWriter, r *http.Request, state string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     s.URLBase + "/api/v1/auth/oidc/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

// startOIDC sets the browser-binding cookie for the state in authURL.
func (s *server) startOIDC(w http.ResponseWriter, r *http.Request, authURL string) error {
	u, err := url.Parse(authURL)
	if err != nil {
		return err
	}
	state := u.Query().Get("state")
	if state == "" {
		return errors.New("oidc: authorization URL without state")
	}
	s.setOIDCStateCookie(w, r, state, 600)
	return nil
}

// oidcFail sends the browser to the login page with a message.
func (s *server) oidcFail(w http.ResponseWriter, r *http.Request, err error) {
	msg, expected := auth.OIDCErrorMessage(err)
	if expected {
		slog.Info("sso sign-in refused", "err", err)
	} else {
		slog.Error("sso sign-in failed", "err", err)
	}
	http.Redirect(w, r, s.URLBase+"/login?error="+url.QueryEscape(msg), http.StatusFound)
}

func (s *server) handleOIDCConfig(w http.ResponseWriter, r *http.Request) {
	st, err := s.Settings.Get(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	needsSetup, err := s.Users.NeedsSetup(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	localOff, err := s.OIDC.LocalAuthDisabled(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	enabled := st.OIDC.Enabled && st.OIDC.Issuer != "" && st.ExternalURL != "" && !needsSetup
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled, "buttonLabel": st.OIDC.ButtonLabel, "localAuthDisabled": localOff,
	})
}

func (s *server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	authURL, err := s.OIDC.Begin(r.Context(), nil)
	if err == nil {
		err = s.startOIDC(w, r, authURL)
	}
	if err != nil {
		s.oidcFail(w, r, err)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	state := q.Get("state")
	cookie, cookieErr := r.Cookie(oidcStateCookie)
	s.setOIDCStateCookie(w, r, "", -1)

	// Every callback uses up its state, whatever happens next.
	if q.Get("error") != "" || cookieErr != nil || state == "" ||
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		if err := s.OIDC.Discard(ctx, state); err != nil {
			slog.Error("discard sso state", "err", err)
		}
		if e := q.Get("error"); e != "" {
			s.oidcFail(w, r, fmt.Errorf("provider returned %q (%q): %w", e, q.Get("error_description"), auth.ErrOIDCFailed))
			return
		}
		s.oidcFail(w, r, auth.ErrOIDCStateInvalid)
		return
	}

	userID, out, err := s.OIDC.Callback(ctx, state, q.Get("code"))
	if err != nil {
		s.oidcFail(w, r, err)
		return
	}
	if out == auth.OutcomeLinked {
		// Linking never signs anyone in; the person keeps whatever session they had.
		http.Redirect(w, r, s.URLBase+"/profile?linked=1", http.StatusFound)
		return
	}
	if err := s.startSession(w, r, userID); err != nil {
		s.oidcFail(w, r, err)
		return
	}
	http.Redirect(w, r, s.URLBase+"/", http.StatusFound)
}

func (s *server) handleOIDCLink(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	if p.ViaAPIKey {
		// The API key acts as the owner, who can't link.
		writeError(w, http.StatusForbidden, auth.ErrOwnerNoOIDC.Error())
		return
	}
	authURL, err := s.OIDC.Begin(r.Context(), &p.UserID)
	if err == nil {
		err = s.startOIDC(w, r, authURL)
	}
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"redirect": authURL})
	case errors.Is(err, auth.ErrOIDCDisabled):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, auth.ErrOwnerNoOIDC), errors.Is(err, auth.ErrAccountInactive):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, auth.ErrAlreadyLinked), errors.Is(err, auth.ErrNotLocal):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, auth.ErrOIDCUnavailable):
		slog.Error("sso provider unavailable", "err", err)
		writeError(w, http.StatusBadGateway, auth.ErrOIDCUnavailable.Error())
	default:
		serverError(w, err)
	}
}
