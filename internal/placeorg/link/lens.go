package link

import (
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// groupLenses turns each demoted group into one view on its parent. The
// parent has to be a node already: this stage does not create a place to
// hang a view on, and a group with nowhere to hang is dropped.
func groupLenses(t *placeorg.Tree, corpus *placeorg.Corpus, demoted []placeorg.Op) []placeorg.Op {
	var ops []placeorg.Op
	for _, op := range demoted {
		if op.Kind != placeorg.OpDemote || op.Parent == "" {
			continue
		}
		if _, ok := t.Node(op.Parent); !ok {
			continue
		}
		members := listed(corpus, op.Members)
		if len(members) == 0 {
			continue
		}
		ops = append(ops, placeorg.Op{
			Kind:    placeorg.OpLens,
			Stage:   placeorg.StageLink,
			Place:   op.Parent,
			Name:    groupName(corpus, members),
			Members: members,
			Into:    axisGroup,
			Why:     whyGroup,
		})
	}
	return ops
}

// monthLenses adds one by-month view to each top-level place whose subtree
// has at least monthPlaceChats countable chats. Child places are not given
// one here: the reference attaches the view to roots, and that is the count
// the parity check compares. A smaller place still gets the view when an
// axis says month; axisLenses does that.
func monthLenses(t *placeorg.Tree, corpus *placeorg.Corpus) []placeorg.Op {
	var ops []placeorg.Op
	for _, id := range t.Roots() {
		op, ok := monthOn(t, corpus, id)
		if !ok {
			continue
		}
		if len(countableIDs(corpus, subtree(t, id))) < monthPlaceChats {
			continue
		}
		ops = append(ops, op)
	}
	return ops
}

// monthOn builds the by-month view for one place that already exists.
func monthOn(t *placeorg.Tree, corpus *placeorg.Corpus, id string) (placeorg.Op, bool) {
	if _, ok := t.Node(id); !ok {
		return placeorg.Op{}, false
	}
	members := listed(corpus, subtree(t, id))
	if len(members) == 0 {
		return placeorg.Op{}, false
	}
	return placeorg.Op{
		Kind:    placeorg.OpLens,
		Stage:   placeorg.StageLink,
		Place:   id,
		Name:    nameByMonth,
		Members: members,
		Into:    axisMonth,
		Why:     whyByMonth,
	}, true
}

// axisLenses adds the views State.Axes names. A month axis adds the by-month
// view even on a place under the size cut, once. Any other axis adds one
// view per stored value, and only the chats that carry that value. An axis
// with no values adds nothing: the stage does not invent groups. A value no
// chat carries adds nothing either.
func axisLenses(t *placeorg.Tree, corpus *placeorg.Corpus, state *placeorg.State, haveMonth map[string]bool) []placeorg.Op {
	if state == nil || len(state.Axes) == 0 {
		return nil
	}
	ids := make([]string, 0, len(state.Axes))
	for id := range state.Axes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	chats := chatsByID(corpus)
	var ops []placeorg.Op
	for _, id := range ids {
		if _, ok := t.Node(id); !ok {
			continue
		}
		axis := state.Axes[id]
		if axis.Kind == placeorg.AxisMonth {
			if haveMonth[id] {
				continue
			}
			op, ok := monthOn(t, corpus, id)
			if ok {
				ops = append(ops, op)
			}
			continue
		}
		into, why, ok := axisWords(axis.Kind)
		if !ok {
			continue
		}
		members := listed(corpus, subtree(t, id))
		for _, value := range axis.Values {
			if value == "" {
				continue
			}
			hit := carrying(chats, corpus, members, value)
			if len(hit) == 0 {
				continue
			}
			ops = append(ops, placeorg.Op{
				Kind:    placeorg.OpLens,
				Stage:   placeorg.StageLink,
				Place:   id,
				Name:    value,
				Members: hit,
				Into:    into,
				Why:     why,
			})
		}
	}
	return ops
}

// axisWords maps a stored axis onto the lens axis and the line a person
// reads. Month is handled by the caller. An unknown kind is refused rather
// than rendered as a guess.
func axisWords(kind placeorg.AxisKind) (into, why string, ok bool) {
	switch kind {
	case placeorg.AxisProject:
		return axisAnchor, whyProject, true
	case placeorg.AxisWorkspace:
		return axisAnchor, whyFolder, true
	case placeorg.AxisKindOfWork:
		return axisKind, whyKindWork, true
	case placeorg.AxisCustom:
		return axisAnchor, whyCustom, true
	default:
		return "", "", false
	}
}

// carrying keeps the chats in members whose anchor, folder or repo key
// equals value. The match is exact: a different spelling is a different
// value, and this stage does not fold them together.
func carrying(chats map[string]placeorg.Chat, corpus *placeorg.Corpus, members []string, value string) []string {
	var out []string
	for _, id := range members {
		chat, ok := chats[id]
		if !ok {
			continue
		}
		if chat.Workspace == value || chat.RepoKey == value {
			out = append(out, id)
			continue
		}
		if corpus != nil && corpus.Anchors != nil {
			if _, has := corpus.Anchors[id][value]; has {
				out = append(out, id)
			}
		}
	}
	return out
}

func chatsByID(corpus *placeorg.Corpus) map[string]placeorg.Chat {
	if corpus == nil {
		return nil
	}
	out := make(map[string]placeorg.Chat, len(corpus.Chats))
	for _, chat := range corpus.Chats {
		if _, ok := out[chat.ID]; !ok {
			out[chat.ID] = chat
		}
	}
	return out
}

// listed copies ids, drops probes, and sorts them. The copy matters: the
// backing array may be a node's Members, and sorting it would change the tree.
func listed(corpus *placeorg.Corpus, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || !shown(corpus, id) {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func countableIDs(corpus *placeorg.Corpus, ids []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] || !countable(corpus, id) {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
