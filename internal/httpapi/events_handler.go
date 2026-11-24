package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
)

const defaultPingInterval = 25 * time.Second

// testHookAfterSubscribe runs right after a stream subscribes; tests use it
// to change the user at that moment.
var testHookAfterSubscribe func()

func (s *server) registerEventRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/events", requireUser(s.handleEvents))
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	rc := http.NewResponseController(w)

	ch, cancel := s.Broker.Subscribe(p.UserID, p.IsAdmin)
	defer cancel()
	if testHookAfterSubscribe != nil {
		testHookAfterSubscribe()
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	// Flush sends the headers right away so clients see the stream open.
	if err := rc.Flush(); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			h.Del("Content-Type")
			h.Del("Cache-Control")
			h.Del("X-Accel-Buffering")
			writeError(w, http.StatusInternalServerError, "streaming is not supported")
		}
		return
	}
	// A demotion between authenticating this request and subscribing would
	// have missed the disconnect, so check once now.
	if !s.streamStillValid(r, p) {
		return
	}

	ping := time.NewTicker(s.PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.Done:
			return
		case <-ping.C:
			// The stream was opened with the caller's role at the time. End
			// it if that no longer holds; the client reconnects with the
			// right scope, or gets signed out.
			if !s.streamStillValid(r, p) {
				return
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case m, open := <-ch:
			if !open {
				return // the broker disconnected this user
			}
			data, err := json.Marshal(m.Data)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Type, data); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// streamStillValid reports whether the caller of an open stream would still
// get the same stream: the session or API key still works, and the user is
// active with the same admin flag and no pending password change.
func (s *server) streamStillValid(r *http.Request, p auth.Principal) bool {
	ctx := r.Context()
	if p.ViaAPIKey {
		st, err := s.Settings.Get(ctx)
		if err != nil {
			return true // can't tell; try again on the next ping
		}
		key := r.Header.Get(apiKeyHeader)
		return st.APIKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(st.APIKey)) == 1
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	userID, err := s.Sessions.Check(ctx, c.Value)
	if errors.Is(err, auth.ErrNoSession) || (err == nil && userID != p.UserID) {
		return false
	}
	if err != nil {
		return true
	}
	user, err := s.Users.Get(ctx, userID)
	if errors.Is(err, auth.ErrNotFound) {
		return false
	}
	if err != nil {
		return true
	}
	return user.Status == "active" && user.IsAdmin == p.IsAdmin && !user.MustChangePassword
}
