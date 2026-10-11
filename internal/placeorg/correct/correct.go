// Package correct is the correction channel of the Places organiser. A chat
// the person moved by hand becomes an exemplar of its new place, the journal
// learns how many of its neighbours would now follow, and a pattern the
// person repeats three times becomes a constraint offered for a yes. Nothing
// moves but the chat the person moved.
package correct

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Neighbours is how many nearest chats are asked whether they would follow.
const Neighbours = 8

// FollowMargin is how much closer a neighbour must be to the new place than
// to its old one before it counts as following. It is the reference's figure.
const FollowMargin = 0.02

// Source gives the correction its vectors, in one space. Vectors are unit
// length; a chat without one is simply not a neighbour.
type Source interface {
	Vector(chatID string) ([]float32, bool)
}

// Corrector implements placeorg.Corrector. It remembers the moves it has
// seen so a repeat is recognised across calls.
type Corrector struct {
	space placeorg.SpaceID
	src   Source
	moves []placeorg.Correction
	// offered holds the place and anchor pairs already proposed, so a fourth
	// move of the same kind does not ask again.
	offered map[string]bool
}

var _ placeorg.Corrector = (*Corrector)(nil)

// New returns a Corrector reading centroids of the given space and chat
// vectors from src. prior is the exemplars already on file; they count
// toward a repeat so a restart does not forget the first two moves.
func New(space placeorg.SpaceID, src Source, prior []placeorg.Exemplar) *Corrector {
	c := &Corrector{space: space, src: src, offered: map[string]bool{}}
	for _, e := range prior {
		c.moves = append(c.moves, placeorg.Correction{Chat: e.Chat, From: e.From, To: e.To, At: e.At})
	}
	return c
}

// Correct absorbs one move: an OpExemplar for the moved chat, then an
// OpConstraint when the move completes a pattern.
func (c *Corrector) Correct(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, m placeorg.Correction) ([]placeorg.Op, error) {
	if _, ok := t.Node(m.To); !ok {
		return nil, fmt.Errorf("correct: %q is not a place in the tree", m.To)
	}
	c.moves = append(c.moves, m)
	follow, total := c.followers(t, m)
	ops := []placeorg.Op{{
		Kind: placeorg.OpExemplar, Stage: stage, Place: m.To, Chat: m.Chat,
		Why: exemplarWhy(follow, total),
	}}
	if op, ok := c.constraint(t, corpus, m); ok {
		ops = append(ops, op)
	}
	return ops, nil
}

const stage = "correct"

func exemplarWhy(follow, total int) string {
	if total == 0 {
		return "you moved this chat; it now counts double in its new place"
	}
	return fmt.Sprintf("you moved this chat; of %d similar chats in the old place, %d would follow it", total, follow)
}

// Followers is the generalisation count the journal records: of the chat's
// nearest neighbours that live in the place it left, how many sit closer to
// the new place, once the chat weighs ExemplarWeight there, than to the old
// one by FollowMargin. The moved chat is the nearest of its own eight, as it
// is in the reference run (its similarity to itself is the largest), and it
// is counted at the old place it left; that is what makes the figure equal
// the reference's "of 1 same-place neighbours, 0 would follow".
func (c *Corrector) Followers(t *placeorg.Tree, m placeorg.Correction) (follow, total int) {
	return c.followers(t, m)
}

func (c *Corrector) followers(t *placeorg.Tree, m placeorg.Correction) (follow, total int) {
	x, ok := c.src.Vector(m.Chat)
	from, okFrom := t.Node(m.From)
	to, okTo := t.Node(m.To)
	if !ok || !okFrom || !okTo {
		return 0, 0
	}
	fromC, toC := from.Centroids[c.space], with(to.Centroids[c.space], x, placeorg.ExemplarWeight)
	if len(fromC.Sum) == 0 || len(toC.Sum) == 0 {
		return 0, 0
	}
	for _, id := range c.nearest(t, x) {
		if home, _ := t.Home(id); home != m.From && id != m.Chat {
			continue
		}
		v, _ := c.src.Vector(id)
		total++
		if cosine(v, toC.Sum) > cosine(v, fromC.Sum)+FollowMargin {
			follow++
		}
	}
	return follow, total
}

// with returns the centroid after a chat joins it at the given weight. The
// weight is a count, not a bigger vector: a centroid is a sum and a count, so
// an exemplar is worth two members and never swamps a large place.
func with(cen placeorg.Centroid, x []float32, weight float64) placeorg.Centroid {
	if len(cen.Sum) != len(x) {
		return placeorg.Centroid{}
	}
	sum := make([]float32, len(x))
	for i := range x {
		sum[i] = cen.Sum[i] + float32(weight)*x[i]
	}
	return placeorg.Centroid{Sum: sum, Count: cen.Count + weight}
}

// nearest returns the Neighbours chats most similar to x, the moved chat
// included, by id order on a tie.
func (c *Corrector) nearest(t *placeorg.Tree, x []float32) []string {
	type scored struct {
		id string
		s  float64
	}
	var all []scored
	for _, id := range ids(t) {
		if v, ok := c.src.Vector(id); ok {
			all = append(all, scored{id, cosine(x, v)})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].s > all[j].s })
	out := make([]string, 0, Neighbours)
	for i := 0; i < len(all) && i < Neighbours; i++ {
		out = append(out, all[i].id)
	}
	return out
}

// ids lists every chat with a home, in a fixed order.
func ids(t *placeorg.Tree) []string {
	var out []string
	var walk func(id string)
	walk = func(id string) {
		n, _ := t.Node(id)
		out = append(out, n.Members...)
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	for _, r := range t.Roots() {
		walk(r)
	}
	sort.Strings(out)
	return out
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
