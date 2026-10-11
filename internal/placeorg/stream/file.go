package stream

import (
	"context"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// choice is where one branch would put the chat. parked is true when the
// stop is a top-level place or a section that still has children under it.
// A stop on a place with no children is a filing.
type choice struct {
	place  place
	inbox  bool
	parked bool
}

// File places one arrival. The folder rule wins outright. Otherwise Chow
// walks each top-level branch and keeps the closest legal stop, and a model
// is asked only when the two closest places anywhere in the tree sit in
// different branches and are within the margin.
func (s *Stream) File(ctx context.Context, t *placeorg.Tree, a placeorg.Arrival, judge placeorg.Judge) (placeorg.Filing, error) {
	if err := ctx.Err(); err != nil {
		return placeorg.Filing{}, err
	}
	if hit, ok := s.folderHit(t, a.FolderPlace); ok {
		why := "This chat's folder already belongs to " + shown(hit)
		return placeFiling(a, hit, false, false, nil, nil, why), nil
	}
	th := s.thresholds()
	roots, kids, all := scorePlaces(t, s.space(), a.Vector)
	picked, ok := chow(roots, kids, th.Floor, th.Margin)
	if !ok || picked.inbox {
		return inboxFiling(a), nil
	}
	if top, ambiguous := contest(all, th.Floor, th.Margin); ambiguous {
		return s.ask(ctx, t, a, top, judge)
	}
	return placeFiling(a, picked.place, picked.parked, false, nil, nil, ""), nil
}

// folderHit is the hard prior. An id the tree does not have is not a prior;
// the chat is placed by Chow instead of being filed at a place that is gone.
func (s *Stream) folderHit(t *placeorg.Tree, id string) (place, bool) {
	if s == nil || id == "" || t == nil {
		return place{}, false
	}
	n, ok := t.Node(id)
	if !ok {
		return place{}, false
	}
	return place{id: n.ID, name: n.Name, parent: n.Parent, depth: n.Depth, has: true}, true
}

// chow applies the one park rule on every top-level branch and keeps the
// closest stop. Descent uses only the best child at each step: a chat is not
// compared with a section it would have to pass a worse section to reach.
func chow(roots []place, kids map[string][]place, floor, margin float32) (choice, bool) {
	if len(roots) == 0 {
		return choice{inbox: true}, false
	}
	var best choice
	seen := false
	for _, root := range roots {
		c := chowBranch(root, kids, floor, margin)
		if !seen || betterChoice(c, best) {
			best = c
			seen = true
		}
	}
	return best, seen
}

// chowBranch scores one top-level branch as a single path: the best child
// at each step, never a section reached by passing a closer sibling. The
// chat is inbox when nothing on that path reaches the floor. Otherwise it
// stops at the deepest place that beats every place above it by the margin.
// Stopping above the finest place on the path is a park; stopping on the
// finest place is a filing. A top-level place is always a park.
func chowBranch(root place, kids map[string][]place, floor, margin float32) choice {
	chain := []place{root}
	cur := root
	for {
		child := bestChild(kids[cur.id])
		if !child.has {
			break
		}
		chain = append(chain, child)
		cur = child
	}
	best := chain[0].score
	for _, p := range chain[1:] {
		if p.score > best {
			best = p.score
		}
	}
	if best < floor {
		return choice{inbox: true}
	}
	// The deepest place that beats every ancestor wins, even when the place
	// in between does not. A section can be a poor fit while the place under
	// it is a clear one; the margin is against the ancestors, not against
	// "every step along the way also won".
	stop := 0
	for i := 1; i < len(chain); i++ {
		if beatsAll(chain[i].score, chain[:i], margin) {
			stop = i
		}
	}
	// The finest place on the path is a filing. Anywhere above it, including
	// the top, the chat is parked: a finer place did not win the margin.
	parked := stop == 0 || stop < len(chain)-1
	return choice{place: chain[stop], parked: parked}
}

// beatsAll reports whether score is a clear improvement on every ancestor.
// A hair better than the parent is not enough when a grandparent still fits.
func beatsAll(score float32, ancestors []place, margin float32) bool {
	for _, a := range ancestors {
		if score <= a.score+margin {
			return false
		}
	}
	return true
}

func bestChild(children []place) place {
	var best place
	for _, c := range children {
		best = prefer(best, c)
	}
	return best
}

// betterChoice prefers a real stop over Now, then the closer stop, then the
// broader one, then a stable id.
func betterChoice(a, b choice) bool {
	if a.inbox != b.inbox {
		return !a.inbox
	}
	if a.inbox {
		return false
	}
	if a.place.score != b.place.score {
		return a.place.score > b.place.score
	}
	if a.place.depth != b.place.depth {
		return a.place.depth < b.place.depth
	}
	return a.place.id < b.place.id
}

// contest is the cross-branch check. It looks at every place, not only the
// branch Chow stopped on: two close places under different tops are a
// question even when one branch looked decided on its own.
func contest(all []place, floor, margin float32) ([]place, bool) {
	if len(all) < 2 {
		return nil, false
	}
	ranked := append([]place(nil), all...)
	sortPlaces(ranked)
	top := ranked[0]
	second := ranked[1]
	if top.root == second.root || top.score-second.score >= margin || top.score < floor {
		return nil, false
	}
	n := contestPlaces
	if n > len(ranked) {
		n = len(ranked)
	}
	return ranked[:n], true
}

func sortPlaces(ps []place) {
	for i := 1; i < len(ps); i++ {
		j := i
		for j > 0 && placeBefore(ps[j], ps[j-1]) {
			ps[j], ps[j-1] = ps[j-1], ps[j]
			j--
		}
	}
}

// placeBefore reports whether a should rank above b: closer first.
func placeBefore(a, b place) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.id < b.id
}

// ask shows the closest places and no way to start a place. "none" leaves
// the chat in Now. A missing judge, or an answer that is not one of the
// shown keys, keeps the broader of the two places that contested.
func (s *Stream) ask(ctx context.Context, t *placeorg.Tree, a placeorg.Arrival, top []place, judge placeorg.Judge) (placeorg.Filing, error) {
	if err := ctx.Err(); err != nil {
		return placeorg.Filing{}, err
	}
	keep := broader(top[0], top[1])
	opts := make([]placeorg.Option, 0, len(top)+1)
	byKey := map[string]place{}
	for i, p := range top {
		key := optionKey(i)
		opts = append(opts, placeorg.Option{Key: key, Text: shown(p)})
		byKey[key] = p
	}
	opts = append(opts, placeorg.Option{Key: "none", Text: "none of these"})
	fallback := placeorg.Option{Key: keyOf(top, keep.id), Text: shown(keep)}
	ids := []string{top[0].id, top[1].id}
	if judge == nil {
		return placeFiling(a, keep, parkedAt(t, keep), false, ids, nil, ""), nil
	}
	pick := judge.Choose(ctx, placeorg.Question{
		Role:      roles.RolePlaceFile,
		Purpose:   "which existing place this chat belongs in",
		Context:   blurb(a.Chat),
		Options:   opts,
		MaxTokens: 60,
	}, fallback)
	if pick.Key == "none" && !pick.FellBack {
		f := inboxFiling(a)
		f.Judged = true
		f.Contest = ids
		f.Pick = &pick
		return f, nil
	}
	chosen := keep
	if p, ok := byKey[pick.Key]; ok && !pick.FellBack {
		chosen = p
	}
	return placeFiling(a, chosen, parkedAt(t, chosen), true, ids, &pick, ""), nil
}

// parkedAt reports whether a chat put at p is parked rather than filed.
// A top-level place is a park even when it has no sections. Anywhere lower,
// a place that still has children is a park and a place with none is a filing.
func parkedAt(t *placeorg.Tree, p place) bool {
	if p.depth <= 1 {
		return true
	}
	if t == nil {
		return false
	}
	n, ok := t.Node(p.id)
	return ok && len(n.Children) > 0
}

func keyOf(top []place, id string) string {
	for i, p := range top {
		if p.id == id {
			return optionKey(i)
		}
	}
	return optionKey(0)
}

func optionKey(i int) string {
	// Keys are labels the model points at. They are never place ids, and
	// there are at most five of them.
	return "p" + string(rune('1'+i))
}

func shown(p place) string {
	if p.name != "" {
		return p.name
	}
	return "place"
}

func blurb(c placeorg.Chat) string {
	if c.Title != "" {
		return clip(c.Title, 200)
	}
	return clip(c.Text, 200)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := range s {
		if i == n {
			return s[:k]
		}
		i++
	}
	return s
}

func placeFiling(a placeorg.Arrival, p place, parked, judged bool, contest []string, pick *placeorg.Pick, why string) placeorg.Filing {
	kind := placeorg.OpFile
	if parked {
		kind = placeorg.OpPark
	}
	if why == "" {
		why = "This chat fits " + shown(p)
	}
	return placeorg.Filing{
		Op: placeorg.Op{
			Kind:   kind,
			Stage:  stage,
			Place:  p.id,
			Chat:   a.Chat.ID,
			Level:  p.depth,
			Scores: []float32{p.score},
			Why:    why,
		},
		Judged:  judged,
		Contest: contest,
		Pick:    pick,
	}
}

func inboxFiling(a placeorg.Arrival) placeorg.Filing {
	return placeorg.Filing{Op: placeorg.Op{
		Kind:  placeorg.OpInbox,
		Stage: stage,
		Chat:  a.Chat.ID,
		Why:   "Nothing open fits this chat, so it stays in Now",
	}}
}
