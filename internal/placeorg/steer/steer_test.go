package steer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// fakeJudge answers like a model that has read the sentence: choose maps the
// question's context to a key, write maps it to text. A nil function, or an
// answer outside the contract, is the fallback, exactly as the real Judge does.
type fakeJudge struct {
	choose func(q placeorg.Question) string
	write  func(q placeorg.Question) string
	writes []roles.Role
}

func (f *fakeJudge) Choose(_ context.Context, q placeorg.Question, fb placeorg.Option) placeorg.Pick {
	if f.choose != nil {
		key := f.choose(q)
		for _, o := range q.Options {
			if o.Key == key {
				return placeorg.Pick{Key: key}
			}
		}
	}
	return placeorg.Pick{Key: fb.Key, FellBack: true, Why: "no model"}
}

func (f *fakeJudge) Write(_ context.Context, q placeorg.Question, valid placeorg.Validator, fb string) placeorg.Pick {
	f.writes = append(f.writes, q.Role)
	if f.write != nil {
		if text, ok := valid(f.write(q)); ok {
			return placeorg.Pick{Written: text}
		}
	}
	return placeorg.Pick{Written: fb, FellBack: true, Why: "no model"}
}

// sentenceModel is a stand-in for a model that understands the wishes.
func sentenceModel(q placeorg.Question) string {
	s := strings.ToLower(q.Context)
	for _, k := range []string{"month", "project", "flat", "separate"} {
		if strings.Contains(s, k) {
			return k
		}
	}
	return "none"
}

// library is one place "P" holding n chats, with a child "C" under it.
func library(n int, anchors func(i int) placeorg.AnchorSet) (*placeorg.Tree, *placeorg.Corpus) {
	corpus := &placeorg.Corpus{Kinds: map[string]placeorg.Kind{}, Anchors: map[string]placeorg.AnchorSet{}}
	var members []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c%02d", i)
		members = append(members, id)
		corpus.Chats = append(corpus.Chats, placeorg.Chat{ID: id, Title: "title " + id, Workspace: fmt.Sprintf("/w/dir%d", i%3)})
		if anchors != nil {
			corpus.Anchors[id] = anchors(i)
		}
	}
	tree := placeorg.TreeFromNodes([]placeorg.Node{
		{ID: "P", Name: "Desk", Members: members, Children: []string{"C"}, Depth: 1},
		{ID: "C", Name: "Sub", Parent: "P", Depth: 2},
	})
	return tree, corpus
}

func parse(t *testing.T, sentence string, j placeorg.Judge) placeorg.Steer {
	t.Helper()
	s, _, err := New().Parse(context.Background(), nil, sentence, j)
	if err != nil {
		t.Fatal(err)
	}
	s.Scope = "P"
	return s
}

func TestBothHarnessWishesParseToTheStatedActions(t *testing.T) {
	j := &fakeJudge{choose: sentenceModel}
	if s := parse(t, "organise codeaf by month", j); s.Action != placeorg.SteerAxis || s.Axis != placeorg.AxisMonth {
		t.Fatalf("by month parsed as %+v", s)
	}
	// The harness's second wish is a filter the grammar has no verb for: it
	// is not understood, and nothing changes.
	s := parse(t, "keep only codeaf, archive everything else", &fakeJudge{})
	if s.Action != placeorg.SteerNone {
		t.Fatalf("filter wish parsed as %+v", s)
	}
	if s := parse(t, "organise by project", j); s.Axis != placeorg.AxisProject {
		t.Fatalf("by project parsed as %+v", s)
	}
}

func TestNonsenseIsNoneAndRendersNothing(t *testing.T) {
	tree, corpus := library(8, nil)
	s := parse(t, "purple monkey dishwasher", &fakeJudge{choose: sentenceModel})
	ops, err := New().Render(context.Background(), tree, corpus, s, &fakeJudge{})
	if s.Action != placeorg.SteerNone || err != nil || len(ops) != 0 {
		t.Fatalf("got %+v, %v, %v", s, ops, err)
	}
	if s := parse(t, "   ", &fakeJudge{}); s.Action != placeorg.SteerNone {
		t.Fatalf("empty sentence parsed as %+v", s)
	}
}

func TestByMonthRendersALensAndNoSplit(t *testing.T) {
	tree, corpus := library(30, nil)
	s := parse(t, "organise this by month", &fakeJudge{choose: sentenceModel})
	ops, err := New().Render(context.Background(), tree, corpus, s, &fakeJudge{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []placeorg.OpKind
	for _, op := range ops {
		kinds = append(kinds, op.Kind)
		if op.Kind == placeorg.OpSplit || op.Proposed {
			t.Fatalf("by month must be a lens, applied at once: %+v", op)
		}
	}
	if fmt.Sprint(kinds) != "[axis lens]" {
		t.Fatalf("ops = %v", kinds)
	}
}

func TestByProjectProposesSplitsWhoseChildrenAreAnchorsAtQuorum(t *testing.T) {
	anchors := func(i int) placeorg.AnchorSet {
		switch {
		case i < 6:
			return placeorg.AnchorSet{"alpha": 2, "shared": 1}
		case i < 10:
			return placeorg.AnchorSet{"beta": 2, "shared": 1}
		}
		return placeorg.AnchorSet{fmt.Sprintf("rare%d", i): 2, "shared": 1}
	}
	tree, corpus := library(12, anchors)
	s := parse(t, "organise by project", &fakeJudge{choose: sentenceModel})
	ops, err := New().Render(context.Background(), tree, corpus, s, &fakeJudge{})
	if err != nil {
		t.Fatal(err)
	}
	var splits []placeorg.Op
	for _, op := range ops {
		if op.Kind == placeorg.OpSplit {
			splits = append(splits, op)
		}
	}
	if len(splits) != 2 || splits[0].Name != "alpha" || splits[1].Name != "beta" {
		t.Fatalf("splits = %+v", splits)
	}
	for _, sp := range splits {
		if !sp.Proposed || len(sp.Members) < placeorg.Quorum || sp.Place != "P" {
			t.Fatalf("split breaks the contract: %+v", sp)
		}
	}
	if ops[0].Kind != placeorg.OpAxis || ops[0].Name != "project" {
		t.Fatalf("first op = %+v", ops[0])
	}
}

func TestProbesAndDuplicatesNeverCountTowardAChild(t *testing.T) {
	tree, corpus := library(8, func(i int) placeorg.AnchorSet { return placeorg.AnchorSet{"alpha": 1} })
	for _, id := range []string{"c00", "c01", "c02", "c03"} {
		corpus.Kinds[id] = placeorg.KindProbe
	}
	s := placeorg.Steer{Action: placeorg.SteerAxis, Axis: placeorg.AxisProject, Scope: "P"}
	ops, _ := New().Render(context.Background(), tree, corpus, s, &fakeJudge{})
	for _, op := range ops {
		if op.Kind == placeorg.OpSplit {
			t.Fatalf("four real chats sharing an anchor with the whole place separate nothing: %+v", op)
		}
	}
}

func TestByKindLabelsInBatchesOfTwentyThroughTheKindRole(t *testing.T) {
	tree, corpus := library(25, nil)
	for i := range corpus.Chats {
		corpus.Chats[i].Title = "fix thing " + fmt.Sprint(i)
		if i >= 20 {
			corpus.Chats[i].Title = "design thing " + fmt.Sprint(i)
		}
	}
	j := &fakeJudge{} // no model: the verb heuristic labels every chat
	s := placeorg.Steer{Action: placeorg.SteerAxis, Axis: placeorg.AxisKindOfWork, Scope: "P"}
	ops, err := New().Render(context.Background(), tree, corpus, s, j)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.writes) != 2 || j.writes[0] != placeorg.RolePlaceKind {
		t.Fatalf("asked %v", j.writes)
	}
	got := map[string]int{}
	for _, op := range ops {
		if op.Kind == placeorg.OpSplit {
			got[op.Name] = len(op.Members)
		}
	}
	if got["fix"] != 20 || got["plan"] != 5 {
		t.Fatalf("labels = %v", got)
	}
}

func TestByKindTakesTheModelsLabelWhenItIsInTheClosedList(t *testing.T) {
	tree, corpus := library(6, nil)
	j := &fakeJudge{write: func(placeorg.Question) string {
		return "1: research\n2: research\n3: research\n4: research\n5: research\n6: not a kind"
	}}
	s := placeorg.Steer{Action: placeorg.SteerAxis, Axis: placeorg.AxisKindOfWork, Scope: "P"}
	ops, _ := New().Render(context.Background(), tree, corpus, s, j)
	for _, op := range ops {
		if op.Kind == placeorg.OpSplit && (op.Name != "research" || len(op.Members) != 5) {
			t.Fatalf("split = %+v", op)
		}
	}
}

func TestSeparateReadsTheGroupsFromTheSentenceWhenThereIsNoModel(t *testing.T) {
	s := parse(t, "keep papers separate from code", &fakeJudge{choose: sentenceModel})
	if s.Action != placeorg.SteerSeparate || strings.Join(s.Values, ",") != "papers,code" {
		t.Fatalf("steer = %+v", s)
	}
	tree, corpus := library(10, nil)
	for i := range corpus.Chats {
		corpus.Chats[i].Title = "a paper on topic"
		if i%2 == 1 {
			corpus.Chats[i].Title = "code for topic"
		}
	}
	ops, _ := New().Render(context.Background(), tree, corpus, s, &fakeJudge{})
	if len(ops) != 3 || ops[0].Kind != placeorg.OpAxis || strings.Join(ops[0].Children, ",") != "papers,code" {
		t.Fatalf("ops = %+v", ops)
	}
}

func TestSeparateWithoutTwoGroupsIsNotUnderstood(t *testing.T) {
	if s := parse(t, "keep it separate", &fakeJudge{choose: func(placeorg.Question) string { return "separate" }}); s.Action != placeorg.SteerNone {
		t.Fatalf("steer = %+v", s)
	}
}

func TestFlatProposesDissolvingDescendantsDeepestFirst(t *testing.T) {
	tree := placeorg.TreeFromNodes([]placeorg.Node{
		{ID: "P", Name: "Desk", Children: []string{"C"}, Depth: 1},
		{ID: "C", Name: "Sub", Parent: "P", Children: []string{"G"}, Depth: 2},
		{ID: "G", Name: "Leaf", Parent: "C", Depth: 3},
	})
	ops, err := New().Render(context.Background(), tree, &placeorg.Corpus{}, placeorg.Steer{Action: placeorg.SteerFlat, Scope: "P"}, &fakeJudge{})
	if err != nil || len(ops) != 3 || ops[1].Place != "G" || ops[2].Place != "C" {
		t.Fatalf("ops = %+v, %v", ops, err)
	}
	for _, op := range ops {
		if !op.Proposed {
			t.Fatalf("flattening applies nothing without a yes: %+v", op)
		}
	}
}

func TestAScopeOutsideTheTreeIsAnErrorNotAGuess(t *testing.T) {
	tree, corpus := library(5, nil)
	_, err := New().Render(context.Background(), tree, corpus, placeorg.Steer{Action: placeorg.SteerAxis, Axis: placeorg.AxisMonth, Scope: "nope"}, &fakeJudge{})
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestFallbacksAreStated(t *testing.T) {
	if len(New().Fallbacks()) == 0 {
		t.Fatal("a Judged stage states its fallbacks")
	}
}
