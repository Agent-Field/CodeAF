package placegraph

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// PlanKind names the store operations an organiser can propose. Creations carry
// no sources or permissions; merges follow the existing graph rules.
type PlanKind string

const (
	PlanCreate   PlanKind = "create"
	PlanRename   PlanKind = "rename"
	PlanReparent PlanKind = "reparent"
	PlanMerge    PlanKind = "merge"
	PlanDelete   PlanKind = "delete"
	PlanFile     PlanKind = "file"
	PlanMove     PlanKind = "move"
	PlanLink     PlanKind = "link"
	ActionPlan   Action   = "places.plan"
)

// PlanOp uses caller-bound place ids so later operations can refer to a place
// created earlier in the same plan. FromPlaceID is required for moves and merges;
// NowID represents the absence of a membership for moves.
type PlanOp struct {
	Kind        PlanKind `json:"kind"`
	PlaceID     string   `json:"placeId"`
	Name        string   `json:"name,omitempty"`
	Parents     []string `json:"parents,omitempty"`
	ChatID      string   `json:"chatId,omitempty"`
	FromPlaceID string   `json:"fromPlaceId,omitempty"`
}

// ApplyPlan evaluates every operation on a private copy under the store lock.
// ONE PLAN IS ONE COMMIT: no file replacement or receipt happens until every
// operation and the final graph pass validation. Earlier creates may be used by
// later operations; a refusal discards the entire copy.
func (s *Store) ApplyPlan(ops []PlanOp) (Receipt, error) {
	return s.applyPlan(ops, nil, nil)
}

// applyPlan also checks an offered revision under the same lock as the write,
// closing the gap between reading an offer and accepting it in another window.
func (s *Store) applyPlan(ops []PlanOp, revision *uint64, guard func(*State) error) (Receipt, error) {
	return s.mutate(func(st *State, now time.Time) (*change, error) {
		if revision != nil && st.Revision != *revision {
			return nil, ErrProposalGone
		}
		before := st.clone()
		for i, op := range ops {
			if err := s.planOp(st, op, now); err != nil {
				return nil, fmt.Errorf("plan operation %d: %w", i+1, err)
			}
		}
		if _, err := validateState(st, false); err != nil {
			return nil, err
		}
		for _, p := range st.Places {
			if !p.Archived && nameClash(st, p.Name, p.Parents, p.ID) {
				return nil, ErrNameTaken
			}
		}
		if guard != nil {
			if err := guard(st); err != nil {
				return nil, err
			}
		}
		if reflect.DeepEqual(before, st) {
			return nil, nil
		}
		return &change{ActionPlan, ""}, nil
	})
}

func (s *Store) planOp(st *State, op PlanOp, now time.Time) error {
	switch op.Kind {
	case PlanCreate:
		if err := validID(op.PlaceID); err != nil {
			return err
		}
		if st.find(op.PlaceID) != nil {
			return fmt.Errorf("%w: duplicate place id", ErrInvalid)
		}
		name, err := cleanName(op.Name)
		if err != nil {
			return err
		}
		parents, err := normalizeParents(op.Parents)
		if err != nil {
			return err
		}
		for _, id := range parents {
			p, err := st.needPlace(id)
			if err != nil {
				return err
			}
			if p.Archived {
				return ErrArchived
			}
		}
		tint := Tint("")
		if len(parents) == 0 {
			tint = suggestTint(st)
		}
		st.Places = append(st.Places, Place{ID: op.PlaceID, Name: name, Parents: parents, Tint: tint, CreatedAt: now})
		return nil
	case PlanRename:
		p, err := st.needPlace(op.PlaceID)
		if err != nil {
			return err
		}
		name, err := cleanName(op.Name)
		if err != nil {
			return err
		}
		p.Name = name
		return nil
	case PlanReparent:
		_, err := reparent(st, op.PlaceID, op.Parents)
		return err
	case PlanDelete:
		p, err := st.needPlace(op.PlaceID)
		if err != nil {
			return err
		}
		parents := append([]string{}, p.Parents...)
		_, err = detach(st, op.PlaceID, func(*Place) []string { return parents })
		return err
	case PlanMerge:
		_, err := s.mergePlan(st, op.FromPlaceID, op.PlaceID, &MergeResult{})
		return err
	case PlanFile, PlanLink, PlanMove:
		return planMembership(st, op, now)
	default:
		return fmt.Errorf("%w: unknown plan operation %q", ErrInvalid, op.Kind)
	}
}

func planMembership(st *State, op PlanOp, now time.Time) error {
	if err := validChatID(op.ChatID); err != nil {
		return err
	}
	if op.Kind == PlanMove {
		if op.FromPlaceID != NowID && st.hasMembership(op.ChatID, op.FromPlaceID) < 0 {
			return ErrNotFound
		}
		if op.FromPlaceID == op.PlaceID {
			return nil
		}
		if op.FromPlaceID != NowID {
			st.removeMembership(op.ChatID, op.FromPlaceID)
		}
		if op.PlaceID == NowID {
			return nil
		}
	}
	p, err := st.needPlace(op.PlaceID)
	if err != nil {
		return err
	}
	if p.Archived {
		return ErrArchived
	}
	link := op.Kind == PlanLink
	if i := st.hasMembership(op.ChatID, op.PlaceID); i >= 0 {
		// A shortcut never demotes a home; filing or moving promotes a shortcut.
		if !link && st.Memberships[i].Link {
			st.Memberships[i].Link = false
			st.Memberships[i].AddedBy = AddedByAI
			st.Memberships[i].At = now
		}
		return nil
	}
	if err := st.addMembership(op.ChatID, op.PlaceID, AddedByAI, now); err != nil {
		return err
	}
	st.Memberships[len(st.Memberships)-1].Link = link
	return nil
}
func (s *Store) mergePlan(st *State, from, into string, res *MergeResult) (*change, error) {
	if from == into {
		return nil, fmt.Errorf("%w: cannot merge a place into itself", ErrInvalid)
	}
	src, err := st.needPlace(from)
	if err != nil {
		return nil, err
	}
	dst, err := st.needPlace(into)
	if err != nil {
		return nil, err
	}
	if dst.Archived {
		return nil, fmt.Errorf("%w: %s", ErrArchived, into)
	}
	// Context first, so a size failure aborts before anything moves.
	merged := dst.Context
	merged.Sources = append([]Source(nil), dst.Context.Sources...)
	if src.Context.Instructions != "" && src.Context.Instructions != dst.Context.Instructions {
		if merged.Instructions != "" {
			merged.Instructions += "\n\n"
		}
		merged.Instructions += src.Context.Instructions
	}
	for _, so := range src.Context.Sources {
		dup := false
		for _, d := range merged.Sources {
			if d.Kind == so.Kind && d.Ref == so.Ref {
				dup = true
			}
		}
		if !dup {
			for containsSourceID(merged.Sources, so.ID) {
				so.ID = s.opts.NewID("src_")
			}
			merged.Sources = append(merged.Sources, so)
		}
	}
	if err := validateContext(merged, into); err != nil {
		return nil, err
	}
	below := st.descendantIDs(into)
	srcParents := append([]string{}, src.Parents...)
	// A child of `from` that is itself an ancestor of `into` cannot take `into`
	// as a parent without a cycle; it is decided now, before any edge moves.
	aboveInto := map[string]bool{}
	for _, c := range st.childrenOf()[from] {
		if c != into && st.descendantIDs(c)[into] {
			aboveInto[c] = true
		}
	}
	// Parent union.
	for _, par := range srcParents {
		if par == into || below[par] {
			res.SkippedParents = append(res.SkippedParents, par)
			continue
		}
		if !containsString(dst.Parents, par) {
			dst.Parents = append(dst.Parents, par)
		}
	}
	// Memberships union.
	have := map[string]int{}
	for i, m := range st.Memberships {
		if m.PlaceID == into {
			have[m.ChatID] = i
		}
	}
	for i := range st.Memberships {
		m := &st.Memberships[i]
		if m.PlaceID != from {
			continue
		}
		res.MembershipsMoved++
		if existing, ok := have[m.ChatID]; ok {
			// The existing provenance stays, but a home must survive a union with
			// a shortcut rather than silently becoming only a shortcut.
			if !m.Link {
				st.Memberships[existing].Link = false
			}
			continue
		}
		have[m.ChatID] = i
		m.PlaceID = into
	}
	for i := range st.Lines {
		if st.Lines[i].PlaceID == from {
			st.Lines[i].PlaceID = into
		}
	}
	dst.Context = merged
	if dst.Policy.Model == "" {
		dst.Policy.Model = src.Policy.Model
	}
	if dst.Policy.Permissions == "" {
		dst.Policy.Permissions = src.Policy.Permissions
	}
	if len(dst.Manager) == 0 {
		dst.Manager = append(json.RawMessage(nil), src.Manager...)
	}
	if i := indexOf(st.Pinned, from); i >= 0 && !containsString(st.Pinned, into) {
		st.Pinned[i] = into
	}
	moved, err := detach(st, from, func(child *Place) []string {
		if child.ID == into {
			return nil // the survivor already took the merged place's parents above
		}
		if !aboveInto[child.ID] {
			return []string{into}
		}
		return srcParents // `into` lives below this child: attaching it there would cycle
	})
	if err != nil {
		return nil, err
	}
	res.ChildrenMoved = moved
	if names := st.cycleNames(); names != "" {
		return nil, fmt.Errorf("%w: %s", ErrCycle, names)
	}
	res.Into = st.find(into).clone()
	if res.SkippedParents == nil {
		res.SkippedParents = []string{}
	}
	return &change{ActionMerge, into}, nil
}
