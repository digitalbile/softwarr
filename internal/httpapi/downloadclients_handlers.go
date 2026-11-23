package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
)

const (
	passwordMask      = "********"
	clientTestTimeout = 20 * time.Second
)

func (s *server) registerDownloadClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/downloadclients", requireAdmin(s.handleListDownloadClients))
	mux.HandleFunc("GET /api/v1/downloadclients/types", requireAdmin(s.handleDownloadClientTypes))
	mux.HandleFunc("POST /api/v1/downloadclients", requireAdmin(s.handleCreateDownloadClient))
	mux.HandleFunc("POST /api/v1/downloadclients/test", requireAdmin(s.handleTestDownloadClient))
	mux.HandleFunc("PUT /api/v1/downloadclients/{id}", requireAdmin(s.handleUpdateDownloadClient))
	mux.HandleFunc("DELETE /api/v1/downloadclients/{id}", requireAdmin(s.handleDeleteDownloadClient))
}

type downloadClientInput struct {
	ID       int64           `json:"id"` // only used by the test endpoint
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Enabled  *bool           `json:"enabled"`
	Priority int             `json:"priority"`
	Settings json.RawMessage `json:"settings"`
}

type downloadClientView struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Protocol string         `json:"protocol"`
	Enabled  bool           `json:"enabled"`
	Priority int64          `json:"priority"`
	Settings map[string]any `json:"settings"`
}

func toDownloadClientView(c dbq.DownloadClient) downloadClientView {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(c.Settings), &m)
	if pw, _ := m["password"].(string); pw != "" {
		m["password"] = passwordMask
	}
	return downloadClientView{ID: c.ID, Name: c.Name, Type: c.Type, Protocol: c.Protocol, Enabled: c.Enabled != 0, Priority: c.Priority, Settings: m}
}

func (s *server) handleDownloadClientTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, downloadclient.Types())
}

func (s *server) handleListDownloadClients(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Q.ListDownloadClients(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]downloadClientView, 0, len(rows))
	for _, c := range rows {
		out = append(out, toDownloadClientView(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// prepare validates the input and returns the settings to store. When the
// settings carry the password mask, the stored password for storedID is kept.
func (s *server) prepareDownloadClient(r *http.Request, in downloadClientInput, storedID int64) (dbq.CreateDownloadClientParams, error) {
	var p dbq.CreateDownloadClientParams
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return p, downloadclient.Invalid("Enter a name.")
	}
	info, ok := downloadclient.Lookup(in.Type)
	if !ok {
		return p, downloadclient.Invalid("Choose a download client type.")
	}
	if in.Priority < 0 {
		return p, downloadclient.Invalid("Priority can't be negative.")
	}
	settings := map[string]any{}
	if len(in.Settings) > 0 {
		if err := json.Unmarshal(in.Settings, &settings); err != nil || settings == nil {
			return p, downloadclient.Invalid("The settings aren't valid.")
		}
	}
	if pw, _ := settings["password"].(string); pw == passwordMask && storedID != 0 {
		row, err := s.Q.GetDownloadClient(r.Context(), storedID)
		if err == nil {
			var old map[string]any
			_ = json.Unmarshal([]byte(row.Settings), &old)
			settings["password"], _ = old["password"].(string)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return p, err
		}
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return p, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	cfg := downloadclient.Config{Name: in.Name, Type: in.Type, Enabled: enabled, Priority: in.Priority, Settings: raw}
	if _, err := downloadclient.New(cfg, nil); err != nil {
		return p, err
	}
	p = dbq.CreateDownloadClientParams{Name: in.Name, Type: in.Type, Protocol: info.Protocol, Priority: int64(in.Priority), Settings: string(raw)}
	if enabled {
		p.Enabled = 1
	}
	return p, nil
}

func writeDownloadClientError(w http.ResponseWriter, err error) {
	if downloadclient.IsInputError(err) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	serverError(w, err)
}

func (s *server) handleCreateDownloadClient(w http.ResponseWriter, r *http.Request) {
	var in downloadClientInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	p, err := s.prepareDownloadClient(r, in, 0)
	if err != nil {
		writeDownloadClientError(w, err)
		return
	}
	row, err := s.Q.CreateDownloadClient(r.Context(), p)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDownloadClientView(row))
}

func (s *server) handleUpdateDownloadClient(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	var in downloadClientInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if _, err := s.Q.GetDownloadClient(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, auth.ErrNotFound.Error())
		return
	} else if err != nil {
		serverError(w, err)
		return
	}
	p, err := s.prepareDownloadClient(r, in, id)
	if err != nil {
		writeDownloadClientError(w, err)
		return
	}
	row, err := s.Q.UpdateDownloadClient(r.Context(), dbq.UpdateDownloadClientParams{
		Name: p.Name, Type: p.Type, Protocol: p.Protocol, Enabled: p.Enabled, Priority: p.Priority, Settings: p.Settings, ID: id,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDownloadClientView(row))
}

func (s *server) handleDeleteDownloadClient(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	n, err := s.Q.DeleteDownloadClient(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, auth.ErrNotFound.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestDownloadClient checks an unsaved config. A body id lets a masked
// password resolve against that saved client.
func (s *server) handleTestDownloadClient(w http.ResponseWriter, r *http.Request) {
	var in downloadClientInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	type result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if in.Name == "" {
		in.Name = "test"
	}
	p, err := s.prepareDownloadClient(r, in, in.ID)
	if err != nil {
		if downloadclient.IsInputError(err) {
			writeJSON(w, http.StatusOK, result{Error: err.Error()})
			return
		}
		serverError(w, err)
		return
	}
	c, err := downloadclient.New(downloadclient.Config{Name: p.Name, Type: p.Type, Settings: []byte(p.Settings)}, &http.Client{Timeout: clientTestTimeout})
	if err != nil {
		writeJSON(w, http.StatusOK, result{Error: err.Error()})
		return
	}
	if err := c.Test(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, result{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result{OK: true})
}
