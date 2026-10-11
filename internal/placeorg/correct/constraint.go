package correct

import (
	"fmt"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// constraint proposes a rule when the move just made is the
// ConstraintRepeats-th chat moved into one place that shares one anchor.
// A constraint is "chats with this anchor go to this place", so a pattern of
// moves with no anchor in common has nothing to say and proposes nothing.
func (c *Corrector) constraint(t *placeorg.Tree, corpus *placeorg.Corpus, m placeorg.Correction) (placeorg.Op, bool) {
	chats := c.movedInto(m.To)
	if len(chats) < placeorg.ConstraintRepeats {
		return placeorg.Op{}, false
	}
	anchor := sharedAnchor(corpus, chats, m.Chat)
	key := m.To + "\x00" + anchor
	if anchor == "" || c.offered[key] {
		return placeorg.Op{}, false
	}
	c.offered[key] = true
	to, _ := t.Node(m.To)
	return placeorg.Op{
		Kind: placeorg.OpConstraint, Stage: stage, Place: m.To, Name: anchor, Proposed: true,
		Why: fmt.Sprintf("you moved %d chats about %s to %s", len(chats), anchor, to.Name),
	}, true
}

// movedInto lists the distinct chats the person has moved into a place.
func (c *Corrector) movedInto(place string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range c.moves {
		if m.To == place && !seen[m.Chat] {
			seen[m.Chat] = true
			out = append(out, m.Chat)
		}
	}
	return out
}

// sharedAnchor returns the anchor every chat carries, the heaviest in the
// latest move's chat first, or "" when they share none.
func sharedAnchor(corpus *placeorg.Corpus, chats []string, latest string) string {
	var shared []string
	for a := range corpus.Anchors[latest] {
		if all(corpus, chats, a) {
			shared = append(shared, a)
		}
	}
	sort.Slice(shared, func(i, j int) bool {
		wi, wj := corpus.Anchors[latest][shared[i]], corpus.Anchors[latest][shared[j]]
		if wi != wj {
			return wi > wj
		}
		return shared[i] < shared[j]
	})
	if len(shared) == 0 {
		return ""
	}
	return shared[0]
}

func all(corpus *placeorg.Corpus, chats []string, anchor string) bool {
	for _, id := range chats {
		if _, ok := corpus.Anchors[id][anchor]; !ok {
			return false
		}
	}
	return true
}
