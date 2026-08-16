package head

import (
	"errors"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The head's world may not be smaller than what is happening in it.
//
// Every read on this belt resolved through [store.ActiveNodes], which is the
// COMPACT view: a fold's outermost representative stands in for its members, so
// a job that has been filed away is one line instead of forty. That is the
// right shape for history and the wrong shape for anything alive, and the day
// the two met the belt produced its worst possible sentence. A continuation was
// running, seven minutes in, a cent spent, plainly drawn on the rail. Asked to
// cancel it, the head read the board three times, searched five times, and
// answered that there was no such work — a false negative that reads as a
// judgement, and left the person with no way to stop their own job but raw SQL.
//
// The law these two helpers carry is one sentence: FOLDING HIDES NOTHING THAT
// IS STILL ALIVE. Folding is filing, filing is for work that is over, and a
// node that is running, claimed or pending is not over however old its lineage
// is. So every belt read that turns on liveness — the board, the sets a verb
// resolves, the counts on the status page — reads the active view UNIONED with
// the open one, and a live node reaches the head as its own row whatever has
// been filed around it.

// errNoGraph is the refusal a read owes a surface with nothing behind it. The
// belt's tools already answer in these words; the helpers below reach the store
// before a tool's own guard has run, so they carry it too.
var errNoGraph = errors.New("this surface has no graph behind it, so nothing can be read")

// headNodes is the graph as the belt must see it: the active view widened by
// every node that is still open.
func (h *Head) headNodes() ([]store.Node, error) {
	if h == nil || h.store == nil {
		return nil, errNoGraph
	}
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return nil, err
	}
	return h.widenWithOpen(nodes), nil
}

// headSnapshot is headNodes with the active edges beside it, which is what any
// reader needs that draws what a row is waiting on.
func (h *Head) headSnapshot() (store.Snapshot, error) {
	snapshot, err := h.store.ActiveSnapshot()
	if err != nil {
		return store.Snapshot{}, err
	}
	snapshot.Nodes = h.widenWithOpen(snapshot.Nodes)
	return snapshot, nil
}

// widenWithOpen appends the open nodes a view was missing, in their own order,
// leaving the view's order alone.
//
// A failed widening returns the view unchanged. This is additive by
// construction, and an addition that cannot be made must never subtract: a
// belt that answered "nothing is running" because a second query failed would
// be reproducing the defect through a different door.
func (h *Head) widenWithOpen(view []store.Node) []store.Node {
	if h == nil || h.store == nil {
		return view
	}
	open, err := h.store.OpenNodes()
	if err != nil || len(open) == 0 {
		return view
	}
	present := make(map[string]bool, len(view))
	for _, node := range view {
		present[node.ID] = true
	}
	for _, node := range open {
		if present[node.ID] {
			continue
		}
		present[node.ID] = true
		view = append(view, node)
	}
	return view
}

// liveFolded is the state this file exists for: a node that folding has filed
// away and that has not finished. It is named so the two renderers that must
// treat it as live rather than as history say so in the same words.
func liveFolded(node store.Node) bool { return node.Folded && classOpen(node.Status) }
