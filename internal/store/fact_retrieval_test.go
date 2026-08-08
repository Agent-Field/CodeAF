package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// The scenario is month three of an ordinary working relationship: the `user`
// shelf has more entries than one retrieval can carry, and every query carries
// the `user` cue because ExtractCues appends it to everything. Before reserved
// slots, that cue took all eight seats in seq DESC order and the BM25 arm never
// ran at all — so a belief the message was literally about was unreachable the
// moment anything newer happened to share a shelf with it.
func TestOldButRelevantSurvivesANewerCue(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))

	const wanted = "the pricing analysis uses the enterprise seat ladder, not per-seat"
	old, err := graph.RecordFactFrom(FactWriterDistiller, "", "domain:pricing", FactLesson, wanted)
	if err != nil {
		t.Fatal(err)
	}
	// Twelve newer beliefs on the shelf the degenerate cue points at. None of
	// them mentions pricing; every one of them is more recent.
	for _, body := range []string{
		"they read on a laptop and hate wide tables",
		"they want deliverables as one file, not a folder",
		"they prefer bullet points over paragraphs",
		"they work from a cafe on Tuesday mornings",
		"they dislike being asked to confirm cheap things",
		"they keep their notes in obsidian",
		"they want costs stated in dollars, never tokens",
		"they answer questions by number",
		"they use a 60-column terminal",
		"they want the answer first and the working after",
		"they never want emoji in a deliverable",
		"they call the product aforge, lowercase",
	} {
		if _, err := graph.RecordFactFrom(FactWriterDistiller, "", "user", FactPreference, body); err != nil {
			t.Fatal(err)
		}
	}

	found, err := graph.SearchFacts(FactQuery{
		Cues:  []string{"user", "env"},
		Terms: "redo the pricing analysis for the enterprise tier",
		Limit: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 8 {
		t.Fatalf("retrieval returned %d facts, want a full 8", len(found))
	}
	if !containsSeq(found, old.Seq) {
		t.Fatalf("the relevant older belief #%d never came back: %s", old.Seq, renderFacts(found))
	}
	// The cue arm is not starved either: relevance is reserved half, not all.
	cued := 0
	for _, fact := range found {
		if fact.Scope == "user" {
			cued++
		}
	}
	if cued < 4 {
		t.Fatalf("the cue arm kept only %d of its reserved slots: %s", cued, renderFacts(found))
	}
}

// Cues are ordered most-specific-first, and the first one used to be allowed the
// whole limit. A coding brief carrying a file path therefore never reached the
// `user` shelf — which is where a lesson the person stated in their own words
// twenty minutes ago is sitting.
func TestASpecificCueCannotStarveTheGeneralOnes(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))

	for index := range 10 {
		if _, err := graph.RecordFactFrom(FactWriterDistiller, "", "repo:/src", FactLesson,
			"the build in /src needs a clean module cache, note "+string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}
	lesson, err := graph.RecordFactFrom(FactWriterHead, "", "user", FactPreference,
		"always run what you build once before telling me it's done")
	if err != nil {
		t.Fatal(err)
	}

	found, err := graph.SearchFacts(FactQuery{
		Cues:  []string{"repo:/src", "user", "env"},
		Terms: "add a subcommand to the /src binary",
		Limit: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSeq(found, lesson.Seq) {
		t.Fatalf("the stated lesson lost every seat to the specific cue: %s", renderFacts(found))
	}
}

// Eligibility used to run after the LIMIT with no backfill: a query for eight
// could come back with two while six perfectly good beliefs sat unretrieved
// behind the cut.
func TestIneligibleRowsDoNotCostTheAnswerItsSlots(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))

	// Drive the distilled channel's survival rate under the eligibility bar, so
	// a single-occurrence line from it needs corroboration it does not have.
	for index := range 20 {
		noise, err := graph.RecordFactFrom(FactWriterDistiller, "", "domain:noise", FactLesson,
			"a claim that did not last, number "+string(rune('a'+index)))
		if err != nil {
			t.Fatal(err)
		}
		if index >= 2 {
			if err := graph.QuarantineFact(noise.Seq, noise.Seq, FactOriginConsolidator); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Six corroborated lessons, then six single-occurrence ones on top of them.
	// The ineligible six are the newest, so seq DESC hands them out first.
	for index := range 6 {
		body := "ops lesson number " + string(rune('a'+index))
		for range 2 {
			if _, err := graph.RecordFactFrom(FactWriterDistiller, "", "domain:ops", FactLesson, body); err != nil {
				t.Fatal(err)
			}
		}
	}
	for index := range 6 {
		if _, err := graph.RecordFactFrom(FactWriterDistiller, "", "domain:ops", FactLesson,
			"an uncorroborated ops guess, number "+string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}

	found, err := graph.SearchFacts(FactQuery{Cues: []string{"domain:ops"}, Limit: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 6 {
		t.Fatalf("retrieval returned %d facts, want 6 backfilled past the ineligible rows: %s",
			len(found), renderFacts(found))
	}
	for _, fact := range found {
		if strings.Contains(fact.Body, "uncorroborated") {
			t.Fatalf("an ineligible row was returned: %+v", fact)
		}
	}
}

func containsSeq(facts []Fact, seq int64) bool {
	for _, fact := range facts {
		if fact.Seq == seq {
			return true
		}
	}
	return false
}

func renderFacts(facts []Fact) string {
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		lines = append(lines, "["+fact.Scope+"] "+fact.Body)
	}
	return strings.Join(lines, " | ")
}
