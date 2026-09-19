package wsapi

import (
	"context"
)

const defaultSearchLimit = 20

// SearchEvidence is hybrid: lexical candidates plus embedding (or labelled
// expansion) candidates, deduped by source id. A missing discoverer returns
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
	lexical, err := s.disc.SearchLexical(ctx, q.Query, limit)
	if err != nil {
		return nil, err
	}
	embed, _ := s.disc.SearchEmbed(ctx, q.Query, limit)
	merged := mergeHits(lexical, embed)
	out := make([]SearchHit, 0, len(merged))
	for _, hit := range merged {
		if hitMatches(hit, q) {
			out = append(out, hit)
		}
	}
	return out, nil
}

func mergeHits(lexical, embed []SearchHit) []SearchHit {
	bySource := map[string]SearchHit{}
	order := make([]string, 0)
	for _, hit := range lexical {
		key := sourceKey(hit)
		if _, ok := bySource[key]; ok {
			continue
		}
		if hit.ScoreKind == "" {
			hit.ScoreKind = ScoreBM25
		}
		bySource[key] = hit
		order = append(order, key)
	}
	for _, hit := range embed {
		key := sourceKey(hit)
		if hit.ScoreKind == "" {
			if hit.Degraded {
				hit.ScoreKind = ScoreExpansion
			} else {
				hit.ScoreKind = ScoreEmbed
			}
		}
		if _, ok := bySource[key]; !ok {
			order = append(order, key)
		}
		bySource[key] = hit
	}
	out := make([]SearchHit, 0, len(order))
	for _, key := range order {
		out = append(out, bySource[key])
	}
	return out
}

func sourceKey(hit SearchHit) string {
	if hit.SessionID != "" {
		return hit.SessionID + "\x00" + hit.Ref
	}
	return hit.Ref
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
