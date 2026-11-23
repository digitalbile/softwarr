package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/pathmap"
)

const errPathMappingNotFound = "That path mapping doesn't exist."

func (s *server) registerPathMappingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/pathmappings", requireAdmin(s.handleListPathMappings))
	mux.HandleFunc("POST /api/v1/pathmappings", requireAdmin(s.handleCreatePathMapping))
	mux.HandleFunc("PUT /api/v1/pathmappings/{id}", requireAdmin(s.handleUpdatePathMapping))
	mux.HandleFunc("DELETE /api/v1/pathmappings/{id}", requireAdmin(s.handleDeletePathMapping))
}

type pathMappingInput struct {
	Host       string `json:"host"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
}

type pathMappingView struct {
	ID         int64  `json:"id"`
	Host       string `json:"host"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
}

func toPathMappingView(m dbq.PathMapping) pathMappingView {
	return pathMappingView{ID: m.ID, Host: m.Host, RemotePath: m.RemotePath, LocalPath: m.LocalPath}
}

// validate trims and normalizes the input, returning a user-facing message
// when it is not acceptable.
func (in pathMappingInput) validate() (pathMappingInput, string) {
	in.Host = strings.TrimSpace(in.Host)
	in.RemotePath = strings.TrimSpace(in.RemotePath)
	in.LocalPath = strings.TrimSpace(in.LocalPath)
	switch {
	case in.Host == "":
		return in, "Enter the download client host."
	case in.RemotePath == "":
		return in, "Enter the remote path."
	case in.LocalPath == "":
		return in, "Enter the local path."
	case !strings.HasPrefix(in.LocalPath, "/"):
		return in, "The local path must start with /."
	}
	if pathmap.IsWindows(in.RemotePath) {
		if !strings.HasSuffix(in.RemotePath, `\`) && !strings.HasSuffix(in.RemotePath, "/") {
			in.RemotePath += `\`
		}
	} else if !strings.HasSuffix(in.RemotePath, "/") {
		in.RemotePath += "/"
	}
	if !strings.HasSuffix(in.LocalPath, "/") {
		in.LocalPath += "/"
	}
	return in, ""
}

func (s *server) handleListPathMappings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Q.ListPathMappings(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]pathMappingView, 0, len(rows))
	for _, m := range rows {
		out = append(out, toPathMappingView(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleCreatePathMapping(w http.ResponseWriter, r *http.Request) {
	var in pathMappingInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	in, msg := in.validate()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	row, err := s.Q.CreatePathMapping(r.Context(), dbq.CreatePathMappingParams{Host: in.Host, RemotePath: in.RemotePath, LocalPath: in.LocalPath})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPathMappingView(row))
}

func (s *server) handleUpdatePathMapping(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, errPathMappingNotFound)
	if !ok {
		return
	}
	var in pathMappingInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	in, msg := in.validate()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	row, err := s.Q.UpdatePathMapping(r.Context(), dbq.UpdatePathMappingParams{Host: in.Host, RemotePath: in.RemotePath, LocalPath: in.LocalPath, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, errPathMappingNotFound)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPathMappingView(row))
}

func (s *server) handleDeletePathMapping(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, errPathMappingNotFound)
	if !ok {
		return
	}
	n, err := s.Q.DeletePathMapping(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, errPathMappingNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
