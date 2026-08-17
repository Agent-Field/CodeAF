package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE HARNESS OFFER, from the sides a person meets it: the row, its two keys,
// the press, and the ways it goes away.

// harnessAgent records the answers this offer sends back into the session.
type harnessAgent struct {
	*fakeAgent
	answers []harnessAnswer
}

type harnessAnswer struct {
	id  uint64
	run bool
}

func (h *harnessAgent) ResolveHarness(id uint64, run bool) {
	h.answers = append(h.answers, harnessAnswer{id: id, run: run})
}

func harnessOffered(t *testing.T, turn string) (*harnessAgent, *app) {
	t.Helper()
	agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		{
			Kind: session.EventHarnessOffer, ID: 3, Text: "research",
			Hint: "Research a question across sources and write a report",
		},
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, turn)
	if !a.asksHarness() {
		t.Fatalf("no offer is up:\n%s", plain(frame(a)))
	}
	return agent, a
}

// ONE ROW, and it says the harness's name and both answers.
func TestTheHarnessOfferIsOneRowWithBothAnswers(t *testing.T) {
	_, a := harnessOffered(t, "research the pricing tiers")

	if got := a.harnessAskHeight(); got != 1 {
		t.Fatalf("the offer took %d rows, want one", got)
	}
	got := plain(frame(a))
	for _, want := range []string{
		`run harness "research"?`, // the whole question
		"[enter] run",
		"[esc] no",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the offer is missing %q:\n%s", want, got)
		}
	}
	// A frame with room says what the harness is FOR; the narrow one above is
	// the same row with the description dropped, never with an answer cut off.
	a.width = 120
	if wide := plain(frame(a)); !strings.Contains(wide, "Research a question across sources") {
		t.Fatalf("the wide row does not say what the harness does:\n%s", wide)
	}
	a.width = 60
	narrow := plain(frame(a))
	if strings.Contains(narrow, "Research a question across sources") {
		t.Fatalf("the narrow row kept the description:\n%s", narrow)
	}
	if !strings.Contains(narrow, "[enter] run") || !strings.Contains(narrow, "[esc] no") {
		t.Fatalf("the narrow row lost an answer:\n%s", narrow)
	}
}

// THE TWO ANSWERS, in the four keys they are given by.
func TestTheHarnessOfferAnswersToItsKeys(t *testing.T) {
	for _, c := range []struct {
		key string
		run bool
	}{{"enter", true}, {"y", true}, {"esc", false}, {"n", false}} {
		agent, a := harnessOffered(t, "research the pricing tiers")
		drive(t, a, key(c.key))
		if len(agent.answers) != 1 {
			t.Fatalf("%s sent %d answers", c.key, len(agent.answers))
		}
		if got := agent.answers[0]; got.id != 3 || got.run != c.run {
			t.Fatalf("%s answered %+v, want id 3 run=%v", c.key, got, c.run)
		}
		if a.asksHarness() {
			t.Fatalf("%s left the offer up", c.key)
		}
	}
}

// WHILE IT IS UP IT OWNS THE KEYBOARD. A key that is not an answer does
// nothing — it does not type into a conversation that cannot move.
func TestTheHarnessOfferSuspendsTheDraft(t *testing.T) {
	agent, a := harnessOffered(t, "research the pricing tiers")
	before := a.input.String()
	drive(t, a, key("k"), key("z"))
	if got := a.input.String(); got != before {
		t.Fatalf("the draft took %q while a question was up", got)
	}
	if len(agent.answers) != 0 {
		t.Fatalf("an ordinary key answered the offer: %+v", agent.answers)
	}
}

// AND IT IS A BUTTON. The row's two answers are targets, and a press anywhere
// else on the row is swallowed rather than falling through to the transcript.
func TestTheHarnessOfferAnswersToThePointer(t *testing.T) {
	agent, a := harnessOffered(t, "research the pricing tiers")
	y := harnessRowY(t, a)
	if len(a.harnessTaps) != 2 {
		t.Fatalf("the row recorded %d targets, want two", len(a.harnessTaps))
	}
	at := a.harnessTaps[0]
	drive(t, a, tea.MouseClickMsg{X: at.span.from + 1, Y: y, Button: tea.MouseLeft})
	if len(agent.answers) != 1 || !agent.answers[0].run {
		t.Fatalf("the press answered %+v, want a run", agent.answers)
	}
}

// THE TURN ENDING TAKES IT AWAY. The session already released the turn; a row
// left on screen would be asking about work that is over.
func TestTheHarnessOfferDiesWithTheTurn(t *testing.T) {
	agent, a := harnessOffered(t, "research the pricing tiers")
	a.settle()
	if a.asksHarness() {
		t.Fatal("the offer outlived the turn")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("a dropped offer answered the session: %+v", agent.answers)
	}
}

// AND THE RUN IS A NOTE. The person said yes; what follows is the harness's
// report as ordinary text, so the start of it is one dim line.
func TestTheHarnessRunIsANote(t *testing.T) {
	agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventHarnessRun, Text: "research"},
		{Kind: session.EventTextDelta, Text: "the report"},
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, "research the pricing tiers")

	got := plain(frame(a))
	if !strings.Contains(got, "harness · research") {
		t.Fatalf("the run was never drawn:\n%s", got)
	}
	if !strings.Contains(got, "the report") {
		t.Fatalf("the harness's report is missing:\n%s", got)
	}
}

// harnessRowY is the screen row the offer is drawn on, derived the way
// [app.chromeAt] derives it backwards so the test and the surface cannot
// disagree about where the row is.
func harnessRowY(t *testing.T, a *app) int {
	t.Helper()
	_, marks, _, _ := a.chrome(a.width)
	for at, mark := range marks {
		if mark.kind == chromeHarnessAsk {
			return at + a.height - len(marks)
		}
	}
	t.Fatalf("no row is marked as the harness offer")
	return -1
}
