package tui3

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/session"
)

// offerAgentDouble is an engine that can be asked, and remembers what it was
// asked. It embeds the ordinary double so that only the one new door is added.
type offerAgentDouble struct {
	*fakeAgent
	answers []bool
}

func (a *offerAgentDouble) AnswerLaneOffer(yes bool) bool {
	a.answers = append(a.answers, yes)
	return true
}

// offerApp is a conversation with a question standing over its own model.
func offerApp(t *testing.T, now time.Time) (*app, *offerAgentDouble) {
	t.Helper()
	t.Cleanup(forgetPhases)
	engine := &offerAgentDouble{fakeAgent: &fakeAgent{model: phaseModel}}
	a := newTestApp(engine)
	a.model = phaseModel
	a.state = stateWorking
	a.clock = func() time.Time { return now }
	PostPhaseNews(PhaseNews{
		Phase:  session.PhaseAsking,
		Since:  now.Add(-10 * time.Second),
		Detail: "coreweave is slow",
		Then:   "auto",
		Model:  phaseModel,
		Role:   lane.RoleTalk,
		At:     now,
	})
	return a, engine
}

// TestThePhaseClockAsksTheQuestionAndReportsAWaitNothingCanEnd is the two new
// sentences, spelled exactly as a person reads them.
//
// The first is a pin that has gone quiet and the one key that ends the wait; the
// second is the visible half of a report — every reachable lane is believed
// slow, so acting buys nothing and saying so IS the act.
func TestThePhaseClockAsksTheQuestionAndReportsAWaitNothingCanEnd(t *testing.T) {
	t.Cleanup(forgetPhases)
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	for _, c := range []struct {
		what string
		news PhaseNews
		want string
	}{{
		what: "a pinned lane that has gone quiet",
		news: PhaseNews{
			Phase: session.PhaseAsking, Since: now.Add(-10 * time.Second),
			Detail: "coreweave is slow", Then: "auto",
		},
		want: "coreweave is slow · switch to auto? (y)",
	}, {
		what: "a wait with nowhere better to go",
		news: PhaseNews{Phase: session.PhaseAllSlow, Since: now.Add(-12 * time.Second)},
		want: "all hosts slow · still waiting · 12s",
	}} {
		news := c.news
		news.Model, news.Role, news.At = phaseModel, lane.RoleTalk, now
		PostPhaseNews(news)
		live, ok := phaseNewsFor(phaseModel)
		if !ok {
			t.Fatalf("%s: the desk kept nothing", c.what)
		}
		if got := phaseWords(live, now); got != c.want {
			t.Fatalf("%s reads %q, want %q", c.what, got, c.want)
		}
	}
}

// TestTheOfferKeyIsIgnoredWhileSomebodyIsTyping is the guard that makes a letter
// safe to take at all.
//
// A person with a half-written sentence in the box is writing their next
// message, not answering a question, and a surface that stole the `y` out of the
// middle of a word would be worse than the wait it was ending.
func TestTheOfferKeyIsIgnoredWhileSomebodyIsTyping(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	a, engine := offerApp(t, now)
	a.input.value = []rune("yes, that reads well")

	if a.offerKey(tea.KeyPressMsg{Code: 'y'}) {
		t.Fatal("the offer took a letter out of a sentence somebody was writing")
	}
	if len(engine.answers) != 0 {
		t.Fatalf("the engine was answered %v while the box had text in it", engine.answers)
	}
}

// TestTheOfferKeyIsTakenWhileTheBoxIsEmpty is the other half: an empty box under
// a standing question is a person answering it.
func TestTheOfferKeyIsTakenWhileTheBoxIsEmpty(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	a, engine := offerApp(t, now)

	if !a.offerKey(tea.KeyPressMsg{Code: 'y'}) {
		t.Fatal("a standing question over an empty box did not take its own key")
	}
	if len(engine.answers) != 1 || !engine.answers[0] {
		t.Fatalf("the engine was answered %v, want one yes", engine.answers)
	}
	// AND EVERY OTHER LETTER IS STILL A LETTER.
	if a.offerKey(tea.KeyPressMsg{Code: 'n'}) {
		t.Error("the offer claimed a key that is not its own")
	}
}

// TestNoQuestionMeansTheLetterTypes is the third guard, and the one a person
// would notice most: with nothing being asked, `y` is a `y`.
func TestNoQuestionMeansTheLetterTypes(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	a, engine := offerApp(t, now)
	forgetPhases()

	if a.offerKey(tea.KeyPressMsg{Code: 'y'}) {
		t.Fatal("a `y` was swallowed with no question on the screen")
	}
	if len(engine.answers) != 0 {
		t.Fatalf("the engine was answered %v with no question standing", engine.answers)
	}
}

// TestAQuestionThatHasLapsedIsNotAnswered holds the window's half of the
// bargain: a question about a request that is over never sits on a screen, and
// must not be answerable either.
func TestAQuestionThatHasLapsedIsNotAnswered(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	a, engine := offerApp(t, now)
	a.clock = func() time.Time { return now.Add(phaseWindow + time.Second) }

	if a.offerKey(tea.KeyPressMsg{Code: 'y'}) {
		t.Fatal("an offer nobody can see any more still took a key")
	}
	if len(engine.answers) != 0 {
		t.Fatalf("a lapsed offer was answered %v", engine.answers)
	}
}
