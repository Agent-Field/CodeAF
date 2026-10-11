package stream

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func driftTree(extra int) *placeorg.Tree {
	nodes := []placeorg.Node{{ID: "start", Name: "Start", Depth: 1, Members: []string{"chat"}, Centroids: centroid(placeorg.SpacePrimary, []float32{1, 0})}}
	for i := 0; i < extra; i++ {
		id := "end" + itoa(i)
		nodes = append(nodes, placeorg.Node{ID: id, Name: id, Depth: 1, Centroids: centroid(placeorg.SpacePrimary, []float32{0, 1})})
	}
	return placeorg.TreeFromNodes(nodes)
}

func driftingArrival() placeorg.Arrival {
	a := arrival("chat", []float32{1, 0})
	a.Chat.EndText = "The conversation ended on another subject."
	return a
}

func TestDriftEarnsUncappedHomesAndOnlyProposesAPrimaryMove(t *testing.T) {
	tree := driftTree(7)
	before, _ := tree.Home("chat")
	ops, err := New().Drift(context.Background(), tree, driftingArrival(), []float32{0, 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	links, moves := 0, 0
	for _, op := range ops {
		switch op.Kind {
		case placeorg.OpLink:
			links++
			if op.Proposed || len(op.Scores) != 2 || op.Scores[0] < op.Scores[1] {
				t.Fatalf("unearned home: %+v", op)
			}
		case placeorg.OpFile, placeorg.OpPark:
			moves++
			if !op.Proposed || op.Place == before {
				t.Fatalf("primary changed without an offer: %+v", op)
			}
		default:
			t.Fatalf("unexpected op: %+v", op)
		}
	}
	if links != 7 || moves != 1 {
		t.Fatalf("links %d moves %d: %+v", links, moves, ops)
	}
	after, _ := tree.Home("chat")
	if after != before || len(tree.Links("chat")) != 0 {
		t.Fatal("Drift mutated the tree")
	}
}

func TestDriftKeepsBothAnchorsAndTheRecordedPrimary(t *testing.T) {
	tree := placeorg.TreeFromNodes([]placeorg.Node{
		{ID: "primary", Depth: 1, Members: []string{"chat"}, Centroids: centroid(placeorg.SpacePrimary, []float32{-1, 0})},
		{ID: "began", Depth: 1, Centroids: centroid(placeorg.SpacePrimary, []float32{1, 0})},
		{ID: "ended", Depth: 1, Centroids: centroid(placeorg.SpacePrimary, []float32{0, 1})},
	})
	ops, err := New().Drift(context.Background(), tree, driftingArrival(), []float32{0, 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, op := range ops {
		if op.Kind == placeorg.OpLink {
			got[op.Place] = true
		}
	}
	if !reflect.DeepEqual(got, map[string]bool{"began": true, "ended": true}) {
		t.Fatalf("homes: %+v", ops)
	}
}

func TestDriftDoesNothingWithoutAnEndAnchorOrAPrimary(t *testing.T) {
	for _, variant := range []string{"no-text", "no-vector", "zero-vector", "no-home", "probe", "wrong-space"} {
		t.Run(variant, func(t *testing.T) {
			s, tree, a, end := New(), driftTree(1), driftingArrival(), []float32{0, 1}
			switch variant {
			case "no-text":
				a.Chat.EndText = ""
			case "no-vector":
				end = nil
			case "zero-vector":
				end = []float32{0, 0}
			case "no-home":
				a.Chat.ID = "unfiled"
			case "probe":
				s.Kinds = map[string]placeorg.Kind{"chat": placeorg.KindProbe}
			case "wrong-space":
				s.Space = placeorg.SpaceLocal
			}
			ops, err := s.Drift(context.Background(), tree, a, end, nil)
			if err != nil || len(ops) != 0 {
				t.Fatalf("ops %+v err %v", ops, err)
			}
		})
	}
}

func TestDriftUsesTheEarnedFloorAndBandAndOneHomePerBranch(t *testing.T) {
	tree := placeorg.TreeFromNodes([]placeorg.Node{
		{ID: "root", Depth: 1, Children: []string{"primary", "sibling"}},
		{ID: "primary", Parent: "root", Depth: 2, Members: []string{"chat"}, Centroids: centroid(placeorg.SpacePrimary, []float32{1, 0})},
		{ID: "sibling", Parent: "root", Depth: 2, Centroids: centroid(placeorg.SpacePrimary, []float32{0, 1})},
		{ID: "other", Depth: 1, Children: []string{"best", "near"}},
		{ID: "best", Parent: "other", Depth: 2, Centroids: centroid(placeorg.SpacePrimary, []float32{0, 1})},
		{ID: "near", Parent: "other", Depth: 2, Centroids: centroid(placeorg.SpacePrimary, toward(0.1, 2))},
		{ID: "outside-band", Depth: 2, Centroids: centroid(placeorg.SpacePrimary, toward(0.5, 2))},
		{ID: "below-floor", Depth: 2, Centroids: centroid(placeorg.SpacePrimary, []float32{-1, 0})},
	})
	ops, err := New().Drift(context.Background(), tree, driftingArrival(), []float32{0, 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var links []string
	for _, op := range ops {
		if op.Kind == placeorg.OpLink {
			links = append(links, op.Place)
		}
	}
	if !reflect.DeepEqual(links, []string{"best"}) {
		t.Fatalf("links %v", links)
	}
}

func TestDriftDoesNotRepeatExistingLinks(t *testing.T) {
	tree := driftTree(1)
	setTreeField(tree, "links", map[string][]string{"chat": {"end0"}})
	ops, err := New().Drift(context.Background(), tree, driftingArrival(), []float32{0, 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Kind == placeorg.OpLink {
			t.Fatalf("repeated link %+v", op)
		}
	}
}

func TestDriftRespectsCancellationAndFolderPrior(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ops, err := New().Drift(ctx, driftTree(1), driftingArrival(), []float32{0, 1}, nil); !errors.Is(err, context.Canceled) || len(ops) != 0 {
		t.Fatalf("ops %+v err %v", ops, err)
	}
	a := driftingArrival()
	a.FolderPlace = "start"
	ops, err := New().Drift(context.Background(), driftTree(1), a, []float32{0, 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != placeorg.OpLink {
		t.Fatalf("folder prior lost: %+v", ops)
	}
}

func TestDriftContestReadsTheEndingAndStillOnlyOffersAMove(t *testing.T) {
	tree := driftTree(2)
	// The second destination is close enough to contest but not identical.
	if err := placeorg.SetCentroid(tree, "end1", placeorg.SpacePrimary, placeorg.Centroid{Sum: toward(0.1, 2), Count: 1}); err != nil {
		t.Fatal(err)
	}
	j := &fakeJudge{pick: placeorg.Pick{Key: "p1"}}
	ops, err := New().Drift(context.Background(), tree, driftingArrival(), []float32{0, 1}, j)
	if err != nil {
		t.Fatal(err)
	}
	if j.calls != 1 || j.last.Context != driftingArrival().Chat.EndText {
		t.Fatalf("contest context %+v calls %d", j.last, j.calls)
	}
	for _, op := range ops {
		if op.Kind != placeorg.OpLink && !op.Proposed {
			t.Fatalf("direct move %+v", op)
		}
	}
}

func TestDriftUsesCalibratedZeroDeltaAndRejectsMixedDimensions(t *testing.T) {
	tree := driftTree(2)
	if err := placeorg.SetCentroid(tree, "end1", placeorg.SpacePrimary, placeorg.Centroid{Sum: toward(0.1, 2), Count: 1}); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Thresholds = placeorg.Thresholds{Floor: 0.9, Margin: 0.04, Delta: 0}
	ops, err := s.Drift(context.Background(), tree, driftingArrival(), []float32{0, 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	links := 0
	for _, op := range ops {
		if op.Kind == placeorg.OpLink {
			links++
			if op.Place != "end0" {
				t.Fatalf("zero delta earned %+v", op)
			}
		}
	}
	if links != 1 {
		t.Fatalf("links %d", links)
	}
	if ops, err := s.Drift(context.Background(), tree, driftingArrival(), []float32{0, 1, 0}, nil); err == nil || len(ops) != 0 {
		t.Fatalf("mixed dimensions accepted: %+v %v", ops, err)
	}
}
