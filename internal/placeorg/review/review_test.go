package review

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

type replay struct {
	raw  string
	fall bool
	q    placeorg.Question
}

func (*replay) Choose(context.Context, placeorg.Question, placeorg.Option) placeorg.Pick {
	panic("unexpected Choose")
}
func (j *replay) Write(_ context.Context, q placeorg.Question, _ placeorg.Validator, fallback string) placeorg.Pick {
	j.q = q
	if j.fall {
		return placeorg.Pick{Written: fallback, FellBack: true}
	}
	return placeorg.Pick{Written: j.raw}
}
func TestReviewValidatesRenamesAndOnlyProposesMerges(t *testing.T) {
	nodes := []placeorg.Node{{ID: "a", Name: "Image work", Members: []string{"1", "2"}, Person: true}, {ID: "b", Name: "Orchid", Members: []string{"3"}}, {ID: "c", Name: "Care", Members: []string{"4"}}}
	corpus := &placeorg.Corpus{Chats: []placeorg.Chat{{ID: "1", Title: "codeaf Images", Text: "codeaf images"}, {ID: "2", Title: "codeaf Images", Text: "codeaf images"}, {ID: "3", Title: "Orchid Care", Text: "orchid care"}, {ID: "4", Title: "Orchid Care", Text: "orchid care"}}}
	j := &replay{raw: `{"renames":{"Image work":"Code` + `af Images","Orchid":"Care","Care":"invented"},"merge_suggestions":[["Image work","Orchid"],["Orchid","Image work"],["missing","Care"],["Care","Care"]]}`}
	ops, err := (&Stage{}).reviewNodes(context.Background(), nodes, corpus, j)
	if err != nil || len(ops) != 2 {
		t.Fatalf("%+v %v", ops, err)
	}
	if ops[0].Kind != placeorg.OpRename || ops[0].Name != "codeaf Images" || !ops[0].Proposed {
		t.Fatal(ops[0])
	}
	if ops[1].Kind != placeorg.OpMerge || !ops[1].Proposed || ops[1].Place != "a" || ops[1].Into != "b" {
		t.Fatal(ops[1])
	}
	if j.q.Role != placeorg.RolePlaceReview || j.q.MaxTokens != 400 {
		t.Fatal(j.q)
	}
	if nodes[0].Name != "Image work" {
		t.Fatal("mutated input")
	}
	for _, j := range []*replay{{raw: "invalid"}, {fall: true}, {raw: `{"renames":{"Image work":"too many name words"}}`}} {
		ops, err = (&Stage{}).reviewNodes(context.Background(), nodes, corpus, j)
		if err != nil || len(ops) != 0 {
			t.Fatal(ops, err)
		}
	}
}
func TestReviewCancellationAndEmptyTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Stage{}).Review(ctx, placeorg.NewTree(), &placeorg.Corpus{}, nil); err != context.Canceled {
		t.Fatal(err)
	}
	ops, err := (&Stage{}).Review(context.Background(), placeorg.NewTree(), &placeorg.Corpus{}, &replay{})
	if err != nil || len(ops) != 0 {
		t.Fatal(ops, err)
	}
}
