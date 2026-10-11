package audit

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// demote turns a place that is a kind of activity into a view on its parent.
// A discriminative anchor decides without a model: an anchor the parent does
// not already share, carried by enough of the child's chats. Otherwise the
// Judge picks THING or ACTIVITY. KEEP is the fallback, and it is also the
// answer when the Judge is absent or answers outside those two options, so
// an unanswered place stays a place.
func (a Auditor) demote(ctx context.Context, f *forest, ix *corpusIndex, judge placeorg.Judge) ([]placeorg.Op, error) {
	var ops []placeorg.Op
	for d := 2; d <= f.maxDepth(); d++ {
		for _, id := range sortedIDs(f.liveAt(d)) {
			p := f.places[id]
			if p == nil || !p.live || p.parent == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			members := f.community(id)
			parent := f.community(p.parent)
			ok, err := a.destination(ctx, members, parent, ix, judge)
			if err != nil {
				return nil, err
			}
			if ok {
				continue
			}
			parentID := p.parent
			f.absorb(id)
			ops = append(ops, proposed(placeorg.OpDemote, parentID, id, "", members,
				fmt.Sprintf("These %d chats are one kind of activity, so they become a view on the place above.", len(members))))
		}
	}
	return ops, nil
}

// destination reports whether the chats are a place someone would go to.
// false means they should become a view.
func (a Auditor) destination(ctx context.Context, members, parent []string, ix *corpusIndex, judge placeorg.Judge) (bool, error) {
	if len(members) < placeorg.Quorum {
		return false, nil
	}
	if bestAnchor(members, parent, ix.anchor) {
		return true, nil
	}
	if judge == nil {
		// No Judge at all is the same as the fallback: the place stays.
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	q := placeorg.Question{
		Role:      placeorg.RolePlaceFacet,
		Purpose:   "Is this place about one thing, or one kind of activity?",
		Context:   facetContext(members, ix.title),
		Options:   facetOptions(),
		MaxTokens: 10,
	}
	// KEEP is not one of the options. It is the answer when the model does
	// not pick, and an unjudged place is never turned into a view.
	pick := judge.Choose(ctx, q, placeorg.Option{Key: keepKey, Text: "keep the place"})
	if pick.FellBack || (pick.Key != "THING" && pick.Key != "ACTIVITY") {
		return true, nil
	}
	return pick.Key == "THING", nil
}

const keepKey = "KEEP"

func facetOptions() []placeorg.Option {
	return []placeorg.Option{
		{Key: "THING", Text: "one thing"},
		{Key: "ACTIVITY", Text: "one kind of activity"},
	}
}

// bestAnchor is true when some anchor discriminates the child from the parent
// and covers at least the quorum or a quarter of the child, whichever is more.
func bestAnchor(members, parent []string, anchors map[string]placeorg.AnchorSet) bool {
	counts := map[string]int{}
	for _, id := range members {
		for key := range anchors[id] {
			if key == "" {
				continue
			}
			counts[key]++
		}
	}
	parentN := len(parent)
	if parentN == 0 {
		parentN = 1
	}
	best := 0
	for key, n := range counts {
		p := 0
		for _, id := range parent {
			if _, ok := anchors[id][key]; ok {
				p++
			}
		}
		if float64(p)/float64(parentN) < anchorParentMax && n > best {
			best = n
		}
	}
	need := math.Max(float64(placeorg.Quorum), anchorMemberShare*float64(len(members)))
	return float64(best) >= need
}

// facetContext shows ten titles, then the chat ids the question is about, so
// a journal line names the group and a replay can recognise it. The ids are
// the group's own ids, not text a model wrote.
func facetContext(members []string, title map[string]string) string {
	var b strings.Builder
	n := len(members)
	if n > facetTitles {
		n = facetTitles
	}
	for i := 0; i < n; i++ {
		line := strings.TrimSpace(title[members[i]])
		if line == "" {
			line = members[i]
		}
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString("chats:")
	for _, id := range members {
		b.WriteByte(' ')
		b.WriteString(id)
	}
	return b.String()
}
