package placeorg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func testJudge(ask placegraph.Asker, budget Budget) (*meteredJudge, *memJournal) {
	j := &memJournal{}
	now := fixedClock()
	return &meteredJudge{ask: ask, budget: budget, note: j.Write, run: "r", now: now}, j
}

func answering(s string) placegraph.Asker {
	return func(context.Context, placegraph.ModelRequest) (string, error) { return s, nil }
}

var facet = Question{Role: RolePlaceFacet, Purpose: "Thing or activity?", Options: []Option{{Key: "THING", Text: "one thing"}, {Key: "ACTIVITY", Text: "one kind of activity"}}}

func TestChooseReturnsAnOptionTheModelWasShown(t *testing.T) {
	for _, answer := range []string{`{"pick":"ACTIVITY"}`, "activity", "```json\n{\"pick\": \"ACTIVITY\"}\n```", "one kind of activity"} {
		j, _ := testJudge(answering(answer), Budget{Calls: 5})
		if p := j.Choose(context.Background(), facet, Option{Key: "KEEP", Text: "keep"}); p.Key != "ACTIVITY" || p.FellBack {
			t.Errorf("answer %q gave %+v, want ACTIVITY", answer, p)
		}
	}
}

func TestChooseTakesTheFallbackForAnythingOutsideTheContract(t *testing.T) {
	cases := map[string]placegraph.Asker{
		"an option it was not shown": answering(`{"pick":"MAYBE"}`),
		"prose":                      answering("I think it is about gardening"),
		"an error":                   func(context.Context, placegraph.ModelRequest) (string, error) { return "", errors.New("boom") },
		"no model":                   nil,
	}
	for name, ask := range cases {
		j, journal := testJudge(ask, Budget{Calls: 5})
		p := j.Choose(context.Background(), facet, Option{Key: "KEEP", Text: "keep"})
		if p.Key != "KEEP" || !p.FellBack || p.Why == "" {
			t.Errorf("%s: got %+v, want the fallback with a reason", name, p)
		}
		if !journal.has(EntryJudgment) || !journal.has(EntryFallback) {
			t.Errorf("%s: the judgment and its fallback must both be journaled", name)
		}
	}
}

func TestWriteHoldsTheAnswerToTheValidator(t *testing.T) {
	valid := func(raw string) (string, bool) {
		raw = strings.Trim(strings.TrimSpace(raw), `"`)
		return raw, len(strings.Fields(raw)) <= 2
	}
	j, _ := testJudge(answering(`"Garden notes"`), Budget{Calls: 5})
	if p := j.Write(context.Background(), Question{Role: RolePlaceName, Purpose: "Name it."}, valid, "fallback"); p.Written != "Garden notes" || p.FellBack {
		t.Errorf("a valid name was not taken: %+v", p)
	}
	j, _ = testJudge(answering("a name that is far too long"), Budget{Calls: 5})
	if p := j.Write(context.Background(), Question{Role: RolePlaceName, Purpose: "Name it."}, valid, "fallback"); p.Written != "fallback" || !p.FellBack {
		t.Errorf("an invalid name was taken: %+v", p)
	}
}

func TestTheBudgetCutsTheModelOffByCallsAndByDollars(t *testing.T) {
	asked := 0
	ask := func(context.Context, placegraph.ModelRequest) (string, error) { asked++; return "THING", nil }
	j, _ := testJudge(ask, Budget{Calls: 2})
	for i := 0; i < 4; i++ {
		j.Choose(context.Background(), facet, Option{Key: "KEEP", Text: "keep"})
	}
	if spent, over := j.snapshot(); asked != 2 || spent.Calls != 2 || spent.Fallbacks != 2 || !over {
		t.Errorf("asked %d, spent %+v, over %v; want 2 calls then 2 fallbacks", asked, spent, over)
	}

	asked = 0
	j, _ = testJudge(ask, Budget{Calls: 10, USD: 0.01})
	j.price = func(string, string) float64 { return 0.006 }
	for i := 0; i < 4; i++ {
		j.Choose(context.Background(), facet, Option{Key: "KEEP", Text: "keep"})
	}
	if asked != 2 {
		t.Errorf("asked %d times, want the dollar limit to stop it after 2", asked)
	}
}

func TestAZeroBudgetAsksNothing(t *testing.T) {
	j, _ := testJudge(answering("THING"), Budget{})
	if p := j.Choose(context.Background(), facet, Option{Key: "KEEP", Text: "keep"}); !p.FellBack || p.Why != "over budget" {
		t.Errorf("got %+v, want a fallback for a run with no budget", p)
	}
}

func TestACancelledContextIsAFallbackNotACall(t *testing.T) {
	asked := false
	j, _ := testJudge(func(context.Context, placegraph.ModelRequest) (string, error) { asked = true; return "THING", nil }, Budget{Calls: 3})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p := j.Choose(ctx, facet, Option{Key: "KEEP", Text: "keep"}); !p.FellBack || asked {
		t.Errorf("a cancelled run still asked the model: %+v", p)
	}
}

func TestALostJournalIsReportedToTheRunner(t *testing.T) {
	j, _ := testJudge(answering("THING"), Budget{Calls: 3})
	j.note = func(Entry) error { return errors.New("disk full") }
	j.Choose(context.Background(), facet, Option{Key: "KEEP", Text: "keep"})
	if j.failure() == nil {
		t.Error("the Judge swallowed a journal error")
	}
}

func TestTheModelIsOnlyShownWhatTheQuestionHolds(t *testing.T) {
	var got placegraph.ModelRequest
	j, _ := testJudge(func(_ context.Context, r placegraph.ModelRequest) (string, error) { got = r; return "THING", nil }, Budget{Calls: 1})
	q := facet
	q.Context = "Titles: a, b, c"
	j.Choose(context.Background(), q, Option{Key: "KEEP", Text: "keep"})
	for _, want := range []string{"Thing or activity?", "Titles: a, b, c", "THING: one thing", "ACTIVITY: one kind of activity"} {
		if !strings.Contains(got.User, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, got.User)
		}
	}
	if got.Role != RolePlaceFacet {
		t.Errorf("asked as %q, want the question's own role", got.Role)
	}
}
