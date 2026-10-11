// Package nest turns three partitions of the same chats into one forest.
// The finest partition is the leaves, and each community is given exactly
// one parent: the coarser community that already holds the most of its
// chats. Names are left empty. Collapsing and dissolving groups is a later
// stage; this package only nests.
package nest

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Overlap implements placeorg.Nester. The zero value is ready to use.
type Overlap struct{}

// New returns a nester. It keeps nothing between calls: the same labels
// always build the same tree.
func New() *Overlap { return &Overlap{} }

// compile-time check that this stage is the Nester the Runner asks for.
var _ placeorg.Nester = (*Overlap)(nil)

// Nest builds the forest. fine, mid and coarse are one label per chat, in
// corpus order, from the three resolutions. A negative label means the chat
// was not in that partition; it goes to Now. Labels are otherwise arbitrary
// integers, and the same integer at two resolutions is two different places.
func (o *Overlap) Nest(ctx context.Context, corpus *placeorg.Corpus, fine, mid, coarse []int) (*placeorg.Tree, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if corpus == nil {
		return nil, fmt.Errorf("placeorg/nest: no corpus")
	}
	n := len(corpus.Chats)
	if len(fine) != n || len(mid) != n || len(coarse) != n {
		return nil, fmt.Errorf("placeorg/nest: labels are %d, %d and %d long and the corpus has %d chats", len(fine), len(mid), len(coarse), n)
	}
	if err := uniqueChats(corpus.Chats); err != nil {
		return nil, err
	}
	// The mid parent is chosen from the fine communities, and the root
	// parent is chosen from the original mid partition. A chat is then
	// moved onto its fine community's parents, so a chat that sat in a
	// different mid community follows the majority of its fine group.
	fineParent := majorityParent(fine, mid)
	midParent := majorityParent(mid, coarse)
	nodes, homes, inbox := forest(corpus.Chats, fine, fineParent, midParent)
	return placeorg.Assemble(nodes, homes, inbox)
}

func uniqueChats(chats []placeorg.Chat) error {
	seen := make(map[string]bool, len(chats))
	for _, chat := range chats {
		if chat.ID == "" {
			return fmt.Errorf("placeorg/nest: a chat has no id")
		}
		if seen[chat.ID] {
			return fmt.Errorf("placeorg/nest: chat %s is in the corpus twice", chat.ID)
		}
		seen[chat.ID] = true
	}
	return nil
}

// forest places every chat at the leaf of its fine community and hangs that
// leaf under the mid and root the majority vote chose. A chat with no such
// path goes to Now. Ancestor nodes list no members: a member is a chat whose
// home is that node, and every home here is a leaf.
func forest(chats []placeorg.Chat, fine []int, fineParent, midParent map[int]int) ([]placeorg.BuiltNode, map[string]string, []string) {
	nodes := map[string]*placeorg.BuiltNode{}
	homes := map[string]string{}
	var inbox []string
	ensure := func(id, parent string, depth int) {
		if _, ok := nodes[id]; ok {
			return
		}
		nodes[id] = &placeorg.BuiltNode{ID: id, Parent: parent, Depth: depth}
	}
	for i, chat := range chats {
		label := fine[i]
		midLabel, midOK := fineParent[label]
		rootLabel, rootOK := midParent[midLabel]
		if label < 0 || !midOK || !rootOK {
			inbox = append(inbox, chat.ID)
			continue
		}
		rootID := levelID("c/", rootLabel)
		midID := levelID("m/", midLabel)
		leafID := levelID("f/", label)
		ensure(rootID, "", 1)
		ensure(midID, rootID, 2)
		ensure(leafID, midID, 3)
		nodes[leafID].Members = append(nodes[leafID].Members, chat.ID)
		homes[chat.ID] = leafID
	}
	out := make([]placeorg.BuiltNode, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, *node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, homes, inbox
}

// levelID keeps a community number from one resolution from colliding with
// the same number at another. The letters are c for the coarse parent, m
// for the middle and f for the fine leaf.
func levelID(prefix string, label int) string {
	return prefix + strconv.Itoa(label)
}
