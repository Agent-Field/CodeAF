package link

import (
	"context"
	"math"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// A view's axis, spelled the way placeorg.Lens.Axis documents it. An Op has
// no axis field, so OpLens carries the axis on Into. The pipeline reads that
// field back into Lens.Axis; a new spelling here is a view the server cannot
// serve.
const (
	axisGroup  = "group"
	axisMonth  = "month"
	axisAnchor = "anchor"
	axisKind   = "kind"
)

// Person-facing lines. The manual quotes these; a change here is a change
// to the page.
const (
	whyAlsoIn   = "Also in this place."
	whyByMonth  = "A view of this place by month."
	whyGroup    = "A view of these chats, not a place."
	whyProject  = "A view of this place by project."
	whyKindWork = "A view of this place by kind of work."
	whyFolder   = "A view of this place by folder."
	whyCustom   = "A view of this place."
	nameByMonth = "by month"
	// nameFallback is the view name when no word of the chats qualifies.
	// It is the reference harness's fallback, and it is a plain word rather
	// than a made-up label.
	nameFallback = "group"
)

// monthPlaceChats is the reference cut: a top-level place with this many
// chats (probes and duplicates not counted) gets a by-month view. The
// reference run has six such places.
const monthPlaceChats = 15

// defaultFloor and defaultDelta are the reference figures, used when the
// tree's space has no calibrated threshold yet. A calibrated space uses
// State.Thresholds, so two spaces never share one floor.
const (
	defaultFloor float32 = 0.45
	defaultDelta float32 = 0.03
)

// Linker implements placeorg.Linker. State is the organiser's file: axes
// and per-space thresholds. Demoted is the auditor's OpDemote list for this
// run, copied in by the runner before Link. Apply has already parked those
// chats on the parent, so the group is no longer a node and this stage
// cannot rediscover it from the tree. A nil Linker records nothing that
// needs State or a demotion; shortcuts and the by-month cut still run.
type Linker struct {
	state *placeorg.State
	// Demoted is set per run. It is not safe to share across runs that
	// overlap, and the runner does not overlap them.
	Demoted []placeorg.Op
	// end holds the end-anchor vector per chat, in corpus order, when the
	// run has later messages to embed; homes is the histogram of the last
	// Link: extra homes per chat, including zero.
	end   [][]float32
	homes map[int]int
}

// New returns a linker that reads axes and thresholds from state. State may
// be nil on a run that has no file yet.
func New(state *placeorg.State) *Linker {
	return &Linker{state: state}
}

// SetDemoted hands the linker this run's demoted groups. The runner calls it
// before Link, so the groups become views even though the tree no longer
// has them.
func (l *Linker) SetDemoted(ops []placeorg.Op) { l.Demoted = ops }

// SetEnd hands the linker the end-anchor vectors, one per corpus chat, so a
// chat earns homes where it ended up as well as where it began. Without
// them only the start anchor counts; unknown renders as nothing.
func (l *Linker) SetEnd(vectors [][]float32) { l.end = vectors }

// Homes is the histogram of extra homes per chat from the last Link.
func (l *Linker) Homes() map[int]int {
	out := make(map[int]int, len(l.homes))
	for k, v := range l.homes {
		out[k] = v
	}
	return out
}

// compile-time check that the stage is the interface the runner holds.
var _ placeorg.Linker = (*Linker)(nil)

// Link returns shortcut ops and view ops. It does not touch the tree.
// vectors[i] is the embedding of corpus.Chats[i], in one space, already
// comparable with the centroids on the nodes. A missing or short vector is
// a chat with no shortcut, not an error: unknown renders as nothing.
func (l *Linker) Link(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, vectors [][]float32) ([]placeorg.Op, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil || corpus == nil {
		return nil, nil
	}
	state, demoted, end := (*placeorg.State)(nil), []placeorg.Op(nil), [][]float32(nil)
	if l != nil {
		state = l.state
		demoted = l.Demoted
		end = l.end
	}
	var ops []placeorg.Op
	ops = append(ops, groupLenses(t, corpus, demoted)...)
	ops = append(ops, monthLenses(t, corpus)...)
	ops = append(ops, axisLenses(t, corpus, state, monthPlaces(ops))...)
	linked, hist, err := shortcutOps(ctx, corpus, vectors, end, t, state)
	if err != nil {
		return nil, err
	}
	if l != nil {
		l.homes = hist
	}
	return append(ops, linked...), nil
}

// monthPlaces is the set of places that already have a by-month view, so a
// stored month axis does not add a second one.
func monthPlaces(ops []placeorg.Op) map[string]bool {
	got := map[string]bool{}
	for _, op := range ops {
		if op.Kind == placeorg.OpLens && op.Into == axisMonth {
			got[op.Place] = true
		}
	}
	return got
}

// forest lists every node reachable from a root, parents before children.
// A cycle, which a forest does not have, stops at the second visit so a
// broken tree cannot hang the run. A node the roots cannot reach is left
// out: the person cannot open it, so it is not a place a chat can also be in.
func forest(t *placeorg.Tree) []placeorg.Node {
	var out []placeorg.Node
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		n, ok := t.Node(id)
		if !ok {
			return
		}
		out = append(out, n)
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, id := range t.Roots() {
		walk(id)
	}
	return out
}

// subtree lists the chats whose home is this place or a place under it.
// The slice is new, so sorting it cannot reorder a node's own Members.
// Place ids and chat ids are tracked apart, so a chat whose id happens to
// equal a place id is still listed.
func subtree(t *placeorg.Tree, id string) []string {
	visited := map[string]bool{}
	chats := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if id == "" || visited[id] {
			return
		}
		visited[id] = true
		n, ok := t.Node(id)
		if !ok {
			return
		}
		for _, chat := range n.Members {
			if chat != "" {
				chats[chat] = true
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(id)
	out := make([]string, 0, len(chats))
	for id := range chats {
		out = append(out, id)
	}
	return out
}

// unit is the centroid's direction. Cosine similarity is the dot of two
// unit vectors; the stored Sum is a total, and dividing by Count would not
// change the direction. A zero or empty sum is no position.
func unit(c placeorg.Centroid) []float64 {
	if len(c.Sum) == 0 || c.Count <= 0 {
		return nil
	}
	var ss float64
	for _, v := range c.Sum {
		ss += float64(v) * float64(v)
	}
	if ss == 0 || math.IsNaN(ss) {
		return nil
	}
	inv := 1 / math.Sqrt(ss)
	out := make([]float64, len(c.Sum))
	for i, v := range c.Sum {
		out[i] = float64(v) * inv
	}
	return out
}

// countable is a chat that counts toward the by-month cut. A probe is not
// a chat, and a duplicate would otherwise push a place over the cut by
// being a copy of one that was already counted.
func countable(corpus *placeorg.Corpus, id string) bool {
	if corpus == nil || corpus.Kinds == nil {
		return true
	}
	switch corpus.Kinds[id] {
	case placeorg.KindProbe, placeorg.KindDuplicate:
		return false
	default:
		return true
	}
}

// shown is a chat a view may list. Probes stay out. Duplicates stay in,
// because the person can open them and the view is a list, not a count.
func shown(corpus *placeorg.Corpus, id string) bool {
	if corpus == nil || corpus.Kinds == nil {
		return true
	}
	return corpus.Kinds[id] != placeorg.KindProbe
}
