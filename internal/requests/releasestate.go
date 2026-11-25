package requests

import (
	"context"
	"slices"

	"github.com/digitalbile/softwarr/internal/prowlarr"
)

// What a user can do with a search result.
const (
	ReleaseAvailable   = "available"   // in the library already
	ReleaseRequested   = "requested"   // an open request has it
	ReleaseNoClient    = "no_client"   // no enabled client takes its protocol
	ReleaseRequestable = "requestable" // none of the above
)

// ReleaseState is a release's status. LibraryItemID is set when the release
// is available.
type ReleaseState struct {
	Status        string
	LibraryItemID *int64
}

// ReleaseStates returns the state of each release, in the same order.
func (s *Service) ReleaseStates(ctx context.Context, rels []prowlarr.Release) ([]ReleaseState, error) {
	keys := make([]string, len(rels))
	for i, rel := range rels {
		keys[i] = rel.Key()
	}
	available := map[string]int64{}
	requested := map[string]bool{}
	if len(keys) > 0 {
		rows, err := s.q.ListAvailableLibraryKeys(ctx, keys)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			available[row.ReleaseKey] = row.ID
		}
		open, err := s.q.ListOpenRequestKeys(ctx, keys)
		if err != nil {
			return nil, err
		}
		for _, k := range open {
			requested[k] = true
		}
	}
	protocols, err := s.q.ListEnabledClientProtocols(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ReleaseState, len(rels))
	for i, rel := range rels {
		if id, ok := available[keys[i]]; ok {
			out[i] = ReleaseState{Status: ReleaseAvailable, LibraryItemID: &id}
		} else if requested[keys[i]] {
			out[i].Status = ReleaseRequested
		} else if !slices.Contains(protocols, rel.Protocol) {
			out[i].Status = ReleaseNoClient
		} else {
			out[i].Status = ReleaseRequestable
		}
	}
	return out, nil
}
