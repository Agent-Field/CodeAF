package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// scopedAsk is a permission that offered three lifetimes for its answer.
func scopedAsk() session.Question {
	q := consentAsk()
	q.Scope = []session.AnswerScope{session.ScopeAlways, session.ScopeOnce, session.ScopeProject}
	return q
}

// "STOP ASKING ME FOR THIS" HAS A ROW ON THE FRAME (owner addendum 2026-09-11).
//
// The lifetimes the question offered are drawn under the answers, narrowest
// first whatever order the lane listed them in, with a key that cycles them.
func TestThePermissionFrameDrawsTheLifetimesItOffers(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 110
	lab.raise(scopedAsk())
	screen := lab.plain()
	for _, want := range []string{
		questionScopeWord(session.ScopeOnce),
		questionScopeWord(session.ScopeProject),
		questionScopeWord(session.ScopeAlways),
		questionScopeKey + " how long",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the frame does not say %q:\n%s", want, screen)
		}
	}
	// NARROWEST FIRST, and the narrowest is what it opens on.
	once := strings.Index(screen, questionScopeWord(session.ScopeOnce))
	project := strings.Index(screen, questionScopeWord(session.ScopeProject))
	always := strings.Index(screen, questionScopeWord(session.ScopeAlways))
	if !(once < project && project < always) {
		t.Fatalf("the lifetimes are not drawn narrowest first:\n%s", screen)
	}
	if got := questionScopeNow(lab.a.questions[0]); got != session.ScopeOnce {
		t.Fatalf("the row opened on %q, not the narrowest lifetime", got)
	}
}

// AND THE KEY CYCLES IT, AND THE ANSWER CARRIES WHAT THE ROW SAYS.
func TestTheLifetimeKeyCyclesAndTheAnswerCarriesIt(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 110
	lab.raise(scopedAsk())
	// The settle guard is spent first: a key that lands before the frame has
	// been on screen long enough was aimed at whatever was there before it.
	lab.tick(questionSettle)
	lab.press(questionScopeKey)
	if got := questionScopeNow(lab.a.questions[0]); got != session.ScopeProject {
		t.Fatalf("one press reached %q, not `for this project`", got)
	}
	// The mark moved with it: the tick stands on the lifetime that is on.
	if tick := lab.a.icon(tokens.GSettled); !strings.Contains(lab.plain(),
		tick+" "+questionScopeWord(session.ScopeProject)) {
		t.Fatalf("the tick did not move onto the lifetime that is on:\n%s", lab.plain())
	}
	// AND THE ANSWER GOES OUT WITH IT. `deny` is an ordinary answer, so before
	// this row it could only ever have carried `once`.
	if got := questionScopeOf(lab.a.questions[0], "3"); got != session.ScopeProject {
		t.Fatalf("the answer would go out as %q, not what the row says", got)
	}
	// And round it goes.
	lab.press(questionScopeKey)
	lab.press(questionScopeKey)
	if got := questionScopeNow(lab.a.questions[0]); got != session.ScopeOnce {
		t.Fatalf("three presses did not come back round to `just this once`: %q", got)
	}
}

// AND AN IRREVERSIBLE QUESTION OFFERS NO LIFETIME AT ALL. A call that cannot be
// taken back is asked about every time, and a row offering to stop asking would
// be this surface selling the one guarantee it has.
func TestAnIrreversibleQuestionOffersNoLifetimeRow(t *testing.T) {
	q := scopedAsk()
	q.Stakes = session.StakesIrreversible
	lab := newQuestionLab(t)
	lab.a.width = 110
	lab.raise(q)
	screen := lab.plain()
	for _, gone := range []string{
		questionScopeWord(session.ScopeProject),
		questionScopeWord(session.ScopeAlways),
		questionScopeKey + " how long",
	} {
		if strings.Contains(screen, gone) {
			t.Fatalf("an irreversible question offers %q:\n%s", gone, screen)
		}
	}
	if got := questionScopeOf(lab.a.questions[0], "3"); got != session.ScopeOnce {
		t.Fatalf("an irreversible answer would go out as %q", got)
	}
}

// AND A QUESTION THAT OFFERED ONE LIFETIME DRAWS NO ROW, which is the emptiness
// law over a toggle: a choice with one answer is not a choice.
func TestAQuestionWithOneLifetimeDrawsNoRow(t *testing.T) {
	q := consentAsk()
	q.Scope = []session.AnswerScope{session.ScopeOnce}
	lab := newQuestionLab(t)
	lab.a.width = 110
	lab.raise(q)
	if screen := lab.plain(); strings.Contains(screen, questionScopeKey+" how long") {
		t.Fatalf("a question with one lifetime offers the key:\n%s", screen)
	}
}

// AND THE DIGITS ROW NAMES NO KEY NOTHING ANSWERS. An irreversible permission is
// the ordinary shape this happens on: the engine drops the widening answer from
// a gate it may not offer one on, leaving `1` and `3`, and a row that said `1–3`
// would be offering a key that does nothing.
func TestTheDigitsRowOnlyDrawsARangeWhereTheKeysRun(t *testing.T) {
	q := consentAsk()
	q.Stakes = session.StakesIrreversible
	kept := q.Options[:0:0]
	for _, option := range q.Options {
		if option.Widening {
			continue
		}
		kept = append(kept, option)
	}
	q.Options = kept
	if got := questionDigitsWord(q); got != "1 3" {
		t.Fatalf("the digits row says %q over the answers 1 and 3", got)
	}
	if got := questionDigitsWord(consentAsk()); got != "1–3" {
		t.Fatalf("three answers that run are not drawn as a range: %q", got)
	}
}

// WHERE THE POINTER OPENS ON A PERMISSION IS DECIDED BY THE STAKES, and by
// nothing else (owner ruling 2026-09-11, consent pick B).
//
// An irreversible call opens on the answer that loses nothing, because `enter`
// takes what the pointer is on and that call cannot be taken back. An ordinary
// one opens on `allow once`: every gate opened on deny for a year, including the
// ones over a command the rules had merely not seen before, so the key a person
// presses to get on with their work was the key that stopped it.
func TestWhereThePermissionPointerOpensIsDecidedByTheStakes(t *testing.T) {
	ordinary := consentAsk()
	if got := questionPointerStart(ordinary); got != 0 {
		t.Fatalf("an ordinary permission opens on answer %d, not `allow once`", got)
	}
	grave := consentAsk()
	grave.Stakes = session.StakesIrreversible
	if got := questionPointerStart(grave); got != questionSafeAt(grave) {
		t.Fatalf("an irreversible permission opens on answer %d, not the answer that loses nothing", got)
	}
	// AND THE TOOL'S NAME IS NEVER READ. The same stakes over a different call
	// stand in the same place.
	grave.Subject = session.SubjectRef{Kind: session.SubjectCall, Name: "read"}
	if got := questionPointerStart(grave); got != questionSafeAt(grave) {
		t.Fatalf("the pointer moved when the tool's name changed: %d", got)
	}
	// AND A CONFIRMATION IS UNTOUCHED: it keeps stop.go's law whatever its
	// stakes say, because it was raised by a person's own gesture.
	stop := session.Question{
		ID: 9, Kind: session.QuestionTask, Ask: session.AskConfirmation,
		Asker: session.Asker{Kind: session.AskerSurface}, Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "stop it"}, {Key: "2", Label: "keep going", Safe: true}},
	}
	if got := questionPointerStart(stop); got != 1 {
		t.Fatalf("a confirmation opens on answer %d, not the answer that loses nothing", got)
	}
}

// THE FOLD RULE PRINTS THE KEY THAT OPENS IT, AND THE KEY OPENS IT.
//
// `── ? which store · 3 answers · ◆ SQLite ──── space open ──` is what `esc`
// leaves behind, and the manual says the same thing in a person's words — and
// nothing routed the press. Found by lane T driving the real binary: esc, then
// space over the empty box, and the rule stayed folded.
func TestSpaceOpensAQuestionFoldedToItsRule(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 110
	lab.raise(scopedAsk())
	lab.tick(questionSettle)
	if !lab.press(questionLaterKey) {
		t.Fatal("esc did not fold the question")
	}
	if _, folded := lab.a.questionPutOff(); !folded {
		t.Fatal("esc left nothing folded")
	}
	if screen := lab.plain(); !strings.Contains(screen, questionOpenFoldWord) {
		t.Fatalf("the fold rule does not print the key that opens it:\n%s", screen)
	}
	if !lab.press(questionToggleKey) {
		t.Fatal("space over an empty box was not taken by the fold rule")
	}
	if _, folded := lab.a.questionPutOff(); folded {
		t.Fatal("space did not open the folded question")
	}
	if screen := lab.plain(); !strings.Contains(screen, "allow once") {
		t.Fatalf("the question did not come back:\n%s", screen)
	}
}

// AND A SPACE INSIDE A SENTENCE IS A SPACE. With words in the box the key
// belongs to the composer, which is the law every printable key here is held to.
func TestSpaceWithWordsInTheBoxLeavesTheFoldAlone(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 110
	lab.raise(scopedAsk())
	lab.tick(questionSettle)
	lab.press(questionLaterKey)
	lab.a.input.setText("a half-typed sentence")
	if lab.press(questionToggleKey) {
		t.Fatal("space was taken from the composer")
	}
	if _, folded := lab.a.questionPutOff(); !folded {
		t.Fatal("the question was opened by a key that belonged to the box")
	}
}

// AND THE PANEL'S RECOMMENDED ROW JOINS THE ASKER'S SENTENCE ON ONCE. A model
// asked what would change its mind answers "If the index has to be read from
// another machine", and the row drew `would switch if if the index has…` — the
// defect the room fixed on 2026-09-10, arriving here the day the panel started
// drawing the same sentence. Also found by lane T, driving.
func TestTheRecommendedRowSaysIfOnlyOnce(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 120
	q := session.Question{
		ID: 86, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "which store?",
		Reason: "three ways work", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{
			{Key: "1", Label: "SQLite", Consequence: "one file"},
			{Key: "2", Label: "JSONL", Consequence: "append-only"},
		},
		Pick: &session.Pick{
			Key: "1", Reason: "it survives a crash mid-write",
			WouldChange: "If the index has to be read from another machine",
		},
	}
	lab.raise(q)
	screen := lab.plain()
	if strings.Contains(screen, "if if") || strings.Contains(screen, "if If") {
		t.Fatalf("the recommended row says `if` twice:\n%s", screen)
	}
	if !strings.Contains(screen, questionWouldSwitchWord+"the index has to be read") {
		t.Fatalf("the asker's sentence does not join onto the clause:\n%s", screen)
	}
}
