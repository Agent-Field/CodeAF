package steer

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Render turns a validated Steer into ops over its scope.
//
// How an op carries a steer: OpAxis names the axis in Name and, for a custom
// axis, the person's values in Children; an empty Name removes the axis. A
// lens is an OpLens whose Name is the view's name. A split is one OpSplit per
// child, with Place the scope, Name the child's value and Members the chats
// that go into it. Splits, dissolves and every other structural change are
// Proposed; the axis and a lens change no structure and apply at once.
func (st *Steerer) Render(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, s placeorg.Steer, judge placeorg.Judge) ([]placeorg.Op, error) {
	if s.Action == placeorg.SteerNone {
		return nil, nil
	}
	scope, ok := t.Node(s.Scope)
	if !ok {
		return nil, fmt.Errorf("steer: scope %q is not a place in the tree", s.Scope)
	}
	if s.Action == placeorg.SteerFlat {
		return flatten(t, scope), nil
	}
	if s.Axis == placeorg.AxisMonth {
		return []placeorg.Op{axisOp(scope.ID, s, false), {
			Kind: placeorg.OpLens, Stage: stage, Place: scope.ID, Name: "by month",
			Why: "a view of " + scope.Name + " by the month each chat began",
		}}, nil
	}
	members := considered(t, corpus, scope.ID)
	groups, err := st.groups(ctx, corpus, s, members, judge)
	if err != nil {
		return nil, err
	}
	ops := []placeorg.Op{axisOp(scope.ID, s, true)}
	for _, g := range groups {
		ops = append(ops, placeorg.Op{
			Kind: placeorg.OpSplit, Stage: stage, Place: scope.ID, Name: g.value, Members: g.chats, Proposed: true,
			Why: fmt.Sprintf("%d chats in %s are about %s", len(g.chats), scope.Name, g.value),
		})
	}
	return ops, nil
}

const stage = "steer"

// axisOp is the op that stores the axis on the place. A proposed axis waits
// with the splits it came with; a lens-only axis applies at once.
func axisOp(place string, s placeorg.Steer, proposed bool) placeorg.Op {
	return placeorg.Op{
		Kind: placeorg.OpAxis, Stage: stage, Place: place, Name: string(s.Axis), Children: s.Values, Proposed: proposed,
		Why: "future chats in this place are filed by " + string(s.Axis),
	}
}

// flatten proposes dissolving every descendant into the place, deepest first
// so a child never dissolves into a parent that is already gone. The axis is
// removed with it.
func flatten(t *placeorg.Tree, scope placeorg.Node) []placeorg.Op {
	ops := []placeorg.Op{{Kind: placeorg.OpAxis, Stage: stage, Place: scope.ID, Why: "no axis: chats stay in " + scope.Name, Proposed: true}}
	var walk func(id string)
	walk = func(id string) {
		n, _ := t.Node(id)
		for _, c := range n.Children {
			walk(c)
			ops = append(ops, placeorg.Op{
				Kind: placeorg.OpDissolve, Stage: stage, Place: c, Proposed: true,
				Why: "flattening " + scope.Name + ": its chats stay in the place",
			})
		}
	}
	walk(scope.ID)
	return ops
}

// group is one proposed child: a value and the chats that carry it.
type group struct {
	value string
	chats []string
}

// groups dispatches on the axis.
func (st *Steerer) groups(ctx context.Context, corpus *placeorg.Corpus, s placeorg.Steer, members []string, judge placeorg.Judge) ([]group, error) {
	chats := byID(corpus)
	switch s.Axis {
	case placeorg.AxisProject:
		return bucket(members, anchorValues(corpus, members)), nil
	case placeorg.AxisWorkspace:
		return bucket(members, func(id string) string { return filepath.Base(chats[id].Workspace) }), nil
	case placeorg.AxisKindOfWork:
		labels := kindLabels(ctx, chats, members, judge)
		return bucket(members, func(id string) string { return labels[id] }), nil
	case placeorg.AxisCustom:
		return bucket(members, nearestValue(chats, s.Values)), nil
	}
	return nil, fmt.Errorf("steer: axis %q has no split", s.Axis)
}

// byID indexes the corpus.
func byID(corpus *placeorg.Corpus) map[string]placeorg.Chat {
	m := make(map[string]placeorg.Chat, len(corpus.Chats))
	for _, c := range corpus.Chats {
		m[c.ID] = c
	}
	return m
}

// considered lists the distinct conversations at or below a place: probes
// and folded duplicates never count toward a group.
func considered(t *placeorg.Tree, corpus *placeorg.Corpus, place string) []string {
	var out []string
	var walk func(id string)
	walk = func(id string) {
		n, _ := t.Node(id)
		for _, m := range n.Members {
			if k := corpus.Kinds[m]; k != placeorg.KindProbe && k != placeorg.KindDuplicate {
				out = append(out, m)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(place)
	sort.Strings(out)
	return out
}

// bucket groups chats by value and keeps the values that reach Quorum. A
// value every member carries separates nothing, so it is not a child.
func bucket(members []string, value func(string) string) []group {
	by := map[string][]string{}
	for _, id := range members {
		if v := value(id); v != "" {
			by[v] = append(by[v], id)
		}
	}
	var out []group
	for v, ids := range by {
		if len(ids) >= placeorg.Quorum && len(ids) < len(members) {
			out = append(out, group{value: v, chats: ids})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].chats) != len(out[j].chats) {
			return len(out[i].chats) > len(out[j].chats)
		}
		return out[i].value < out[j].value
	})
	return out
}

// anchorValues gives each chat its heaviest anchor among the anchors that
// enough of the place's chats share: a child must be an anchor value at or
// above Quorum, so a chat's own rare anchor never makes a place of one.
func anchorValues(corpus *placeorg.Corpus, members []string) func(string) string {
	support := map[string]int{}
	for _, id := range members {
		for a := range corpus.Anchors[id] {
			support[a]++
		}
	}
	return func(id string) string {
		best, bestW := "", float32(-1)
		for a, w := range corpus.Anchors[id] {
			if support[a] < placeorg.Quorum {
				continue
			}
			if w > bestW || (w == bestW && (support[a] > support[best] || (support[a] == support[best] && a < best))) {
				best, bestW = a, w
			}
		}
		return best
	}
}

// nearestValue gives a chat the person's value it mentions most, a title
// mention counting double. This is lexical on purpose: a steer holds no
// vectors, and a chat that mentions none of the values stays where it is.
func nearestValue(chats map[string]placeorg.Chat, values []string) func(string) string {
	return func(id string) string {
		c := chats[id]
		title, text := strings.ToLower(c.Title), strings.ToLower(c.Text)
		best, bestScore := "", 0
		for _, v := range values {
			score := 2*strings.Count(title, stem(v)) + strings.Count(text, stem(v))
			if score > bestScore {
				best, bestScore = v, score
			}
		}
		return best
	}
}

// stemRunes is how much of a value must match, so "papers" finds "paper".
const stemRunes = 5

func stem(v string) string {
	if r := []rune(v); len(r) > stemRunes {
		return string(r[:stemRunes])
	}
	return v
}
