package desktopbridge

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func getPlaceTree(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
	if !needGet(w, r) {
		return
	}
	org := p.organizer()
	if org == nil {
		fail(w, 404, "unknown engine action")
		return
	}
	tree, err := p.advice.existingTree()
	if err != nil {
		failPlaces(w, 500, "store", "Places could not be read.")
		return
	}
	org.mu.Lock()
	lenses := map[string][]string{}
	axes := map[string]string{}
	if org.State != nil {
		for id, list := range org.State.Lenses {
			names := make([]string, 0, len(list))
			for _, lens := range list {
				if lens.Name != "" {
					names = append(names, lens.Name)
				}
			}
			lenses[id] = names
		}
		for id, axis := range org.State.Axes {
			axes[id] = string(axis.Kind)
		}
	}
	org.mu.Unlock()
	var nodes []treeNodeWire
	for _, id := range tree.Roots() {
		nodes = append(nodes, treeNode(tree, id, lenses, axes))
	}
	write(w, map[string]any{"places": nodes})
}

func treeNode(tree *placeorg.Tree, id string, lenses map[string][]string, axes map[string]string) treeNodeWire {
	n, _ := tree.Node(id)
	names := lenses[id]
	if names == nil {
		names = []string{}
	}
	node := treeNodeWire{
		ID: n.ID, Name: n.Name, Depth: n.Depth,
		Chats: len(n.Members), Lenses: names, Axis: axes[id],
	}
	if len(n.Children) > 0 {
		node.Parked = len(n.Members)
		for _, child := range n.Children {
			node.Children = append(node.Children, treeNode(tree, child, lenses, axes))
		}
	}
	return node
}

// treeNodeWire is one node of the forest. chats is how many conversations
// have their home here. parked is that same count when the place still has
// children, and zero on a leaf: those chats stopped above a smaller place.
// children is how the forest nests; the design lists the fields of a node
// and is silent on the nesting, which a forest has to have.
type treeNodeWire struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Depth    int            `json:"depth"`
	Chats    int            `json:"chats"`
	Parked   int            `json:"parked"`
	Lenses   []string       `json:"lenses"`
	Axis     string         `json:"axis,omitempty"`
	Children []treeNodeWire `json:"children,omitempty"`
}
