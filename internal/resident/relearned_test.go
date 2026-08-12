package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The five wordings of one lesson, taken from the user's own notebook. Each was
// recorded weeks apart, each superseded the one before it, and every one of them
// says the same thing.
var relearnedWordings = []string{
	"When the working method requires the final message to contain the full deliverables written out, include every line of them in that message rather than a pointer to the file.",
	"If the method says the final message must carry the deliverables in full, write them all out in the message instead of naming where they were saved.",
	"Deliverables belong in the final message in full when the working method asks for them; do not hand back a file path in place of the content.",
	"When a working method requires the full deliverables in the final message, every one of them must appear there in full, not as a reference to a saved file.",
}

// The loop this closes: a lesson relearned five times, each pass paying a write
// to say a sentence the notebook already held in four other shapes. The active
// view showed ONE line, because supersession is how the notebook stays small, so
// nothing reading it could tell the fifth copy from a new belief.
func TestALessonAlreadyInTheLineageIsNotLearnedAgain(t *testing.T) {
	graph := openStore(t)
	recorded := make([]store.Fact, 0, len(relearnedWordings))
	for _, body := range relearnedWordings {
		fact, err := graph.RecordFactFrom(store.FactWriterDistiller, store.RootID, "user", store.FactPreference, body)
		if err != nil {
			t.Fatalf("record %q: %v", body[:20], err)
		}
		if len(recorded) > 0 {
			if err := graph.SupersedeFact(recorded[len(recorded)-1].Seq, fact.Seq); err != nil {
				t.Fatal(err)
			}
		}
		recorded = append(recorded, fact)
	}
	// The active view is one line; the lineage is all of them, which is the
	// difference the guard reads.
	lineage, err := graph.FactLineage("user", 0)
	if err != nil || len(lineage) != len(relearnedWordings) {
		t.Fatalf("lineage = %d facts err=%v", len(lineage), err)
	}

	reconciler := New(graph, nil, nil)
	sixth := Learned{
		Scope: "user", Kind: store.FactPreference,
		Body: "The final message has to contain the full deliverables written out whenever the working method requires it, rather than a pointer to the file they were saved in.",
	}
	if !reconciler.alreadyLearned(sixth) {
		t.Fatal("the sixth wording of one lesson was treated as a new belief")
	}
	// A genuinely different belief in the same scope is untouched.
	fresh := Learned{
		Scope: "user", Kind: store.FactPreference,
		Body: "Prefers the reasoning before the recommendation, and tables over prose when comparing options.",
	}
	if reconciler.alreadyLearned(fresh) {
		t.Fatal("a new preference was refused for rhyming with an old one")
	}
	// And the guard is narrow: a per-repository line is cheap, local, and none
	// of its business.
	local := Learned{Scope: "repo:aforge", Kind: store.FactPreference, Body: relearnedWordings[0]}
	if reconciler.alreadyLearned(local) {
		t.Fatal("a scoped belief was judged against the user's standing notebook")
	}
}

// The same thing through the door it actually arrives by: the retrospective's
// own write.
func TestTheRetrospectiveStopsRewritingALessonItAlreadyHolds(t *testing.T) {
	graph := openStore(t)
	for index := 1; index <= reflectionMinJobs; index++ {
		settleRetrospectiveJob(t, graph, index)
	}
	held, err := graph.RecordFactFrom(store.FactWriterDistiller, store.RootID, "user",
		store.FactPreference, relearnedWordings[0])
	if err != nil {
		t.Fatal(err)
	}

	learned := []Learned{{
		Scope: "user", Kind: store.FactPreference, Body: relearnedWordings[1],
	}, {
		Scope: "user", Kind: store.FactPreference,
		Body: "Reads the morning briefing on a phone, so keep its rows to one line each.",
	}}
	reconciler := New(graph, nil, nil).WithReflector(
		func(context.Context, []JobSketch) ([]Learned, error) { return learned, nil })
	reconciler.reflectOnJobs(context.Background())

	facts, err := graph.FactLineage("user", 0)
	if err != nil {
		t.Fatal(err)
	}
	repeats, fresh := 0, 0
	for _, fact := range facts {
		switch {
		case strings.Contains(fact.Body, "deliverable"):
			repeats++
		case strings.Contains(fact.Body, "morning briefing"):
			fresh++
		}
	}
	if repeats != 1 {
		t.Fatalf("the lesson is now held %d times", repeats)
	}
	if fresh != 1 {
		t.Fatalf("the genuinely new line was recorded %d times", fresh)
	}
	if held.Status != store.FactActive {
		t.Fatalf("the standing line = %q", held.Status)
	}
	_ = time.Now
}
