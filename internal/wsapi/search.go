package wsapi

import (
	"context"
)

const defaultSearchLimit = 20

// SearchEvidence is hybrid: a wide lexical pool plus embedding (or labelled
// expansion) candidates, ranked as one list and truncated to Limit. Restating
// the query is not a hit on the original passage. A missing discoverer returns
// no hits — it does not invent membership or claim the workspace was checked.
func (s *Service) SearchEvidence(ctx context.Context, q SearchQuery) ([]SearchHit, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if s.disc == nil || q.Query == "" {
		return []SearchHit{}, nil
	}
	limit := q.Limit
	if limit < 1 {
		limit = defaultSearchLimit
	}
	lexical, err := s.disc.SearchLexical(ctx, q.Query, searchPool(limit))
	if err != nil {
		return nil, err
	}
	embed, _ := s.disc.SearchEmbed(ctx, q.Query, searchPool(limit))
	merged := rankEvidence(q.Query, lexical, embed, limit)
	out := make([]SearchHit, 0, len(merged))
	for _, hit := range merged {
		if hitMatches(hit, q) {
			out = append(out, hit)
		}
	}
	return out, nil
}

func hitMatches(hit SearchHit, q SearchQuery) bool {
	if q.SessionID != "" && hit.SessionID != q.SessionID {
		return false
	}
	if q.ConversationID != "" && hit.SessionID != q.ConversationID && hit.Ref != q.ConversationID {
		return false
	}
	return true
}

// IndexProgress reports software counters. Nil discoverer is delayed, never 100%.
func (s *Service) IndexProgress(ctx context.Context) (IndexView, error) {
	if err := s.ready(ctx); err != nil {
		return IndexView{}, err
	}
	if s.disc == nil {
		return IndexView{Delayed: true, Detail: delayedDetail}, nil
	}
	view, err := s.disc.IndexProgress(ctx)
	if err != nil {
		return IndexView{}, err
	}
	if view.Delayed && view.Detail == "" {
		view.Detail = delayedDetail
	}
	return view, nil
}
