package subharness

import (
	"strings"
	"testing"
)

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

// ── the scorer ──────────────────────────────────────────────────────────────

func TestScoreRanksEvidence(t *testing.T) {
	// Nothing, one word, most of a phrase, the whole phrase — the four rungs the
	// weights exist to keep in this order. The numbers are not asserted; the
	// ORDER is, because the order is the contract a designer writing cues relies
	// on and the numbers are a tuning detail.
	probe := Entry{Name: "counterweight", Cues: []string{"pricing", "strongest counterargument addressed"}}
	rungs := []struct {
		name string
		turn string
	}{
		{"nothing", "rewrite the config loader in place"},
		{"one word", "check the pricing tiers again"},
		{"most of a phrase", "give me the strongest counterargument you have"},
		{"the whole phrase", "i want the strongest counterargument addressed"},
	}
	last := -1.0
	for _, rung := range rungs {
		score := Score(Turn{Text: rung.turn}, probe)
		if score <= last {
			t.Fatalf("%s scored %.3f, not above the rung below it (%.3f)", rung.name, score, last)
		}
		last = score
	}
}

func TestNameOrWholePhraseScoresNineTenths(t *testing.T) {
	// The law the designer flow leans on: a turn that says the harness's name,
	// or one of its cues word for word, is not a guess about what somebody meant.
	probe := Entry{Name: "counterweight", Cues: []string{"strongest counterargument addressed"}}
	for _, turn := range []string{
		"run counterweight over the proposal",
		"i want the strongest counterargument addressed first",
	} {
		if score := Score(Turn{Text: turn}, probe); score < 0.9 {
			t.Errorf("%q scored %.3f, want at least 0.90", turn, score)
		}
	}
}

func TestScorePhraseCueBeatsItsFirstWord(t *testing.T) {
	// "find out" is a cue; "find" alone is not, and a turn that happens to
	// contain the word must not inherit the phrase's weight. Half of a two-word
	// cue is one word, and one word is a coincidence — [nearly] is deliberately
	// unavailable here.
	whole := Score(Turn{Text: "find out which endpoint is slow"}, research)
	part := Score(Turn{Text: "find the slow endpoint and fix it"}, research)
	if part >= whole {
		t.Fatalf("a stray %q scored %.3f against the phrase's %.3f", "find", part, whole)
	}
	if part >= Threshold {
		t.Fatalf("half a phrase cue cleared the threshold at %.3f", part)
	}
}

func TestScoreTolerantOfEndings(t *testing.T) {
	// A cue is written in one form and typed in another. A short suffix table's
	// worth of tolerance, and no more (see [canonical]).
	for _, turn := range []string{
		"researching the pricing tiers",
		"digging into the pricing tiers",
		"looks into the pricing tiers",
	} {
		bare := Score(Turn{Text: "rewrite the pricing tiers"}, research)
		if got := Score(Turn{Text: turn}, research); got <= bare {
			t.Fatalf("%q scored %.3f, no better than a turn with no cue at all (%.3f)", turn, got, bare)
		}
	}
}

func TestScoreFires(t *testing.T) {
	cases := []struct {
		turn  string
		fires bool
		why   string
	}{
		{"summarize this file for me", false, "no cue, no name, nothing"},
		{"find the slow endpoint and fix it", false, "one word of a two-word cue is a coincidence"},
		{"rewrite the loader and write a report", false, "two description words, and prose does not decide"},
		{"find out what our sources say and write a report", true, "a phrase cue the description corroborates"},
		{"dig into this and look into that", true, "two of the designer's own cues"},
		{"run the research harness on the pricing tiers", true, "the harness named out loud"},
		{"research the pricing tiers", true, "the name on its own — a name is what somebody said, not what they might have meant"},
	}
	for _, c := range cases {
		score := Score(Turn{Text: c.turn}, research)
		if fired := score >= Threshold; fired != c.fires {
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
	if score >= Threshold {
		t.Fatalf("a description quoted back fired at %.3f", score)
	}
	if score <= 0 {
		t.Fatalf("a description quoted back scored nothing (%.3f)", score)
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
	if cues := SeedCues("   "); len(cues) != 0 {
		t.Fatalf("an empty goal seeded %q", cues)
	}
}

// ── the decision ────────────────────────────────────────────────────────────

func TestBestPicksTheHigherAndHoldsTheFloor(t *testing.T) {
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

func TestBestClearWinner(t *testing.T) {
	// One word of one entry's cue list, and nothing much for anybody else. It is
	// under the threshold and it is still the answer to the question, because
	// the only other candidate is a quarter of the scale behind it.
	alpha := Entry{Name: "alpha", Cues: []string{"pricing"}}
	beta := Entry{Name: "beta", Description: "pricing tiers and margins"}
	turn := Turn{Text: "pricing, quickly"}

	lead, trail := Score(turn, alpha), Score(turn, beta)
	if lead >= Threshold {
		t.Fatalf("the setup is wrong: %.3f already clears the threshold on its own", lead)
	}
	if lead-trail < ClearWinner {
		t.Fatalf("the setup is wrong: the lead is %.3f, under the clear-winner margin", lead-trail)
	}
	match, ok := Best(turn, []Entry{alpha, beta})
	if !ok || match.Entry.Name != "alpha" {
		t.Fatalf("the clear winner did not fire: %q at %.3f (ok=%v)", match.Entry.Name, match.Score, ok)
	}
}

func TestBestTiesDoNotFire(t *testing.T) {
	// The same evidence for two harnesses is evidence for neither. A sentence
	// that describes both equally well is a sentence nobody should be asked
	// about, so the clear-winner rule is unavailable and the threshold decides.
	left := Entry{Name: "left", Cues: []string{"pricing"}}
	right := Entry{Name: "right", Cues: []string{"pricing"}}
	turn := Turn{Text: "pricing, quickly"}
	if match, ok := Best(turn, []Entry{left, right}); ok {
		t.Fatalf("a tie fired %q at %.3f", match.Entry.Name, match.Score)
	}
}

func TestBestHoldsTheFloor(t *testing.T) {
	// A lone entry has no runner-up, so every score is a clear win — which is
	// why the floor is a separate number. Prose overlap and nothing else stays
	// under it, alone in the registry or not.
	lonely := Entry{Name: "lonely", Description: "pricing tiers, margins and discounts"}
	turn := Turn{Text: "the pricing tiers again"}
	score := Score(turn, lonely)
	if score >= Floor {
		t.Fatalf("the setup is wrong: %.3f is already over the floor", score)
	}
	if match, ok := Best(turn, []Entry{lonely}); ok {
		t.Fatalf("a match under the floor fired %q at %.3f", match.Entry.Name, match.Score)
	}
	// And the same evidence in a registry of one, once it is a real cue, does
	// fire on the clear-winner rule alone.
	cued := Entry{Name: "lonely", Cues: []string{"pricing tiers"}}
	if _, ok := Best(turn, []Entry{cued}); !ok {
		t.Fatal("the only harness in the registry, cued by the turn, did not fire")
	}
}

func TestBestIsDeterministic(t *testing.T) {
	// Ties go to the registry's own order, and the same turn asks the same
	// question every time — the property the whole no-model choice buys.
	first := Entry{Name: "first", Description: research.Description, Cues: research.Cues}
	second := Entry{Name: "second", Description: research.Description, Cues: research.Cues}
	turn := Turn{Text: "dig into this and find out what our sources say"}
	for i := 0; i < 8; i++ {
		match, ok := Best(turn, []Entry{first, second})
		if !ok || match.Entry.Name != "first" {
			t.Fatalf("run %d matched %q (ok=%v), want first", i, match.Entry.Name, ok)
		}
	}
}

// ── the seeded cues ─────────────────────────────────────────────────────────

// The three goals the design rig is measured on (cmd/harness-design), and the
// harnesses a build of each would leave behind. They are the real sentences on
// purpose: the defect this file was changed for was a real goal scoring 0.42
// against the harness that goal built.
var (
	g1 = "Give me three punchy taglines for a CLI tool that watches files."
	g2 = "Research the current state of small open-weight LLMs for coding (2026) and give me a cited summary."
	g3 = "Decide whether our Go TUI should adopt a component model like Elm or stay with ad-hoc views — " +
		"investigate both sides, argue against your own conclusion, then produce a recommendation with " +
		"the strongest counterargument addressed."

	taglines = Entry{
		Name:        "taglines",
		Description: "Write a handful of short taglines for a tool",
		Cues:        SeedCues(g1),
		Revision:    1,
	}
	weightscan = Entry{
		Name:        "weight-scan",
		Description: "Survey open-weight models and hand back a cited summary",
		Cues:        SeedCues(g2),
		Revision:    1,
	}
	tuicall = Entry{
		Name:        "tui-call",
		Description: "Weigh a design decision from both sides and recommend one, with the counterargument answered",
		Cues:        SeedCues(g3),
		Revision:    1,
	}
	built = []Entry{taglines, weightscan, tuicall}
)

func TestSeedCuesAreTheGoalsOwnWords(t *testing.T) {
	for _, goal := range []string{g1, g2, g3} {
		cues := SeedCues(goal)
		if len(cues) < seedMin || len(cues) > seedMax {
			t.Errorf("%q seeded %d cues (%q), want %d to %d", clip(goal), len(cues), cues, seedMin, seedMax)
		}
		words := tokenize(goal)
		seen := map[string]bool{}
		for _, cue := range cues {
			if seen[cue] {
				t.Errorf("%q seeded %q twice", clip(goal), cue)
			}
			seen[cue] = true
			if !holds(words, tokenize(cue)) {
				t.Errorf("%q seeded %q, which is not a run of words the goal contains", clip(goal), cue)
			}
		}
		// Same goal, same cues, every time.
		again := SeedCues(goal)
		if strings.Join(again, "|") != strings.Join(cues, "|") {
			t.Errorf("%q seeded %q and then %q", clip(goal), cues, again)
		}
	}
}

func TestSeedCuesStopAtAClause(t *testing.T) {
	// "ad-hoc views — investigate both sides" is two things being asked for, and
	// a cue that spans the dash would be a phrase nobody said.
	for _, cue := range SeedCues(g3) {
		if strings.Contains(cue, "views investigate") {
			t.Errorf("a cue crossed the dash: %q", cue)
		}
	}
	// The hyphen inside a word is not a clause break, and the words either side
	// of it stay together.
	if cues := strings.Join(SeedCues(g3), "|"); !strings.Contains(cues, "ad hoc views") {
		t.Errorf("the hyphenated phrase did not survive seeding: %q", cues)
	}
}

func TestSeededHarnessIsReachedByTheGoalThatBuiltIt(t *testing.T) {
	// The defect, as a test. Each goal, scored against the harness it built.
	for _, c := range []struct {
		goal  string
		entry Entry
	}{{g1, taglines}, {g2, weightscan}, {g3, tuicall}} {
		score := Score(Turn{Text: c.goal}, c.entry)
		if score < Threshold {
			t.Errorf("%q scored %.3f against the harness it built, under the threshold of %.2f",
				clip(c.goal), score, Threshold)
		}
		match, ok := Best(Turn{Text: c.goal}, built)
		if !ok || match.Entry.Name != c.entry.Name {
			t.Errorf("%q matched %q at %.3f (ok=%v), want %q",
				clip(c.goal), match.Entry.Name, match.Score, ok, c.entry.Name)
		}
	}
}

func TestSeededHarnessSurvivesAParaphrase(t *testing.T) {
	// The goal is typed once, at build time. What arrives afterwards is the same
	// request in a slightly different sentence, and that is what the phrase and
	// near-phrase weights are for.
	cases := []struct {
		turn string
		want string
	}{
		{"give me some punchy taglines for the cli tool that watches files", "taglines"},
		{"i need punchy taglines for a file watcher", "taglines"},
		{"research the current state of small open-weight llms and give me a cited summary", "weight-scan"},
		{"what is the current state of small open weight llms for coding?", "weight-scan"},
		{"should our go tui adopt a component model like elm, or stay with ad-hoc views?", "tui-call"},
		{"argue both sides of whether to adopt a component model in the tui, then recommend one", "tui-call"},
	}
	for _, c := range cases {
		match, ok := Best(Turn{Text: c.turn}, built)
		if !ok || match.Entry.Name != c.want {
			t.Errorf("%q matched %q at %.3f (ok=%v), want %q",
				c.turn, match.Entry.Name, match.Score, ok, c.want)
		}
	}
}

func TestOrdinaryTurnsReachNoSeededHarness(t *testing.T) {
	// The other half of the bargain. Loosening the vocabulary is only worth
	// anything if the turns that are nobody's stay nobody's.
	for _, turn := range []string{
		"what's for lunch",
		"what time is it",
		"rewrite the config loader in place",
		"why is the test suite so slow this week",
	} {
		for _, entry := range built {
			if score := Score(Turn{Text: turn}, entry); score >= Floor {
				t.Errorf("%q scored %.3f against %q, over the floor of %.2f", turn, score, entry.Name, Floor)
			}
		}
		if match, ok := Best(Turn{Text: turn}, built); ok {
			t.Errorf("%q fired %q at %.3f", turn, match.Entry.Name, match.Score)
		}
	}
}

func TestSeedCuesOfAThinGoal(t *testing.T) {
	// A goal with one phrase in it seeds one phrase and its own words, and does
	// not pad the list out to four with whatever is left. What comes back is
	// what the sentence had to give.
	cues := SeedCues("Summarize my email")
	if len(cues) == 0 || len(cues) > seedMin {
		t.Fatalf("a three-word goal seeded %d cues: %q", len(cues), cues)
	}
	entry := Entry{Name: "inbox", Description: "Read the inbox and say what is in it", Cues: cues}
	if score := Score(Turn{Text: "summarize my email"}, entry); score < Threshold {
		t.Errorf("the goal that built it scored %.3f", score)
	}
	// And two of its words, in a sentence that is not the goal, still reach it —
	// which is the whole reason a thin goal seeds single words at all.
	if score := Score(Turn{Text: "can you summarize this email thread"}, entry); score < Threshold {
		t.Errorf("a paraphrase scored %.3f", score)
	}
}

func clip(text string) string {
	if len(text) <= 48 {
		return text
	}
	return text[:48] + "…"
}
