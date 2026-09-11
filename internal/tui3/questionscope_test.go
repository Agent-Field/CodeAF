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
