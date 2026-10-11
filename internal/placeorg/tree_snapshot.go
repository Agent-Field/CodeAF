package placeorg

import "sort"

// TreeFromNodes builds a read-only Tree from a snapshot of nodes, the way the
// bridge will bind the store's existing places as Options.Existing. It is a
// constructor, not a mutator: roots are the nodes with no parent, a chat's
// home is the node listing it as a member, and the tree changes afterwards
// only through the Runner. Stage packages use it to give their tests a tree
// without a way to reach the unexported apply.
func TreeFromNodes(nodes []Node) *Tree {
	t := NewTree()
	for i := range nodes {
		n := nodes[i]
		t.nodes[n.ID] = &n
		for _, chat := range n.Members {
			t.home[chat] = n.ID
		}
		if n.Parent == "" {
			t.roots = append(t.roots, n.ID)
		}
	}
	sort.Strings(t.roots)
	return t
}
