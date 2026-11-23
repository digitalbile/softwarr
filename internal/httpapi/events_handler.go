package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
)

const defaultPingInterval = 25 * time.Second

func (s *server) registerEventRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/events", requireUser(s.handleEvents))
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	rc := http.NewResponseController(w)

	ch, cancel := s.Broker.Subscribe(p.UserID, p.IsAdmin)
	defer cancel()

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

	ping := time.NewTicker(s.PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.Done:
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case m := <-ch:
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
