// Package park gives every chat its home in a tree that is already nested
// and trimmed. A chat descends into a smaller place only
// when that place is clearly closer than every place above it. Otherwise it
// stays with the broader place. A chat that is close to nothing stays in
// Now. The positions of the places are then retallied, with parked chats
// counted in, so the next chat is filed against the library as it actually
// sits.
package park

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Chow implements placeorg.Parker. The zero value calibrates its own
// thresholds. UseThresholds pins them, which tests of the placement rule
// do when a handful of vectors should not be asked to invent a distribution.
type Chow struct {
	fixed   *placeorg.Thresholds
	bySpace map[placeorg.SpaceID]placeorg.Thresholds
}

// New returns a parker with no thresholds yet. Park fills them per space.
func New() *Chow { return &Chow{} }

// compile-time check that this stage is the Parker the Runner asks for.
var _ placeorg.Parker = (*Chow)(nil)

// UseThresholds pins the floor, margin and delta for later Park calls on
// this value. The organiser does not call it: a real run calibrates from
// the space it is standing in. Tests call it so the placement rule can be
// read off a few vectors whose own quantiles would be noise.
func (c *Chow) UseThresholds(th placeorg.Thresholds) {
	if c == nil {
		return
	}
	copied := th
	c.fixed = &copied
}

// Thresholds returns the figures Park last computed, one entry per space.
// The runner copies the entry for the run's space into State.Thresholds,
// which is what a later filing reads. The map is a copy.
func (c *Chow) Thresholds() map[placeorg.SpaceID]placeorg.Thresholds {
	if c == nil || len(c.bySpace) == 0 {
		return nil
	}
	out := make(map[placeorg.SpaceID]placeorg.Thresholds, len(c.bySpace))
	for space, th := range c.bySpace {
		out[space] = th
	}
	return out
}

// Park places every chat that has a home and returns one op per chat it
// considered, including chats already in Now. Centroids on the tree are
// rewritten for this space: each place's sum counts the chats in its
// community that stayed out of Now, parked ones included.
func (c *Chow) Park(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, vectors [][]float32, space placeorg.SpaceID) ([]placeorg.Op, error) {
	if c == nil {
		return nil, fmt.Errorf("placeorg/park: no parker")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil || corpus == nil {
		return nil, fmt.Errorf("placeorg/park: no tree or no corpus")
	}
	if len(vectors) != len(corpus.Chats) {
		return nil, fmt.Errorf("placeorg/park: %d vectors for %d chats", len(vectors), len(corpus.Chats))
	}
	if err := oneWidth(vectors); err != nil {
		return nil, err
	}
	th, err := c.thresholdsFor(ctx, vectors, space)
	if err != nil {
		return nil, err
	}
	directions := communityDirections(t, corpus.Chats, vectors)
	ops := make([]placeorg.Op, 0, len(corpus.Chats))
	kept := make([]bool, len(corpus.Chats))
	for i, chat := range corpus.Chats {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		op, stays := c.placeOne(t, chat, vectors[i], directions, th)
		ops = append(ops, op)
		kept[i] = stays
	}
	if err := recount(t, corpus.Chats, vectors, kept, space); err != nil {
		return nil, err
	}
	return ops, nil
}

func (c *Chow) thresholdsFor(ctx context.Context, vectors [][]float32, space placeorg.SpaceID) (placeorg.Thresholds, error) {
	th := c.fixed
	if th == nil {
		calibrated, err := calibrate(ctx, vectors)
		if err != nil {
			return placeorg.Thresholds{}, err
		}
		th = &calibrated
	}
	if c.bySpace == nil {
		c.bySpace = map[placeorg.SpaceID]placeorg.Thresholds{}
	}
	c.bySpace[space] = *th
	return *th, nil
}

// placeOne decides one chat. A chat already in Now, or one with no vector,
// stays in Now: there is nothing to compare, and a missing position is not
// a home. stays is false when the chat is left out of every centroid.
func (c *Chow) placeOne(t *placeorg.Tree, chat placeorg.Chat, vector []float32, directions map[string][]float64, th placeorg.Thresholds) (placeorg.Op, bool) {
	path := pathOf(t, chat.ID)
	if len(path) == 0 {
		// Already in Now: an earlier pass put it there, or it never had a place.
		// Reporting it keeps the ops a complete account of the corpus.
		return placeorg.Op{
			Kind: placeorg.OpInbox, Stage: placeorg.StagePark, Chat: chat.ID,
			Why: "This chat stays in Now.",
		}, false
	}
	if len(vector) == 0 {
		return inboxOp(chat.ID), false
	}
	scores := make([]float64, len(path))
	for i, step := range path {
		scores[i] = similarity(vector, directions[step.id])
	}
	if belowFloor(scores, float64(th.Floor)) {
		return inboxOp(chat.ID), false
	}
	at := landingIndex(scores, float64(th.Margin))
	step := path[at]
	return placeOp(chat.ID, step, scores), true
}

func inboxOp(chat string) placeorg.Op {
	return placeorg.Op{
		Kind:  placeorg.OpInbox,
		Stage: placeorg.StagePark,
		Chat:  chat,
		Why:   "Nothing is close enough, so this chat stays in Now.",
	}
}

func placeOp(chat string, step pathStep, scores []float64) placeorg.Op {
	op := placeorg.Op{
		Stage:  placeorg.StagePark,
		Place:  step.id,
		Chat:   chat,
		Level:  step.depth,
		Scores: float32Scores(scores),
	}
	// A filing is the chat landing on a small place. Landing on the place
	// above that, or on the top place, is a park: the smaller one was not
	// clearly closer. Depth counts from one at the top.
	if step.depth >= 3 && step.deepest {
		op.Kind = placeorg.OpFile
		op.Why = "This chat fits the smaller place."
		return op
	}
	op.Kind = placeorg.OpPark
	op.Why = "This chat stays with the broader place."
	return op
}

func float32Scores(scores []float64) []float32 {
	out := make([]float32, len(scores))
	for i, s := range scores {
		out[i] = float32(s)
	}
	return out
}

func oneWidth(vectors [][]float32) error {
	width := 0
	for _, v := range vectors {
		if len(v) == 0 {
			continue
		}
		if width == 0 {
			width = len(v)
			continue
		}
		if len(v) != width {
			return fmt.Errorf("placeorg/park: vectors have widths %d and %d, and one run lives in one space", width, len(v))
		}
	}
	return nil
}
