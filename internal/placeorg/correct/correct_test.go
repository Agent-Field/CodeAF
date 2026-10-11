package correct

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const space = placeorg.SpaceLocal

type vecs map[string][]float32

func (v vecs) Vector(id string) ([]float32, bool) { x, ok := v[id]; return x, ok }

// world is two places, A and B, one centroid each in 2-D, and A's members.
func world(aMembers []string, aSum, bSum []float32) *placeorg.Tree {
	return placeorg.TreeFromNodes([]placeorg.Node{
		{ID: "A", Name: "Alpha", Members: aMembers, Depth: 1, Centroids: map[placeorg.SpaceID]placeorg.Centroid{space: {Sum: aSum, Count: 3}}},
		{ID: "B", Name: "Beta", Depth: 1, Centroids: map[placeorg.SpaceID]placeorg.Centroid{space: {Sum: bSum, Count: 3}}},
	})
}

func move(chat string) placeorg.Correction {
	return placeorg.Correction{Chat: chat, From: "A", To: "B", At: time.Unix(1, 0)}
}

func TestACorrectionMovesOnlyTheMovedChatAndWeighsItTwo(t *testing.T) {
	src := vecs{"x": {0.707, 0.707}, "a1": {0.65, 0.76}, "a2": {0.995, 0.1}}
	tree := world([]string{"x", "a1", "a2"}, []float32{3, 0}, []float32{0, 3})
	ops, err := New(space, src, nil).Correct(context.Background(), tree, &placeorg.Corpus{}, move("x"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != placeorg.OpExemplar || ops[0].Chat != "x" || ops[0].Place != "B" {
		t.Fatalf("ops = %+v", ops)
	}
	for _, op := range ops {
		if op.Kind == placeorg.OpFile || op.Kind == placeorg.OpPark || (op.Chat != "" && op.Chat != "x") {
			t.Fatalf("a correction moves only the moved chat: %+v", op)
		}
	}
	// The exemplar is a count of two, not a vector twice as long.
	got := with(placeorg.Centroid{Sum: []float32{0, 3}, Count: 3}, []float32{1, 0}, placeorg.ExemplarWeight)
	if got.Count != 5 || got.Sum[0] != 2 || got.Sum[1] != 3 {
		t.Fatalf("centroid = %+v", got)
	}
}

func TestNeighboursThatWouldFollowAreCountedIntoTheJournalLine(t *testing.T) {
	src := vecs{"x": {0.707, 0.707}, "a1": {0.65, 0.76}, "a2": {0.995, 0.1}}
	tree := world([]string{"x", "a1", "a2"}, []float32{3, 0}, []float32{0, 3})
	c := New(space, src, nil)
	follow, total := c.Followers(tree, move("x"))
	if follow != 2 || total != 3 {
		t.Fatalf("follow %d of %d", follow, total)
	}
	ops, _ := c.Correct(context.Background(), tree, &placeorg.Corpus{}, move("x"))
	if !strings.Contains(ops[0].Why, "2 would follow") {
		t.Fatalf("why = %q", ops[0].Why)
	}
}

// The reference's example: a chat alone in its old place, so "of 1 same-place
// neighbours, 0 would follow".
func TestTheReferenceCorrectionExampleReproducesItsCount(t *testing.T) {
	src := vecs{"x": {0.99, 0.14}}
	tree := world([]string{"x"}, []float32{3, 0}, []float32{0, 10})
	follow, total := New(space, src, nil).Followers(tree, move("x"))
	if follow != 0 || total != 1 {
		t.Fatalf("of %d same-place neighbours, %d would follow", total, follow)
	}
}

func anchored(ids ...string) *placeorg.Corpus {
	c := &placeorg.Corpus{Anchors: map[string]placeorg.AnchorSet{}}
	for i, id := range ids {
		c.Anchors[id] = placeorg.AnchorSet{"#412": 2, fmt.Sprintf("own%d", i): 1}
	}
	return c
}

func TestTheThirdRepeatProposesAConstraintOnce(t *testing.T) {
	ids := []string{"m1", "m2", "m3", "m4"}
	src := vecs{}
	for _, id := range ids {
		src[id] = []float32{1, 0}
	}
	tree := world(ids, []float32{3, 0}, []float32{0, 3})
	corpus := anchored(ids...)
	c := New(space, src, nil)
	for i, id := range ids {
		ops, err := c.Correct(context.Background(), tree, corpus, move(id))
		if err != nil {
			t.Fatal(err)
		}
		constraints := 0
		for _, op := range ops {
			if op.Kind == placeorg.OpConstraint {
				constraints++
				if op.Name != "#412" || op.Place != "B" || !op.Proposed || i != placeorg.ConstraintRepeats-1 {
					t.Fatalf("move %d: %+v", i+1, op)
				}
			}
		}
		if want := map[bool]int{true: 1, false: 0}[i == placeorg.ConstraintRepeats-1]; constraints != want {
			t.Fatalf("move %d proposed %d constraints", i+1, constraints)
		}
	}
}

func TestMovesSharingNoAnchorProposeNothing(t *testing.T) {
	ids := []string{"m1", "m2", "m3"}
	corpus := &placeorg.Corpus{Anchors: map[string]placeorg.AnchorSet{
		"m1": {"a": 1}, "m2": {"b": 1}, "m3": {"c": 1},
	}}
	tree := world(ids, []float32{3, 0}, []float32{0, 3})
	c := New(space, vecs{}, nil)
	for _, id := range ids {
		ops, _ := c.Correct(context.Background(), tree, corpus, move(id))
		if len(ops) != 1 {
			t.Fatalf("ops = %+v", ops)
		}
	}
}

func TestExemplarsOnFileCountTowardTheRepeat(t *testing.T) {
	prior := []placeorg.Exemplar{{Chat: "m1", From: "A", To: "B"}, {Chat: "m2", From: "A", To: "B"}}
	tree := world(nil, []float32{3, 0}, []float32{0, 3})
	ops, _ := New(space, vecs{}, prior).Correct(context.Background(), tree, anchored("m1", "m2", "m3"), move("m3"))
	if len(ops) != 2 || ops[1].Kind != placeorg.OpConstraint {
		t.Fatalf("ops = %+v", ops)
	}
}

func TestAMoveToAPlaceNotInTheTreeIsAnError(t *testing.T) {
	tree := world(nil, nil, nil)
	if _, err := New(space, vecs{}, nil).Correct(context.Background(), tree, &placeorg.Corpus{}, placeorg.Correction{Chat: "x", From: "A", To: "Z"}); err == nil {
		t.Fatal("want an error")
	}
}
