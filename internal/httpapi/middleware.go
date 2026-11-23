package httpapi

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
)

const (
	sessionCookie = "softwarr_session"
	csrfHeader    = "X-Softwarr-CSRF"
	apiKeyHeader  = "X-Api-Key"
)

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic in handler", "panic", v, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "Something went wrong.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     s.URLBase + "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Path:     s.URLBase + "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

func isSafeMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead }

// mustChangeExempt lists the routes a user with a temporary password may use.
func mustChangeExempt(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/auth/me", "POST /api/v1/auth/password", "POST /api/v1/auth/logout":
		return true
	}
	return false
}

// authenticate resolves the caller from an API key or session cookie and
// stores the Principal in the request context. Unauthenticated requests pass
// through; requireUser decides whether that is allowed.
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if key := r.Header.Get(apiKeyHeader); key != "" {
			st, err := s.Settings.Get(ctx)
			if err != nil {
				slog.Error("load settings", "err", err)
				writeError(w, http.StatusInternalServerError, "Something went wrong.")
				return
			}
			if st.APIKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(st.APIKey)) == 1 {
				owner, err := s.Users.Owner(ctx)
				if err == nil {
					next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(ctx, auth.Principal{
						UserID: owner.ID, Username: owner.Username, IsOwner: true, IsAdmin: true, AutoApprove: true, ViaAPIKey: true,
					})))
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		}

		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		userID, err := s.Sessions.Lookup(ctx, c.Value)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.Users.Get(ctx, userID)
		if err != nil || user.Status != "active" {
			_ = s.Sessions.Delete(ctx, c.Value)
			next.ServeHTTP(w, r)
			return
		}
		if !isSafeMethod(r.Method) && r.Header.Get(csrfHeader) != "1" {
			writeError(w, http.StatusForbidden, "Missing CSRF header.")
			return
		}
		if user.MustChangePassword && !mustChangeExempt(r) {
			writeError(w, http.StatusForbidden, "password_change_required")
			return
		}
		// Keep the browser cookie in step with the sliding server expiry.
		s.setSessionCookie(w, r, c.Value, s.Now().Add(30*24*time.Hour))
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(ctx, auth.Principal{
			UserID: user.ID, Username: user.Username, IsOwner: user.IsOwner, IsAdmin: user.IsAdmin,
			AutoApprove: user.AutoApprove, MustChangePassword: user.MustChangePassword,
		})))
	})
}

func requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.FromContext(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "Sign in to continue.")
			return
		}
		h(w, r)
	}
}

func requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return requireUser(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		if !p.IsAdmin {
			writeError(w, http.StatusForbidden, "Admins only.")
			return
		}
		h(w, r)
	})
}
