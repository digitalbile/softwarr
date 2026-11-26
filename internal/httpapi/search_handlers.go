package httpapi

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/releasetoken"
)

const prowlarrTimeout = 60 * time.Second

// SearchResult is one release as the browser sees it. The embedded Release
// never serializes its download links; Token carries them, sealed, so a
// request names a release the server found rather than one the client wrote.
type SearchResult struct {
	prowlarr.Release
	Key           string `json:"key"`
	Status        string `json:"status"` // one of the requests.Release* values
	LibraryItemID *int64 `json:"libraryItemId"`
	Token         string `json:"token"`
}

const prowlarrUnreachableForUsers = "Couldn't reach Prowlarr. Ask an admin to check System > Health."

func (s *server) registerSearchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/search", requireUser(s.handleSearch))
	mux.HandleFunc("GET /api/v1/indexers", requireAdmin(s.handleIndexers))
	mux.HandleFunc("POST /api/v1/settings/prowlarr/test", requireAdmin(s.handleProwlarrTest))
}

func (s *server) prowlarrClient(url, key string) *prowlarr.Client {
	if s.NewProwlarr != nil {
		return s.NewProwlarr(url, key)
	}
	return prowlarr.New(url, key, &http.Client{Timeout: prowlarrTimeout})
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "Enter something to search for.")
		return
	}
	st, err := s.Settings.Get(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	if st.Prowlarr.URL == "" {
		writeError(w, http.StatusConflict, "Prowlarr isn't set up yet. An admin can add it in Settings > Indexers.")
		return
	}
	p, _ := auth.FromContext(ctx)
	var indexerIDs []int
	if p.IsAdmin {
		for _, f := range strings.Split(r.URL.Query().Get("indexerIds"), ",") {
			if id, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
				indexerIDs = append(indexerIDs, id)
			}
		}
	}
	releases, err := s.prowlarrClient(st.Prowlarr.URL, st.Prowlarr.APIKey).Search(ctx, q, st.Prowlarr.Categories, indexerIDs)
	if err != nil {
		slog.Warn("prowlarr search failed", "err", err)
		if p.IsAdmin {
			writeError(w, http.StatusBadGateway, "Couldn't reach Prowlarr: "+err.Error())
		} else {
			writeError(w, http.StatusBadGateway, prowlarrUnreachableForUsers)
		}
		return
	}
	secret, err := s.Settings.ReleaseSecret(ctx)
	if err != nil {
		serverError(w, err)
		return
	}

	states, err := s.Requests.ReleaseStates(ctx, releases)
	if err != nil {
		serverError(w, err)
		return
	}
	now := s.Now()
	results := make([]SearchResult, len(releases))
	for i, rel := range releases {
		tok, err := releasetoken.Seal(secret, rel, p.UserID, now)
		if err != nil {
			serverError(w, err)
			return
		}
		results[i] = SearchResult{Release: rel, Key: rel.Key(), Status: states[i].Status, LibraryItemID: states[i].LibraryItemID, Token: tok}
	}
	slices.SortStableFunc(results, func(a, b SearchResult) int {
		return b.PublishDate.Compare(a.PublishDate)
	})
	writeJSON(w, http.StatusOK, results)
}

func (s *server) handleIndexers(w http.ResponseWriter, r *http.Request) {
	st, err := s.Settings.Get(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if st.Prowlarr.URL == "" {
		writeError(w, http.StatusConflict, "Prowlarr isn't set up yet. An admin can add it in Settings > Indexers.")
		return
	}
	list, err := s.prowlarrClient(st.Prowlarr.URL, st.Prowlarr.APIKey).Indexers(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Couldn't reach Prowlarr: "+err.Error())
		return
	}
	// Prowlarr skips indexers that offer none of the searched categories, so
	// picking one would only ever return nothing.
	list = slices.DeleteFunc(list, func(i prowlarr.Indexer) bool { return !i.Supports(st.Prowlarr.Categories) })
	writeJSON(w, http.StatusOK, list)
}

func (s *server) handleProwlarrTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request wasn't valid.")
		return
	}
	type result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if strings.TrimSpace(in.URL) == "" {
		writeJSON(w, http.StatusOK, result{Error: "Enter the Prowlarr URL."})
		return
	}
	if err := s.prowlarrClient(in.URL, in.APIKey).Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, result{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result{OK: true})
}
