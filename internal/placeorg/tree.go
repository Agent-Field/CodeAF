package placeorg

import (
	"fmt"
	"slices"
	"sort"
)

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
// and the Runner applies them here after journaling. It performs the op the
// way the audit stage's shadow already pictured it, so what a stage proposed
// and what the tree became agree. A refused op changes nothing.
func (t *Tree) apply(op Op) error {
	switch op.Kind {
	case OpFile, OpPark:
		return t.homeAt(op.Chat, op.Place)
	case OpInbox:
		return t.toInbox(op.Chat)
	case OpLink:
		return t.addLink(op.Chat, op.Place)
	case OpName, OpRename:
		return t.setName(op.Place, op.Name)
	case OpCollapse, OpDissolve, OpDemote:
		return t.remove(op.Place)
	case OpMerge:
		return t.mergeInto(op.Place, op.Into)
	case OpSplit, OpBirth:
		return t.create(op.Place, op.Parent, op.Members)
	case OpInterpose:
		return t.interpose(op.Place, op.Parent, op.Children)
	case OpLift:
		return t.lift(op.Place, op.Children)
	case OpLens:
		return t.addLens(op)
	case OpExemplar, OpConstraint, OpAxis:
		// These change what the organiser remembers, which is State, and no
		// place in the tree.
		return nil
	default:
		return fmt.Errorf("placeorg: the tree has no way to apply a %q op", op.Kind)
	}
}

func (t *Tree) node(id string) (*Node, error) {
	n, ok := t.nodes[id]
	if !ok {
		return nil, fmt.Errorf("placeorg: there is no place %q", id)
	}
	return n, nil
}

// homeAt moves a chat to a place, leaving wherever it was before.
func (t *Tree) homeAt(chat, place string) error {
	n, err := t.node(place)
	if err != nil {
		return err
	}
	if chat == "" {
		return fmt.Errorf("placeorg: an op files a chat with no id")
	}
	if t.home[chat] == place {
		return nil
	}
	t.unhome(chat)
	n.Members = append(n.Members, chat)
	t.home[chat] = place
	return nil
}

// unhome takes a chat out of its place and out of Now.
func (t *Tree) unhome(chat string) {
	if old, ok := t.home[chat]; ok {
		if n := t.nodes[old]; n != nil {
			n.Members = without(n.Members, chat)
		}
		delete(t.home, chat)
	}
	delete(t.inbox, chat)
}

func (t *Tree) toInbox(chat string) error {
	if chat == "" {
		return fmt.Errorf("placeorg: an op sends a chat with no id to Now")
	}
	t.unhome(chat)
	t.inbox[chat] = true
	return nil
}

func (t *Tree) addLink(chat, place string) error {
	if _, err := t.node(place); err != nil {
		return err
	}
	if slices.Contains(t.links[chat], place) {
		return nil
	}
	t.links[chat] = append(t.links[chat], place)
	return nil
}

func (t *Tree) setName(place, name string) error {
	n, err := t.node(place)
	if err != nil {
		return err
	}
	n.Name = name
	return nil
}

func (t *Tree) addLens(op Op) error {
	n, err := t.node(op.Place)
	if err != nil {
		return err
	}
	n.Lenses = append(n.Lenses, Lens{Name: op.Name, Axis: op.Into, Members: append([]string(nil), op.Members...)})
	return nil
}

// remove folds a place and everything under it into its parent: the chats of
// the whole subtree become the parent's, and the places are gone. At the top
// level there is no parent to keep them, so the chats go to Now. This is the
// audit stage's own fold, so collapse, dissolve and demote are this one move.
func (t *Tree) remove(id string) error {
	n, err := t.node(id)
	if err != nil {
		return err
	}
	chats := t.subtreeList(id)
	for _, chat := range chats {
		if n.Parent == "" {
			t.toInbox(chat)
		} else {
			t.homeAt(chat, n.Parent)
		}
	}
	t.detach(n)
	t.forget(id)
	return nil
}

// subtreeList is subtree in the order the chats are stored, so a fold files
// them the same way every time.
func (t *Tree) subtreeList(id string) []string {
	var out []string
	var walk func(string)
	walk = func(id string) {
		n := t.nodes[id]
		if n == nil {
			return
		}
		out = append(out, n.Members...)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(id)
	return out
}

// forget deletes a place and every place under it.
func (t *Tree) forget(id string) {
	n := t.nodes[id]
	if n == nil {
		return
	}
	for _, child := range n.Children {
		t.forget(child)
	}
	delete(t.nodes, id)
}

// mergeInto folds one place into another: chats and places move across.
func (t *Tree) mergeInto(id, into string) error {
	n, err := t.node(id)
	if err != nil {
		return err
	}
	dest, err := t.node(into)
	if err != nil {
		return err
	}
	if id == into || t.within(into, id) {
		return fmt.Errorf("placeorg: %q cannot be merged into itself or a place inside it", id)
	}
	for _, chat := range append([]string(nil), n.Members...) {
		t.homeAt(chat, into)
	}
	for _, child := range n.Children {
		t.nodes[child].Parent = into
	}
	t.detach(n)
	dest.Children = sortedUnion(dest.Children, n.Children)
	for _, child := range n.Children {
		t.setDepth(child, dest.Depth+1)
	}
	delete(t.nodes, id)
	return nil
}

// create adds a place under parent (or at the top level) and moves the named
// chats into it. Split and birth are this one move.
func (t *Tree) create(id, parent string, members []string) error {
	if id == "" {
		return fmt.Errorf("placeorg: a new place has no id")
	}
	if _, dup := t.nodes[id]; dup {
		return fmt.Errorf("placeorg: place %q is already there", id)
	}
	depth := 1
	if parent != "" {
		p, err := t.node(parent)
		if err != nil {
			return err
		}
		depth = p.Depth + 1
		p.Children = sortedUnion(p.Children, []string{id})
	} else {
		t.roots = sortedUnion(t.roots, []string{id})
	}
	t.nodes[id] = &Node{ID: id, Parent: parent, Depth: depth}
	for _, chat := range members {
		t.homeAt(chat, id)
	}
	return nil
}

// lift makes each child a root. The parent keeps the chats filed on it.
// Every child is checked first, so a refusal leaves the tree as it was.
func (t *Tree) lift(parent string, children []string) error {
	if _, err := t.node(parent); err != nil {
		return err
	}
	if len(children) < 2 {
		return fmt.Errorf("placeorg: a lift names fewer than two places")
	}
	for _, id := range children {
		c, err := t.node(id)
		if err != nil {
			return err
		}
		if c.Parent != parent {
			return fmt.Errorf("placeorg: %q is not inside %q", id, parent)
		}
	}
	for _, id := range children {
		c := t.nodes[id]
		t.detach(c)
		c.Parent = ""
		t.roots = sortedUnion(t.roots, []string{id})
		t.setDepth(id, 1)
	}
	return nil
}

// interpose puts a new place over some sibling places.
func (t *Tree) interpose(id, parent string, children []string) error {
	if err := t.create(id, parent, nil); err != nil {
		return err
	}
	fresh := t.nodes[id]
	for _, child := range children {
		c, err := t.node(child)
		if err != nil {
			return err
		}
		t.detach(c)
		c.Parent = id
		fresh.Children = sortedUnion(fresh.Children, []string{child})
		t.setDepth(child, fresh.Depth+1)
	}
	return nil
}

// detach removes a node from its parent's child list or from the roots.
func (t *Tree) detach(n *Node) {
	if n.Parent == "" {
		t.roots = without(t.roots, n.ID)
		return
	}
	if p := t.nodes[n.Parent]; p != nil {
		p.Children = without(p.Children, n.ID)
	}
}

// setDepth gives a place a depth and walks its subtree.
func (t *Tree) setDepth(id string, depth int) {
	n := t.nodes[id]
	n.Depth = depth
	for _, child := range n.Children {
		t.setDepth(child, depth+1)
	}
}

// within reports whether id sits at or under ancestor.
func (t *Tree) within(id, ancestor string) bool {
	for steps := 0; id != "" && steps <= len(t.nodes); steps++ {
		if id == ancestor {
			return true
		}
		n := t.nodes[id]
		if n == nil {
			return false
		}
		id = n.Parent
	}
	return false
}

// without returns list minus one value, keeping order. It builds a new
// slice, so a Node copy handed out earlier never changes under a reader.
func without(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != drop {
			out = append(out, v)
		}
	}
	return out
}

func sortedUnion(a, b []string) []string {
	out := append(append([]string(nil), a...), b...)
	sort.Strings(out)
	return slices.Compact(out)
}

// clone is a deep copy. A run works on a copy of the snapshot it was given,
// so a cancelled or declined run leaves the caller's tree exactly as it was.
// A non-nil rename is applied to every place id on the way, which is how the
// Runner turns the nest stage's slash-spelled ids into ones the id law
// accepts.
func (t *Tree) clone(rename func(string) string) *Tree {
	if t == nil {
		return NewTree()
	}
	if rename == nil {
		rename = func(id string) string { return id }
	}
	out := NewTree()
	for _, n := range t.nodes {
		c := *n
		c.ID, c.Parent = rename(n.ID), renameNonEmpty(n.Parent, rename)
		c.Children = mapIDs(n.Children, rename)
		c.Members = append([]string(nil), n.Members...)
		c.Lenses = append([]Lens(nil), n.Lenses...)
		if n.Axis != nil {
			axis := *n.Axis
			c.Axis = &axis
		}
		c.Centroids = nil
		if len(n.Centroids) > 0 {
			c.Centroids = make(map[SpaceID]Centroid, len(n.Centroids))
			for space, cen := range n.Centroids {
				cen.Sum = append([]float32(nil), cen.Sum...)
				c.Centroids[space] = cen
			}
		}
		out.nodes[c.ID] = &c
	}
	out.roots = mapIDs(t.roots, rename)
	sort.Strings(out.roots)
	for chat, id := range t.home {
		out.home[chat] = rename(id)
	}
	for chat, ids := range t.links {
		out.links[chat] = mapIDs(ids, rename)
	}
	for chat := range t.inbox {
		out.inbox[chat] = true
	}
	return out
}

func renameNonEmpty(id string, rename func(string) string) string {
	if id == "" {
		return ""
	}
	return rename(id)
}

func mapIDs(ids []string, rename func(string) string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = rename(id)
	}
	sort.Strings(out)
	return out
}

// subtree lists every chat homed at a place or at a place under it.
func (t *Tree) subtree(id string) map[string]bool {
	out := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		n := t.nodes[id]
		if n == nil {
			return
		}
		for _, m := range n.Members {
			out[m] = true
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(id)
	return out
}

// adopt gives a place of this run the identity of one the person already has:
// its name, its store id and its Person mark. The Runner calls it in refresh
// mode, once it knows which new group is the old place, so that the name pass
// proposes instead of overwriting.
func (t *Tree) adopt(id string, from Node) {
	n := t.nodes[id]
	if n == nil {
		return
	}
	n.Name, n.StoreID, n.Person = from.Name, from.StoreID, from.Person
}
