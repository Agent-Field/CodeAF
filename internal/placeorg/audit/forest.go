package audit

import (
	"sort"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// place is one node of the shadow tree. live is false once an earlier op
// has folded it away. members are the chats listed on this node; the chats
// that belong to it are those plus its descendants, because a child is part
// of its parent.
type place struct {
	id       string
	parent   string
	children []string
	members  []string
	depth    int
	live     bool
}

// forest is the auditor's private copy of a Tree. Absorbing a node moves
// its chats onto the parent and marks the subtree not live, which is what
// the reference does by rewriting labels. The caller's Tree is never written.
type forest struct {
	places map[string]*place
	roots  []string
}

func loadForest(t *placeorg.Tree) *forest {
	f := &forest{places: map[string]*place{}}
	if t == nil {
		return f
	}
	var walk func(string)
	walk = func(id string) {
		n, ok := t.Node(id)
		if !ok || f.places[id] != nil {
			return
		}
		f.places[id] = &place{
			id:       id,
			parent:   n.Parent,
			children: append([]string(nil), n.Children...),
			members:  append([]string(nil), n.Members...),
			depth:    n.Depth,
			live:     true,
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	f.roots = t.Roots()
	for _, id := range f.roots {
		walk(id)
	}
	return f
}

// community lists the chats that belong to a place: its own and its
// descendants', each chat once, in the order they were stored.
func (f *forest) community(id string) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(string)
	walk = func(id string) {
		p := f.places[id]
		if p == nil || !p.live {
			return
		}
		for _, m := range p.members {
			if seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
		for _, c := range p.children {
			walk(c)
		}
	}
	walk(id)
	return out
}

func (f *forest) liveRoots() []string {
	var out []string
	for _, id := range f.roots {
		p := f.places[id]
		if p != nil && p.live {
			out = append(out, id)
		}
	}
	return out
}

func (f *forest) liveAt(depth int) []string {
	var out []string
	for id, p := range f.places {
		if p.live && p.depth == depth {
			out = append(out, id)
		}
	}
	return out
}

func (f *forest) maxDepth() int {
	d := 0
	for _, p := range f.places {
		if p.live && p.depth > d {
			d = p.depth
		}
	}
	return d
}

func (f *forest) liveChats() []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range f.liveRoots() {
		for _, m := range f.community(id) {
			if seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// promote makes each mid a root. The parent keeps its own chats and loses
// those children. Depths under a promoted place move up with it, so a leaf
// that was three deep is two deep once its parent stands on its own.
func (f *forest) promote(rootID string, mids []string) {
	root := f.places[rootID]
	if root == nil || !root.live {
		return
	}
	for _, id := range mids {
		child := f.places[id]
		if child == nil || !child.live {
			continue
		}
		root.children = removeID(root.children, id)
		child.parent = ""
		f.roots = append(f.roots, id)
		f.setDepth(id, 1)
	}
}

func (f *forest) setDepth(id string, depth int) {
	p := f.places[id]
	if p == nil {
		return
	}
	p.depth = depth
	for _, c := range p.children {
		f.setDepth(c, depth+1)
	}
}

// absorb folds id into its parent. The chats move up and the subtree is
// marked not live, so a later pass does not dissolve or demote it again.
// The reference's second dissolve of an already-folded child is that later
// pass seeing its own negated label; this copy does not emit it.
func (f *forest) absorb(id string) {
	p := f.places[id]
	if p == nil || !p.live {
		return
	}
	members := f.community(id)
	parent := p.parent
	f.kill(id)
	if parent == "" {
		return
	}
	par := f.places[parent]
	if par == nil {
		return
	}
	par.children = removeID(par.children, id)
	par.members = append(par.members, members...)
}

// dropRoot removes a top-level place. Its chats leave the forest; the
// caller records them as going to Now.
func (f *forest) dropRoot(id string) []string {
	members := f.community(id)
	f.kill(id)
	f.roots = removeID(f.roots, id)
	return members
}

func (f *forest) kill(id string) {
	p := f.places[id]
	if p == nil || !p.live {
		return
	}
	children := append([]string(nil), p.children...)
	for _, c := range children {
		f.kill(c)
	}
	p.live = false
	p.members = nil
	p.children = nil
}

func removeID(ids []string, drop string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

// sortedIDs orders ids so two runs propose the same ops. A trailing integer
// sorts numerically, which is how the reference walks community labels;
// anything else sorts as text.
func sortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Slice(out, func(i, j int) bool {
		ni, okI := trailingInt(out[i])
		nj, okJ := trailingInt(out[j])
		if okI && okJ && ni != nj {
			return ni < nj
		}
		return out[i] < out[j]
	})
	return out
}

func trailingInt(id string) (int, bool) {
	i := len(id)
	for i > 0 && id[i-1] >= '0' && id[i-1] <= '9' {
		i--
	}
	if i == len(id) {
		return 0, false
	}
	n, err := strconv.Atoi(id[i:])
	if err != nil {
		return 0, false
	}
	return n, true
}
