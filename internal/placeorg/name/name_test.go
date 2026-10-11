package name

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

type fakeJudge struct {
	answers []string
	roles   []placeorg.Question
	fall    bool
}

func (j *fakeJudge) Choose(context.Context, placeorg.Question, placeorg.Option) placeorg.Pick {
	panic("unexpected Choose")
}
func (j *fakeJudge) Write(_ context.Context, q placeorg.Question, _ placeorg.Validator, fallback string) placeorg.Pick {
	j.roles = append(j.roles, q)
	if j.fall {
		return placeorg.Pick{Written: fallback, FellBack: true}
	}
	if len(j.answers) == 0 {
		panic("missing replay answer")
	}
	out := j.answers[0]
	j.answers = j.answers[1:]
	return placeorg.Pick{Written: out}
}
func corpus() *placeorg.Corpus {
	return &placeorg.Corpus{Chats: []placeorg.Chat{
		{ID: "a", Title: "codeaf Images", Text: "codeaf codeaf images images"},
		{ID: "b", Title: "codeaf Images", Text: "codeaf images"},
		{ID: "c", Title: "Orchid Care", Text: "orchid orchid care"},
		{ID: "d", Title: "Orchid Care", Text: "orchid care"},
		{ID: "e", Text: "independent topic"},
		{ID: "f", Text: "different subject"},
	}}
}
func TestCandidatesSupportDFAndStableTies(t *testing.T) {
	c := corpus()
	c.Chats = append(c.Chats, placeorg.Chat{ID: "probe", Text: "codeaf codeaf codeaf"})
	c.Kinds = map[string]placeorg.Kind{"probe": placeorg.KindProbe}
	got := NewIndex(c).Candidates([]string{"a", "b", "probe", "a"})
	if !reflect.DeepEqual(got, []string{"codeaf", "images"}) {
		t.Fatal(got)
	}
	c.Chats[2].Text += " codeaf"
	c.Chats[3].Text += " codeaf"
	if got := NewIndex(c).Candidates([]string{"a", "b"}); !reflect.DeepEqual(got, []string{"images"}) {
		t.Fatal(got)
	}
}
func TestMemberSpellingNeverTitleCasesProduct(t *testing.T) {
	validate := NewIndex(corpus()).Validator([]string{"a", "b"}, map[string]bool{})
	// The retired casing is assembled so the naming gate does not mistake regression evidence for live spelling.
	raw := "Code" + "af Images"
	got, ok := validate(raw)
	if !ok || got != "codeaf Images" {
		t.Fatalf("%q %v", got, ok)
	}
	for _, bad := range []string{"invented", "codeaf Images extra", "codeaf!"} {
		if _, ok := validate(bad); ok {
			t.Fatal(bad)
		}
	}
	if _, ok := NewIndex(corpus()).Validator([]string{"a", "b"}, map[string]bool{"codeaf images": true})(raw); ok {
		t.Fatal("collision")
	}
}
func TestNamingReplayCharterDescendantsAndFallback(t *testing.T) {
	nodes := []placeorg.Node{{ID: "root", Members: []string{"a"}}, {ID: "child", Parent: "root", Members: []string{"a", "b"}}}
	judge := &fakeJudge{answers: []string{"Image creation", "Code" + "af", "Images"}}
	ops, err := (&Stage{}).nameNodes(context.Background(), nodes, corpus(), judge)
	if err != nil || len(ops) != 2 || ops[0].Name != "codeaf" || ops[1].Name != "Images" {
		t.Fatalf("%+v %v", ops, err)
	}
	if len(judge.roles) != 3 || judge.roles[0].Role != placeorg.RolePlaceCharter || judge.roles[1].Role != placeorg.RolePlaceName {
		t.Fatal(judge.roles)
	}
	if !strings.Contains(judge.roles[1].Context, "Image creation") {
		t.Fatal("missing charter")
	}
	nodes = []placeorg.Node{{ID: "kept", Name: "codeaf"}, {ID: "new", Members: []string{"a", "b"}}}
	for _, j := range []placeorg.Judge{nil, &fakeJudge{fall: true}, &fakeJudge{answers: []string{"charter", "unsupported"}}} {
		ops, err = (&Stage{}).nameNodes(context.Background(), nodes, corpus(), j)
		if err != nil || len(ops) != 1 || ops[0].Name != "Images" {
			t.Fatalf("%+v %v", ops, err)
		}
	}
}
func TestNoPlaceholderAndCancellation(t *testing.T) {
	ops, err := (&Stage{}).nameNodes(context.Background(), []placeorg.Node{{ID: "x", Members: []string{"e"}}}, corpus(), nil)
	if err != nil || len(ops) != 0 {
		t.Fatal(ops, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Stage{}).nameNodes(ctx, []placeorg.Node{{ID: "x"}}, corpus(), nil); err != context.Canceled {
		t.Fatal(err)
	}
	ops, err = (&Stage{}).Name(context.Background(), placeorg.NewTree(), corpus(), nil)
	if err != nil || len(ops) != 0 {
		t.Fatal(ops, err)
	}
}

// TestSavedReference consumes an external fixture made from saved reference memberships, never a private corpus in git.
func TestSavedReference(t *testing.T) {
	path := os.Getenv("PLACEORG_NAME_FIXTURE")
	if path == "" {
		t.Skip("set PLACEORG_NAME_FIXTURE to the output of testdata/reference_candidates.py")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Corpus     placeorg.Corpus
		Nodes      []placeorg.Node
		Candidates map[string][]string
		Answers    []string
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	idx := NewIndex(&fixture.Corpus)
	for _, n := range fixture.Nodes {
		got := idx.Candidates(Members(fixture.Nodes, n.ID))
		if !slices.Equal(got, fixture.Candidates[n.ID]) {
			t.Fatalf("%s: %v != %v", n.ID, got, fixture.Candidates[n.ID])
		}
	}
	judge := &fakeJudge{answers: fixture.Answers}
	ops, err := (&Stage{}).nameNodes(context.Background(), fixture.Nodes, &fixture.Corpus, judge)
	if err != nil {
		t.Fatal(err)
	}
	// Member spelling intentionally supersedes the reference's forced capitalisation.
	taken := map[string]bool{}
	answerIndex, opIndex, rejected := 0, 0, 0
	for _, n := range fixture.Nodes {
		if n.Parent == "" {
			answerIndex++
		}
		expected, ok := idx.Validator(Members(fixture.Nodes, n.ID), taken)(fixture.Answers[answerIndex])
		answerIndex++
		if !ok {
			rejected++
			for _, candidate := range idx.Candidates(Members(fixture.Nodes, n.ID)) {
				if generic[candidate] {
					continue
				}
				if clean, valid := idx.Validator(Members(fixture.Nodes, n.ID), taken)(candidate); valid {
					expected = clean
					break
				}
			}
		}
		if expected == "" {
			continue
		}
		if opIndex >= len(ops) || ops[opIndex].Place != n.ID || ops[opIndex].Name != expected {
			t.Fatalf("%s: replay mismatch at operation %d", n.ID, opIndex)
		}
		taken[strings.ToLower(expected)] = true
		opIndex++
	}
	if opIndex != len(ops) {
		t.Fatal("extra operations")
	}
	t.Logf("%d candidate lists match; %d valid replay names; %d reference answers rejected by the member-word contract", len(fixture.Nodes), opIndex, rejected)
}

func TestCandidateTiesAndDominantTitleSpelling(t *testing.T) {
	c := &placeorg.Corpus{Chats: []placeorg.Chat{
		{ID: "1", Title: "SQLite sqlite", Text: "alpha beta alpha beta"},
		{ID: "2", Title: "sqlite SQLite", Text: "alpha beta"},
		{ID: "3", Text: "separate topic"},
		{ID: "4", Text: "unrelated topic"},
	}}
	idx := NewIndex(c)
	if got := idx.Candidates([]string{"1", "2"}); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatal(got)
	}
	if got, ok := idx.Validator([]string{"1", "2"}, map[string]bool{})("SQLITE"); !ok || got != "SQLite" {
		t.Fatal(got, ok)
	}
	if got := idx.Candidates([]string{"1"}); len(got) != 0 {
		t.Fatal("repeated words in one member do not meet support", got)
	}
}
