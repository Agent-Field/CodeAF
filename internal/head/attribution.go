package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Four jobs speaking into one thread is the ordinary case, and until now every
// model that had to resolve "it", "that one" or "the auth fix" was shown a
// conversation in which all four spoke in one undifferentiated voice. The TUI
// has always drawn the attribution — a faint chip over every job-anchored line —
// and the head, which is the party that actually has to decide what the user
// means, was handed the same lines with the anchor stripped off.
//
// The name is the one the user already reads on screen: the job's short human
// title, the same string the cards and the receipts use. It is never an id.
// Deixis only resolves when the person and the model are looking at the same
// names, so a second vocabulary — task-3171, node ids, anything from backstage —
// would be worse than no vocabulary at all.

// jobNames resolves the node a line was spoken by to the job that node belongs
// to, memoised for the length of one render. A job is a root and its parts, so
// several lines in one window routinely resolve to the same title and the walk
// is worth doing once.
type jobNames struct {
	store *store.Store
	known map[string]string
}

func (h *Head) jobNames() *jobNames {
	if h == nil {
		return &jobNames{}
	}
	return &jobNames{store: h.store, known: make(map[string]string, 4)}
}

// label is the job's short human title for the node that spoke, or the empty
// string when the line belongs to the conversation rather than to any job.
func (names *jobNames) label(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if names == nil || names.store == nil || nodeID == "" || nodeID == store.RootID {
		return ""
	}
	if known, ok := names.known[nodeID]; ok {
		return known
	}
	// A title is three to five words; a title long enough to need this bound is
	// a brief standing in for one, and a whole brief on every line would crowd
	// out the conversation it is annotating.
	label := truncateBytes(names.walk(nodeID), jobLabelBytes)
	names.known[nodeID] = label
	return label
}

// jobLabelBytes bounds one attributed name.
const jobLabelBytes = 80

// walk climbs from the node that spoke to the job it is part of, using the same
// definition of a job root the store's own surgery reads use: parented on the
// permanent spine, or on a territory that packed it away. A walk that runs out
// of graph settles for the deepest node it did reach — a name from one step
// inside the job is still the user's vocabulary, and silence is not.
func (names *jobNames) walk(nodeID string) string {
	label := ""
	for depth := 0; depth < adjacencyAncestorDepth; depth++ {
		node, found, err := names.store.Node(nodeID)
		if err != nil || !found || node.ID == store.RootID {
			return label
		}
		label = surgeryTargetLabel(node)
		parent := strings.TrimSpace(node.Parent)
		if parent == "" || parent == store.RootID {
			return label
		}
		owner, ok, ownerErr := names.store.Node(parent)
		if ownerErr != nil || !ok || owner.Group == store.TerritoryGroup {
			return label
		}
		nodeID = parent
	}
	return label
}
