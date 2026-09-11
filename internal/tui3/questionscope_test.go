package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// scopedAsk is a question that offered three lifetimes for its answer.
//
// IT IS NOT A PERMISSION, and that is the point of the fixture rather than an
// accident of it: a consent's answer is read as its key and nothing else, so the
// row is drawn for the kinds of question whose lane DOES read
// [session.Answer.Scope] ([TestAPermissionOffersNoLifetimesUntilTheGateHonoursOne]).
func scopedAsk() session.Question {
	return session.Question{
		ID: 7, Kind: session.QuestionTask, Ask: session.AskChoice,
		Form: session.FormCard, Asker: session.Asker{Kind: session.AskerModel},
		Head: "which store should the ledger sit on?", Reason: "a schema change is next",
		Options: []session.AnswerOption{
			{Key: "1", Label: "postgres", Consequence: "one place to back up"},
			{Key: "2", Label: "sqlite", Consequence: "no service to run"},
			{Key: "3", Label: "neither", Consequence: "the ledger waits", Safe: true},
		},
		Stakes:   session.StakesReversible,
		Blocking: session.Blocking{Turn: true},
		Scope:    []session.AnswerScope{session.ScopeAlways, session.ScopeOnce, session.ScopeProject},
	}
}

// "STOP ASKING ME FOR THIS" HAS A ROW ON THE FRAME (owner addendum 2026-09-11).
//
// The lifetimes the question offered are drawn under the answers, narrowest
// first whatever order the lane listed them in, with a key that cycles them.
func TestAFrameDrawsTheLifetimesTheQuestionOffers(t *testing.T) {
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
	// AND THE HAND IS ON THE QUESTION BEFORE A LETTER IS. A press with nobody on
	// the block belongs to the box — the law every printable key on this surface
	// is held to — so the walk comes first, which is also how a person reaches
	// this key: they are already looking at the answers.
	lab.press("down")
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
	q := scopedAsk()
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

// A PERMISSION OFFERS NO LIFETIMES, however many its asker listed.
//
// The gate reads a consent's answer as its key plus the banked comment and
// nothing else — [session.Answer.Scope] never reaches it — so `t → for this
// project` followed by `enter` granted exactly ConsentOnce and the very next
// call asked again. A row a person can move, that says a thing, and that changes
// nothing is worse than no row when the thing it says is about safety: "a
// capability that cannot work is absent, not broken". The row comes back to this
// frame in the same change as the engine reading the field.
func TestAPermissionOffersNoLifetimesUntilTheGateHonoursOne(t *testing.T) {
	q := consentAsk()
	q.Scope = []session.AnswerScope{session.ScopeOnce, session.ScopeProject, session.ScopeAlways}
	if got := questionScopes(q); got != nil {
		t.Fatalf("a permission offers the lifetimes %v", got)
	}
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
			t.Fatalf("a permission frame says %q:\n%s", gone, screen)
		}
	}
	// AND THE ANSWER GOES OUT AS `once`, which is what the gate would grant
	// whatever the row had said.
	if got := questionScopeOf(lab.a.questions[0], "1"); got != session.ScopeOnce {
		t.Fatalf("a permission answer would go out as %q", got)
	}
}

// WHERE THE POINTER OPENS IS NEVER READ OFF THE TOOL'S NAME, and a confirmation
// is untouched by any of it.
//
// (That `enter` on a permission denies, whatever the stakes say, is
// [TestEnterOnAPermissionDeniesUntilTheEngineGradesTheCall] in question_test.go
// — it drives the key rather than reading the placement.)
func TestThePointerIsPlacedByWhatTheQuestionIsAndNotByWhichToolItNames(t *testing.T) {
	gate := consentAsk()
	safe := questionSafeAt(gate)
	if got := questionPointerStart(gate); got != safe {
		t.Fatalf("a permission opens on answer %d, not the one that loses nothing", got)
	}
	// The same question over a different call stands in the same place.
	gate.Subject = session.SubjectRef{Kind: session.SubjectCall, Name: "read"}
	if got := questionPointerStart(gate); got != safe {
		t.Fatalf("the pointer moved when the tool's name changed: %d", got)
	}
	gate.Subject = session.SubjectRef{Kind: session.SubjectCall, Name: "bash"}
	gate.Stakes = session.StakesIrreversible
	if got := questionPointerStart(gate); got != safe {
		t.Fatalf("the pointer moved when the stakes changed: %d", got)
	}
	// AND A CONFIRMATION KEEPS stop.go's LAW whatever its stakes say, because it
	// was raised by a person's own gesture rather than arriving.
	stop := session.Question{
		ID: 9, Kind: session.QuestionTask, Ask: session.AskConfirmation,
		Asker: session.Asker{Kind: session.AskerSurface}, Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "stop it"}, {Key: "2", Label: "keep going", Safe: true}},
	}
	if got := questionPointerStart(stop); got != 1 {
		t.Fatalf("a confirmation opens on answer %d, not the answer that loses nothing", got)
	}
}
