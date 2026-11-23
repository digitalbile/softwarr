package httpapi

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/prowlarr"
)

const prowlarrTimeout = 60 * time.Second

type SearchResult struct {
	prowlarr.Release
	Key           string `json:"key"`
	Status        string `json:"status"` // "available" | "requested" | "requestable" | "no_client"
	LibraryItemID *int64 `json:"libraryItemId"`
}

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
	var indexerIDs []int
	if p, _ := auth.FromContext(ctx); p.IsAdmin {
		for _, f := range strings.Split(r.URL.Query().Get("indexerIds"), ",") {
			if id, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
				indexerIDs = append(indexerIDs, id)
			}
		}
	}
	releases, err := s.prowlarrClient(st.Prowlarr.URL, st.Prowlarr.APIKey).Search(ctx, q, st.Prowlarr.Categories, indexerIDs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Couldn't reach Prowlarr: "+err.Error())
		return
	}

	results := make([]SearchResult, len(releases))
	keys := make([]string, len(releases))
	for i, rel := range releases {
		keys[i] = rel.Key()
		results[i] = SearchResult{Release: rel, Key: keys[i]}
	}
	available := map[string]int64{}
	requested := map[string]bool{}
	if len(keys) > 0 {
		rows, err := s.Q.ListAvailableLibraryKeys(ctx, keys)
		if err != nil {
			serverError(w, err)
			return
		}
		for _, row := range rows {
			available[row.ReleaseKey] = row.ID
		}
		open, err := s.Q.ListOpenRequestKeys(ctx, keys)
		if err != nil {
			serverError(w, err)
			return
		}
		for _, k := range open {
			requested[k] = true
		}
	}
	protocols, err := s.Q.ListEnabledClientProtocols(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	for i := range results {
		res := &results[i]
		if id, ok := available[res.Key]; ok {
			res.Status = "available"
			res.LibraryItemID = &id
		} else if requested[res.Key] {
			res.Status = "requested"
		} else if !slices.Contains(protocols, res.Protocol) {
			res.Status = "no_client"
		} else {
			res.Status = "requestable"
		}
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
