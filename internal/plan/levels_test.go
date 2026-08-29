package plan

import (
	"context"
	"strings"
	"testing"
)

// Seven stages that name no needs are one level: the list's order was the
// order the tools were spoken in, never a gate. The fan-out receives one stage
// that still names every tool, so nothing the model drew is lost — only the
// sequence it never meant.
func TestStagesThatNeedNothingFromEachOtherAreOneLevel(t *testing.T) {
	var drawn []Stage
	for _, name := range []string{"csvstats", "mdlite", "slug", "roman", "wordfreq", "isodur", "luhn"} {
		drawn = append(drawn, Stage{Title: name, Summary: "build " + name, Needs: []int{}})
	}
	stages := levelled(drawn)
	if len(stages) != 1 {
		t.Fatalf("levelled %d independent stages into %d, want 1", len(drawn), len(stages))
	}
	if stages[0].Title != "csvstats, mdlite, slug, roman, wordfreq, isodur, luhn" {
		t.Fatalf("merged title = %q", stages[0].Title)
	}
	for _, name := range []string{"csvstats: build csvstats", "luhn: build luhn"} {
		if !strings.Contains(stages[0].Summary, name) {
			t.Fatalf("merged summary lost %q:\n%s", name, stages[0].Summary)
		}
	}
	if stages[0].Needs != nil {
		t.Fatalf("a levelled stage still carries needs: %v", stages[0].Needs)
	}
}

// A stage that consumes what two others produce is the level after them, and
// the two it consumes are one level — the assembler behind the workers, which
// is the one gate a bundle of requests ever has.
func TestAStageThatConsumesOthersIsTheLevelAfterThem(t *testing.T) {
	stages := levelled([]Stage{
		{Title: "Profile vendor A", Summary: "profile A", Needs: []int{}},
		{Title: "Profile vendor B", Summary: "profile B", Needs: []int{}},
		{Title: "Compare", Summary: "compare the profiles", Needs: []int{1, 2}},
	})
	if len(stages) != 2 {
		t.Fatalf("got %d levels, want 2: the two profiles, then the comparison", len(stages))
	}
	if stages[0].Title != "Profile vendor A, Profile vendor B" || stages[1].Title != "Compare" {
		t.Fatalf("levels = %q, %q", stages[0].Title, stages[1].Title)
	}
}

// A chain the model actually means — each stage consuming the one before —
// is left exactly as it was drawn.
func TestAGenuineChainKeepsItsStages(t *testing.T) {
	stages := levelled([]Stage{
		{Title: "Gather", Summary: "gather the responses", Needs: []int{}},
		{Title: "Code", Summary: "code the responses", Needs: []int{1}},
		{Title: "Write up", Summary: "write the findings", Needs: []int{2}},
	})
	if len(stages) != 3 || stages[0].Title != "Gather" || stages[2].Title != "Write up" {
		t.Fatalf("a genuine chain was reshaped: %+v", stages)
	}
}

// A need that names nothing the planner can honour — a later stage, the stage
// itself, a position off the end — is not a gate. It is ignored, not trusted.
func TestANeedThatPointsAtNothingIsNotAGate(t *testing.T) {
	stages := levelled([]Stage{
		{Title: "One", Summary: "one", Needs: []int{1, 2, 9}},
		{Title: "Two", Summary: "two", Needs: []int{0, -1, 2}},
	})
	if len(stages) != 1 {
		t.Fatalf("unresolvable needs gated %d levels, want 1", len(stages))
	}
}

// Through the spine itself: a sample that wrote independent tools as a list is
// read for what it said, the choice reports both counts, and the medoid votes
// over levelled shapes — so two list-shaped samples cannot outvote the one
// that wrote the single stage plainly.
func TestTheSpineReadsItsOwnNeedsBeforeTheVote(t *testing.T) {
	var calls int
	client := &stubClient{reply: func(system, user string) string {
		if !strings.Contains(system, "You break a goal into its ordered stages") {
			return ""
		}
		calls++
		if calls == 3 {
			return `{"stages":[{"title":"Build the three tools","summary":"csv, markdown and slug tools, side by side","needs":[]}]}`
		}
		return `{"stages":[` +
			`{"title":"csvstats","summary":"the csv tool","needs":[]},` +
			`{"title":"mdlite","summary":"the markdown tool","needs":[]},` +
			`{"title":"slug","summary":"the slug tool","needs":[]}` +
			`]}`
	}}
	choice, _, err := spineWithProgress(context.Background(), client, "build three independent tools", "", nil, 3, nil)
	if err != nil {
		t.Fatalf("spine: %v", err)
	}
	if len(choice.Stages) != 1 {
		t.Fatalf("three independent tools planned as %d stages, want 1", len(choice.Stages))
	}
	if joinInts(choice.Drawn) != "1,3,3" || joinInts(choice.Spread) != "1,1,1" || !choice.Agreed {
		t.Fatalf("drawn %v spread %v agreed %v — want the list read as one level in every sample", choice.Drawn, choice.Spread, choice.Agreed)
	}
	if label := spreadLabel(choice); !strings.Contains(label, "drawn as 1,3,3") {
		t.Fatalf("the report hides what was drawn: %q", label)
	}
}

// A reply with no needs list on any stage did not answer the question, and
// its order stays the schedule it always was. Nil is "did not say"; an empty
// list is "needs nothing" — the one place that distinction carries meaning.
func TestASpineThatNeverSaidItsNeedsKeepsItsList(t *testing.T) {
	stages := levelled([]Stage{
		{Title: "Profile each vendor", Summary: "profile the three vendors"},
		{Title: "Compare the profiles", Summary: "compare them"},
	})
	if len(stages) != 2 {
		t.Fatalf("an unanswered spine was levelled to %d, want its 2 stages kept", len(stages))
	}
}
