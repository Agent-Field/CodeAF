package placeorg

// The working tree an organising run builds. It is a forest of nodes with a
// home for every chat, links for the multi-home minority, and lenses over
// flat lists. It is NOT the store: a Plan binds it to placegraph in one
// commit. Its mutators are unexported so that only the Runner, through the
// journaling door in pipeline.go, can change it.

// Node is one place in the working tree.
type Node struct {
	ID       string
	Parent   string
	Children []string
	// Members are the chats whose home this is, parked ones included.
	Members []string
	// Depth counts from one at the top level.
	Depth int
	Name  string
	// StoreID is set when the node is an existing placegraph place (add-on
	// mode keeps them); empty for a node this run would create.
	StoreID string
	// Person marks a place the person made or confirmed. The pipeline never
	// dissolves, demotes or renames it without a proposal.
	Person bool
	// Centroids holds the node's position per space this run has vectors in.
	Centroids map[SpaceID]Centroid
	// Lenses are views over this node's flat list.
	Lenses []Lens
	// Axis is the steering axis the person set, if any.
	Axis *Axis
}

// Lens is a named view over a place's flat list: a demoted activity group, a
// by-month bucket set, or a steering axis value. It is never a tree node.
type Lens struct {
	Name string `json:"name"`
	// Axis says how members are chosen: "group" (a demoted cluster, members
	// listed), "month" (computed from Created), "anchor" (an anchor value),
	// "kind" (a kind-of-work label).
	Axis    string   `json:"axis"`
	Value   string   `json:"value,omitempty"`
	Members []string `json:"members,omitempty"`
}

// Tree is read by every stage and written by none.
type Tree struct {
	nodes map[string]*Node
	roots []string
	// home maps a chat to the node that is its home; links maps a chat to
	// its other places; inbox lists chats in no place.
	home  map[string]string
	links map[string][]string
	inbox map[string]bool
}

// NewTree returns an empty working tree.
func NewTree() *Tree {
	return &Tree{nodes: map[string]*Node{}, home: map[string]string{}, links: map[string][]string{}, inbox: map[string]bool{}}
}

// Node returns a copy of one node, or false.
func (t *Tree) Node(id string) (Node, bool) {
	n, ok := t.nodes[id]
	if !ok {
		return Node{}, false
	}
	return *n, true
}

// Roots lists the top-level nodes in a fixed order.
func (t *Tree) Roots() []string { return append([]string(nil), t.roots...) }

// Home returns the node a chat lives at, or false when it is in Now.
func (t *Tree) Home(chatID string) (string, bool) {
	id, ok := t.home[chatID]
	return id, ok
}

// Links lists a chat's shortcuts.
func (t *Tree) Links(chatID string) []string { return append([]string(nil), t.links[chatID]...) }

// Inbox lists the chats in Now.
func (t *Tree) Inbox() []string {
	out := make([]string, 0, len(t.inbox))
	for id := range t.inbox {
		out = append(out, id)
	}
	return out
}

// Depth returns the deepest node.
func (t *Tree) Depth() int {
	d := 0
	for _, n := range t.nodes {
		d = max(d, n.Depth)
	}
	return d
}

// apply is the one mutator. It is unexported on purpose: a stage returns Ops
// and the Runner applies them here after journaling. Not built yet.
func (t *Tree) apply(op Op) error { return ErrNotBuilt }
