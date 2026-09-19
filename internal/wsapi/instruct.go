package wsapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// InstructFolder writes standing instructions. THE ORIGIN MUST BE THE PERSON:
// inferred or organizer text is an Observation, not Guidance.
func (s *Service) InstructFolder(ctx context.Context, req InstructRequest) (GuidanceItem, error) {
	if err := s.ready(ctx); err != nil {
		return GuidanceItem{}, err
	}
	if err := requirePersonOrigin(req.Provenance); err != nil {
		return GuidanceItem{}, err
	}
	name, err := s.scopeName(ctx, req.ScopeID)
	if err != nil {
		return GuidanceItem{}, err
	}
	g := workspace.Guidance{
		ScopeID:   req.ScopeID,
		Text:      req.Text,
		Status:    workspace.GuidanceActive,
		Origin:    workspace.OriginPerson,
		Actor:     req.Provenance.Actor,
		SourceRef: req.Provenance.Evidence,
		Revision:  1,
	}
	existing, err := s.store.ListGuidance(ctx, req.ScopeID)
	if err != nil {
		return GuidanceItem{}, wrapStoreError(err)
	}
	if prev := lastActive(existing); prev.ID != "" {
		g.Supersedes = prev.ID
		g.Revision = prev.Revision + 1
	}
	stored, err := s.store.PutGuidance(ctx, g)
	if err != nil {
		return GuidanceItem{}, wrapStoreError(err)
	}
	return guidanceItem(stored, name), nil
}

func requirePersonOrigin(p workspace.Provenance) error {
	switch p.Origin {
	case "", workspace.OriginPerson:
		return nil
	default:
		return fmt.Errorf("%w: InstructFolder requires person origin", workspace.ErrInvalid)
	}
}

func lastActive(rows []workspace.Guidance) workspace.Guidance {
	var last workspace.Guidance
	for _, g := range rows {
		if g.Status == workspace.GuidanceActive {
			last = g
		}
	}
	return last
}

func guidanceItem(g workspace.Guidance, name string) GuidanceItem {
	return GuidanceItem{
		ScopeID:   g.ScopeID,
		Name:      name,
		Text:      g.Text,
		Origin:    g.Origin,
		Actor:     g.Actor,
		SourceRef: g.SourceRef,
		Revision:  g.Revision,
	}
}

func (s *Service) scopeName(ctx context.Context, scopeID string) (string, error) {
	if scopeID == "" {
		return "", nil
	}
	collection, err := s.lookup(ctx, scopeID)
	if err != nil {
		return "", err
	}
	return collection.Name, nil
}

// EffectiveGuidance loads known applicable instructions directly — not by
// similarity ranking. Root appears once. Incompatible sibling texts set Conflict.
func (s *Service) EffectiveGuidance(ctx context.Context, conversationID string) (EffectiveGuidance, error) {
	if err := s.ready(ctx); err != nil {
		return EffectiveGuidance{}, err
	}
	scopes, err := s.scopesForConversation(ctx, conversationID)
	if err != nil {
		return EffectiveGuidance{}, err
	}
	items := make([]GuidanceItem, 0)
	revs := make([]GuidanceRev, 0)
	for _, scope := range scopes {
		item, ok, err := s.activeItem(ctx, scope)
		if err != nil {
			return EffectiveGuidance{}, err
		}
		if !ok {
			continue
		}
		items = append(items, item)
		revs = append(revs, GuidanceRev{ScopeID: item.ScopeID, Revision: item.Revision})
	}
	root, err := rootRevision(ctx, s.store)
	if err != nil {
		return EffectiveGuidance{}, err
	}
	conflict, err := s.guidanceConflicts(ctx, items)
	if err != nil {
		return EffectiveGuidance{}, err
	}
	return EffectiveGuidance{
		Items:    items,
		Snapshot: GuidanceSnapshot{RootRevision: root, Guidance: revs},
		Conflict: conflict,
	}, nil
}

func (s *Service) activeItem(ctx context.Context, scopeID string) (GuidanceItem, bool, error) {
	rows, err := s.store.ListGuidance(ctx, scopeID)
	if err != nil {
		return GuidanceItem{}, false, wrapStoreError(err)
	}
	active := lastActive(rows)
	if active.ID == "" {
		return GuidanceItem{}, false, nil
	}
	name, err := s.scopeName(ctx, scopeID)
	if err != nil {
		return GuidanceItem{}, false, err
	}
	return guidanceItem(active, name), true, nil
}

func (s *Service) scopesForConversation(ctx context.Context, conversationID string) ([]string, error) {
	seen := map[string]struct{}{"": {}}
	order := []string{""}
	parents, err := s.store.CollectionsFor(ctx, conversationRef(conversationID))
	if err != nil {
		return nil, wrapStoreError(err)
	}
	stack := make([]string, 0, len(parents))
	for _, parent := range parents {
		stack = append(stack, parent.ID)
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		order = append(order, id)
		next, err := s.store.CollectionsFor(ctx, collectionRef(id))
		if err != nil {
			return nil, wrapStoreError(err)
		}
		for _, parent := range next {
			stack = append(stack, parent.ID)
		}
	}
	return order, nil
}

func (s *Service) guidanceConflicts(ctx context.Context, items []GuidanceItem) (bool, error) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			related, err := s.scopesRelated(ctx, items[i].ScopeID, items[j].ScopeID)
			if err != nil {
				return false, err
			}
			if related || isRefinement(items[i].Text, items[j].Text) {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) scopesRelated(ctx context.Context, a, b string) (bool, error) {
	if a == b || a == "" || b == "" {
		return true, nil
	}
	if ok, err := s.scopeHasAncestor(ctx, a, b); err != nil || ok {
		return ok, err
	}
	return s.scopeHasAncestor(ctx, b, a)
}

func (s *Service) scopeHasAncestor(ctx context.Context, id, ancestor string) (bool, error) {
	walked := map[string]struct{}{}
	stack := []string{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := walked[cur]; ok {
			continue
		}
		walked[cur] = struct{}{}
		parents, err := s.store.CollectionsFor(ctx, collectionRef(cur))
		if err != nil {
			return false, wrapStoreError(err)
		}
		for _, parent := range parents {
			if parent.ID == ancestor {
				return true, nil
			}
			stack = append(stack, parent.ID)
		}
	}
	return false, nil
}

func isRefinement(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}
