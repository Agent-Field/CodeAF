package subharness

import "testing"

// The registry these tests match against: two harnesses whose vocabularies
// overlap in exactly the way a real build's would.
var (
	research = Entry{
		Name:        "research",
		Description: "Research a question across sources and write a report",
		Cues:        []string{"research", "find out", "dig into", "look into"},
		Revision:    1,
	}
	release = Entry{
		Name:        "release",
		Description: "Cut a release: tag the commit, build, publish the notes",
		Cues:        []string{"release", "ship it", "cut a tag"},
		Revision:    2,
	}
)

func TestScoreRanksCues(t *testing.T) {
	// One single-word cue, one phrase cue, two cues, and the name itself — the
	// four rungs the weights exist to keep in this order. The numbers are not
	// asserted; the ORDER is, because the order is the contract a designer
	// writing cues relies on and the numbers are a tuning detail.
	rungs := []struct {
		name string
		turn string
	}{
		{"nothing", "rewrite the config loader in place"},
		{"one word", "research the pricing tiers"},
		{"one phrase", "find out what the pricing tiers are"},
		{"two cues", "research this and find out what the tiers are"},
		{"named", "run the research harness on the pricing tiers"},
	}
	last := -1.0
	for _, rung := range rungs {
		score := Score(Turn{Text: rung.turn}, research)
		if score <= last {
			t.Fatalf("%s scored %.3f, not above the rung below it (%.3f)", rung.name, score, last)
		}
		last = score
	}
}

func TestScorePhraseCueBeatsItsFirstWord(t *testing.T) {
	// "find out" is a cue; "find" alone is not, and a turn that happens to
	// contain the word must not inherit the phrase's weight.
	whole := Score(Turn{Text: "find out which endpoint is slow"}, research)
	part := Score(Turn{Text: "find the slow endpoint and fix it"}, research)
	if part >= whole {
		t.Fatalf("a stray %q scored %.3f against the phrase's %.3f", "find", part, whole)
	}
	if part > Threshold {
		t.Fatalf("half a phrase cue cleared the threshold at %.3f", part)
	}
}

func TestScoreTolerantOfEndings(t *testing.T) {
	// A cue is written in one form and typed in another. Four endings' worth of
	// tolerance, and no more (see [stem]).
	for _, turn := range []string{
		"researching the pricing tiers",
		"digging into the pricing tiers",
	} {
		bare := Score(Turn{Text: "rewrite the pricing tiers"}, research)
		if got := Score(Turn{Text: turn}, research); got <= bare {
			t.Fatalf("%q scored %.3f, no better than a turn with no cue at all (%.3f)", turn, got, bare)
		}
	}
}

func TestThreshold(t *testing.T) {
	cases := []struct {
		turn  string
		fires bool
		why   string
	}{
		{"summarize this file for me", false, "no cue, no name, nothing"},
		{"research the pricing tiers", false, "one single-word cue is a coincidence"},
		{"find out what the pricing tiers are", false, "one phrase cue, and no corroboration"},
		{"find out what our sources say and write a report", true, "a phrase cue the description corroborates"},
		{"research this and find out what the tiers are", true, "two of the designer's own cues"},
		{"run the research harness on the pricing tiers", true, "the harness named out loud"},
		{"Research a question across sources and write a report", true, "the description quoted, and a cue with it"},
	}
	for _, c := range cases {
		score := Score(Turn{Text: c.turn}, research)
		if fired := score > Threshold; fired != c.fires {
			t.Errorf("%q scored %.3f (fires=%v), want fires=%v — %s", c.turn, score, fired, c.fires, c.why)
		}
	}
}

func TestDescriptionAloneNeverFires(t *testing.T) {
	// A harness with no cues at all, described word for word by the turn. It is
	// the strongest a description-only match can ever be, and it is under the
	// threshold: prose corroborates, it does not decide.
	quiet := Entry{Name: "quiet", Description: "Cut a release, tag the commit, publish the notes"}
	score := Score(Turn{Text: "cut a release, tag the commit, publish the notes"}, quiet)
	if score > Threshold {
		t.Fatalf("a description quoted back fired at %.3f", score)
	}
	if score <= 0 {
		t.Fatalf("a description quoted back scored nothing (%.3f)", score)
	}
}

func TestBestPicksTheHigherAndHoldsTheThreshold(t *testing.T) {
	registry := []Entry{research, release}

	match, ok := Best(Turn{Text: "ship it — cut a tag and publish the notes"}, registry)
	if !ok {
		t.Fatalf("two release cues and its description did not fire: %.3f", match.Score)
	}
	if match.Entry.Name != "release" {
		t.Fatalf("matched %q, want release", match.Entry.Name)
	}

	// A turn that is nobody's: the best entry still comes back, so a caller can
	// log what it nearly was, and the bool says do nothing.
	match, ok = Best(Turn{Text: "what time is it"}, registry)
	if ok {
		t.Fatalf("an ordinary turn fired %q at %.3f", match.Entry.Name, match.Score)
	}
}

func TestBestIsDeterministic(t *testing.T) {
	// Ties go to the registry's own order, and the same turn asks the same
	// question every time — the property the whole no-model choice buys.
	first := Entry{Name: "first", Description: research.Description, Cues: research.Cues}
	second := Entry{Name: "second", Description: research.Description, Cues: research.Cues}
	turn := Turn{Text: "research this and find out what our sources say"}
	for i := 0; i < 8; i++ {
		match, ok := Best(turn, []Entry{first, second})
		if !ok || match.Entry.Name != "first" {
			t.Fatalf("run %d matched %q (ok=%v), want first", i, match.Entry.Name, ok)
		}
	}
}

func TestEmptyInputs(t *testing.T) {
	if score := Score(Turn{}, research); score != 0 {
		t.Fatalf("an empty turn scored %.3f", score)
	}
	if score := Score(Turn{Text: "research this"}, Entry{}); score != 0 {
		t.Fatalf("an empty entry scored %.3f", score)
	}
	if _, ok := Best(Turn{Text: "research this and find out more"}, nil); ok {
		t.Fatal("an empty registry matched")
	}
}
