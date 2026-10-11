package audit

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// dissolve removes a place with fewer than Quorum chats, deepest first, so a
// small child is gone before its parent is measured. Chats from a nested
// place stay with the parent. Chats from a top-level place go to Now.
// A place an earlier collapse already folded is not live, so it is not
// dissolved a second time.
func (Auditor) dissolve(f *forest) []placeorg.Op {
	var ops []placeorg.Op
	for d := f.maxDepth(); d >= 1; d-- {
		for _, id := range sortedIDs(f.liveAt(d)) {
			p := f.places[id]
			if p == nil || !p.live {
				continue
			}
			members := f.community(id)
			if len(members) >= placeorg.Quorum {
				continue
			}
			if p.parent == "" {
				f.dropRoot(id)
				ops = append(ops, proposed(placeorg.OpDissolve, "", id, "", members,
					fmt.Sprintf("This place has %d chats, which is not enough, so they stay in Now.", len(members))))
				continue
			}
			parent := p.parent
			f.absorb(id)
			ops = append(ops, proposed(placeorg.OpDissolve, parent, id, "", members,
				fmt.Sprintf("This place has %d chats, which is not enough, so they stay with the place above.", len(members))))
		}
	}
	return ops
}
