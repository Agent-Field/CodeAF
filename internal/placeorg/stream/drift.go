package stream

import (
	"context"
	"errors"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Drift weighs a settled chat at its END anchor without changing its primary
// home. The caller supplies both anchors in the tree's space after the existing
// settle signal; this stage owns neither an embedding door nor a clock.
// Extra homes are earned at either anchor. A different primary is only an offer.
func (s *Stream) Drift(ctx context.Context, t *placeorg.Tree, start placeorg.Arrival, end []float32, judge placeorg.Judge) ([]placeorg.Op, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil || start.Chat.ID == "" || start.Chat.EndText == "" || len(end) == 0 {
		return nil, nil
	}
	if len(start.Vector) > 0 && len(start.Vector) != len(end) {
		return nil, errors.New("stream: START and END dimensions differ")
	}
	home, ok := t.Home(start.Chat.ID)
	if !ok {
		// The first filing still belongs to START, including a chat in Now.
		return nil, nil
	}
	if s != nil && s.Kinds[start.Chat.ID] == placeorg.KindProbe {
		return nil, nil
	}
	end = unit(end)
	if len(end) == 0 {
		return nil, nil
	}
	ops := s.driftHomes(t, start, end, home)
	a := start
	a.Vector = end
	a.Chat.Text = a.Chat.EndText
	// A contest must read the ending rather than the original title alone.
	a.Chat.Title = ""
	filing, err := s.File(ctx, t, a, judge)
	if err != nil {
		return nil, err
	}
	if filing.Op.Kind != placeorg.OpInbox && filing.Op.Place != home {
		filing.Op.Proposed = true
		filing.Op.Why = "This chat now fits " + shownPlace(t, filing.Op.Place) + "; move its primary home?"
		ops = append(ops, filing.Op)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ops, nil
}

// driftHomes keeps every earned destination without subtracting a replacement
// fit: the recorded primary remains the primary even when neither anchor earns
// it. Existing homes occupy their branches, so settling twice adds no duplicates.
func (s *Stream) driftHomes(t *placeorg.Tree, a placeorg.Arrival, end []float32, home string) []placeorg.Op {
	th := s.thresholds()
	delta := th.Delta
	if s == nil || s.Thresholds.Floor == 0 {
		delta = defaultHomeDelta
	}
	delta = max(0, delta)
	earned := map[string]placeorg.Op{}
	depth := t.Depth()
	for _, v := range [][]float32{unit(a.Vector), end} {
		_, _, all := scorePlaces(t, s.space(), v)
		best := float32(-1)
		for _, p := range all {
			if p.depth == depth {
				best = max(best, p.score)
			}
		}
		bar := max(th.Floor, best-delta)
		for _, p := range all {
			if p.depth != depth || p.score < bar {
				continue
			}
			op := placeorg.Op{Kind: placeorg.OpLink, Stage: stage, Place: p.id, Chat: a.Chat.ID, Scores: []float32{p.score, bar}, Why: "Also in this place."}
			if old, ok := earned[p.id]; !ok || p.score > old.Scores[0] {
				earned[p.id] = op
			}
		}
	}
	var ranked []placeorg.Op
	for _, op := range earned {
		ranked = append(ranked, op)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Scores[0] != ranked[j].Scores[0] {
			return ranked[i].Scores[0] > ranked[j].Scores[0]
		}
		return ranked[i].Place < ranked[j].Place
	})
	occupied := map[string]bool{}
	branches := map[string]bool{}
	for _, id := range append(t.Links(a.Chat.ID), home) {
		occupied[id] = true
		if n, ok := t.Node(id); ok && n.Parent != "" {
			branches[n.Parent] = true
		}
	}
	var ops []placeorg.Op
	for _, op := range ranked {
		n, _ := t.Node(op.Place)
		if occupied[op.Place] || branches[n.Parent] && n.Parent != "" {
			continue
		}
		if n.Parent != "" {
			branches[n.Parent] = true
		}
		ops = append(ops, op)
	}
	return ops
}

// The earned-home band is distinct from Chow's descent margin.
const defaultHomeDelta float32 = 0.03

func shownPlace(t *placeorg.Tree, id string) string {
	n, _ := t.Node(id)
	return shown(place{name: n.Name})
}
