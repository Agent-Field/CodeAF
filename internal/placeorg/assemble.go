package placeorg

import (
	"fmt"
	"sort"
)

// BuiltNode is one place in a tree a stage has just computed. Nest is the
// only stage that creates a tree; every later change is an Op the Runner
// applies. Depth counts from one at the top level, the same way Node.Depth
// does. Members are the chats whose home is this node, not the chats that
// merely sit underneath it.
type BuiltNode struct {
	ID        string
	Parent    string
	Members   []string
	Depth     int
	Name      string
	Person    bool
	StoreID   string
	Centroids map[SpaceID]Centroid
}

// Assemble builds a fresh tree from nodes a stage computed. It exists so the
// nest stage can hand the Runner a tree without calling apply — only
// (*Runner).apply may change a tree that already exists, and that function
// lives in tree.go, which the pipeline lane owns. A node with an empty
// Parent is a root. homes maps a chat to the node that is its home; inbox
// lists chats in Now. Every member's home is that node, so the two cannot
// drift.
func Assemble(nodes []BuiltNode, homes map[string]string, inbox []string) (*Tree, error) {
	t := NewTree()
	for _, n := range nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("placeorg: a place has no id")
		}
		if _, ok := t.nodes[n.ID]; ok {
			return nil, fmt.Errorf("placeorg: place %s is listed twice", n.ID)
		}
		stored := &Node{
			ID: n.ID, Parent: n.Parent, Depth: n.Depth, Name: n.Name,
			Person: n.Person, StoreID: n.StoreID,
			Members: append([]string(nil), n.Members...),
		}
		if len(n.Centroids) > 0 {
			stored.Centroids = make(map[SpaceID]Centroid, len(n.Centroids))
			for space, c := range n.Centroids {
				c.Sum = append([]float32(nil), c.Sum...)
				stored.Centroids[space] = c
			}
		}
		t.nodes[n.ID] = stored
	}
	if err := linkForest(t); err != nil {
		return nil, err
	}
	if err := fillHomes(t, homes, inbox); err != nil {
		return nil, err
	}
	return t, nil
}

// linkForest checks that every parent exists, that the parent links are a
// forest, and that depth counts from one. Children and roots are then stored
// in id order so two runs with the same nodes walk the same tree.
func linkForest(t *Tree) error {
	for id, n := range t.nodes {
		if n.Parent == "" {
			continue
		}
		if _, ok := t.nodes[n.Parent]; !ok {
			return fmt.Errorf("placeorg: place %s names parent %s, which is not in the tree", id, n.Parent)
		}
		if err := climbsToRoot(t, id); err != nil {
			return err
		}
	}
	kids := map[string][]string{}
	for id, n := range t.nodes {
		if n.Parent == "" {
			if n.Depth != 1 {
				return fmt.Errorf("placeorg: root %s has depth %d, and a root's depth is 1", id, n.Depth)
			}
			t.roots = append(t.roots, id)
			continue
		}
		parent := t.nodes[n.Parent]
		if n.Depth != parent.Depth+1 {
			return fmt.Errorf("placeorg: place %s has depth %d, and its parent %s has depth %d", id, n.Depth, n.Parent, parent.Depth)
		}
		kids[n.Parent] = append(kids[n.Parent], id)
	}
	sort.Strings(t.roots)
	for id, list := range kids {
		sort.Strings(list)
		t.nodes[id].Children = list
	}
	return nil
}

// climbsToRoot walks parents and refuses a loop. The depth rule already
// makes a loop hard to write; this names the loop when one is handed in.
func climbsToRoot(t *Tree, id string) error {
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return fmt.Errorf("placeorg: places form a cycle at %s", id)
		}
		seen[id] = true
		n, ok := t.nodes[id]
		if !ok {
			return fmt.Errorf("placeorg: place %s is not in the tree", id)
		}
		id = n.Parent
	}
	return nil
}

// fillHomes records homes and Now. A chat is in one of those, never both,
// and a member list is exactly the chats homed at that node.
func fillHomes(t *Tree, homes map[string]string, inbox []string) error {
	for chat, id := range homes {
		if chat == "" {
			return fmt.Errorf("placeorg: a home is recorded for a chat with no id")
		}
		if _, ok := t.nodes[id]; !ok {
			return fmt.Errorf("placeorg: chat %s is homed at %s, which is not in the tree", chat, id)
		}
		t.home[chat] = id
	}
	for _, chat := range inbox {
		if chat == "" {
			return fmt.Errorf("placeorg: Now lists a chat with no id")
		}
		if _, ok := t.inbox[chat]; ok {
			return fmt.Errorf("placeorg: chat %s is listed in Now twice", chat)
		}
		if _, ok := t.home[chat]; ok {
			return fmt.Errorf("placeorg: chat %s is both homed and in Now", chat)
		}
		t.inbox[chat] = true
	}
	for id, n := range t.nodes {
		for _, chat := range n.Members {
			if t.home[chat] != id {
				return fmt.Errorf("placeorg: place %s lists chat %s, whose home is %q", id, chat, t.home[chat])
			}
		}
	}
	for chat, id := range t.home {
		n := t.nodes[id]
		found := false
		for _, member := range n.Members {
			if member == chat {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("placeorg: chat %s is homed at %s, which does not list it", chat, id)
		}
	}
	return nil
}

// SetCentroid records a place's position in one space: the running sum of
// the vectors and how many chats that sum counts. The park stage writes it
// after it has decided where each chat lives. There is no Op for a position,
// because a position is derived from the chats and the next stage only needs
// to read it. The sum is copied, so the caller may reuse its buffer.
func SetCentroid(t *Tree, id string, space SpaceID, c Centroid) error {
	n, ok := t.nodes[id]
	if !ok {
		return fmt.Errorf("placeorg: no place %s to give a position", id)
	}
	if n.Centroids == nil {
		n.Centroids = map[SpaceID]Centroid{}
	}
	c.Sum = append([]float32(nil), c.Sum...)
	n.Centroids[space] = c
	return nil
}

// ClearCentroid drops one space's position for a place. Park clears the
// space before rewriting it, so a place that lost every chat does not keep
// the position from a previous pass.
func ClearCentroid(t *Tree, id string, space SpaceID) {
	n := t.nodes[id]
	if n == nil || n.Centroids == nil {
		return
	}
	delete(n.Centroids, space)
}
