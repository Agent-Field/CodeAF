package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// THE HARNESS OFFER, from the sides a person meets it: the row, its two keys,
// the press, and the ways it goes away.

// harnessAgent records the answers this offer sends back into the session.
type harnessAgent struct {
	*fakeAgent
	answers []harnessAnswer
}

type harnessAnswer struct {
	id    uint64
	run   bool
	model string
}

func (h *harnessAgent) ResolveHarness(id uint64, run bool, model string) {
	h.answers = append(h.answers, harnessAnswer{id: id, run: run, model: model})
}

// ResolveQuestion is THE ONE DOOR every answer on the block goes through
// ([resolveThroughLanes] says why a fake needs it).
func (h *harnessAgent) ResolveQuestion(answer session.Answer) error {
	return resolveThroughLanes(h, answer)
}

func harnessOffered(t *testing.T, turn string) (*harnessAgent, *app) {
	t.Helper()
	return harnessOfferedWith(t, turn, session.Event{
		Kind: session.EventHarnessOffer, ID: 3, Text: "research",
		Hint: "Research a question across sources and write a report",
	})
}

// harnessOfferedWith is the same offer with the card's own fields said out
// loud, for the rows that are about what the offer CARRIED rather than about
// the question itself.
func harnessOfferedWith(t *testing.T, turn string, offer session.Event) (*harnessAgent, *app) {
	t.Helper()
	agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{offer}}}}
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

	// The line form: head and answers on one row, the reason under it.
	a.width = 120
	if got := len(a.questionRows(a.width)); got != 2 {
		t.Fatalf("the offer took %d rows, want the question and its reason:\n%s",
			got, plain(strings.Join(a.questionRows(a.width), "\n")))
	}
	got := plain(frame(a))
	for _, want := range []string{
		`run harness "research"`, // the whole question
		"[1] run it",
		"[2] not now",
		"[esc] later",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the offer is missing %q:\n%s", want, got)
		}
	}
	// A frame with room says what the harness is FOR; the narrow one below is
	// the same question with the reason cut, never with an answer dropped.
	a.width = 120
	if wide := plain(frame(a)); !strings.Contains(wide, "Research a question across sources") {
		t.Fatalf("the wide row does not say what the harness does:\n%s", wide)
	}
	// AND A FRAME WITH NO ROOM FOR ONE ROW PROMOTES RATHER THAN CUTTING AN
	// ANSWER: the card gives each answer a row of its own, which is the floor
	// moving up under a narrow terminal (question.go's questionLineRows).
	a.width = 60
	narrow := plain(frame(a))
	for _, want := range []string{"1  run it", "2  not now"} {
		if !strings.Contains(narrow, want) {
			t.Fatalf("the narrow form lost %q:\n%s", want, narrow)
		}
	}
}

// THE TWO ANSWERS, in the digits every question on this surface takes.
//
// `enter`, `y`, `esc` and `n` used to be the four keys here, and none of them is
// an answer any more: this lane answers `1` and `2` like every other question on
// the block, and `esc` is *later* rather than the no
// (docs/design/questions/DESIGN.md's ONE KEY GRAMMAR).
func TestTheHarnessOfferAnswersToItsKeys(t *testing.T) {
	for _, c := range []struct {
		key string
		run bool
	}{{"1", true}, {"2", false}} {
		agent, a := harnessOffered(t, "research the pricing tiers")
		harnessSettled(t, a)
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

	// AND esc IS LATER AND ANSWERS NOTHING. The offer stays open, the turn stays
	// held, and the chip goes on counting it.
	agent, a := harnessOffered(t, "research the pricing tiers")
	harnessSettled(t, a)
	drive(t, a, key("esc"))
	if len(agent.answers) != 0 {
		t.Fatalf("esc answered the offer: %+v", agent.answers)
	}
	if !a.asksHarness() || a.questionCount() != 1 {
		t.Fatalf("esc took the offer away rather than putting it off")
	}
}

// AND IT DOES NOT SUSPEND THE DRAFT. The block is never modal: the box under an
// offer is live, the letters are the person's, and the question waits.
//
// This is a change. The row used to own the keyboard while it was up — every key
// that was not one of its four did nothing — which was the honest design while
// `esc` was the only way out and the only way out was an answer. `esc` is later
// now, so there is a way out that neither answers nor traps (question.go's THE
// NEVER MODAL law).
func TestTheHarnessOfferLeavesTheDraftAlone(t *testing.T) {
	agent, a := harnessOffered(t, "research the pricing tiers")
	harnessSettled(t, a)
	drive(t, a, key("k"), key("z"))
	if got := a.input.String(); got != "kz" {
		t.Fatalf("the box holds %q, want the letters that were typed", got)
	}
	if len(agent.answers) != 0 {
		t.Fatalf("an ordinary key answered the offer: %+v", agent.answers)
	}
	// AND WITH WORDS IN THE BOX THE DIGITS ARE TEXT. A question the turn is
	// waiting on still leaves a half-typed sentence alone.
	drive(t, a, key("1"))
	if len(agent.answers) != 0 {
		t.Fatalf("a digit answered over a half-typed sentence: %+v", agent.answers)
	}
	if got := a.input.String(); got != "kz1" {
		t.Fatalf("the box holds %q", got)
	}
}

// AND IT IS A BUTTON. Every answer on the row is a target, and a press that
// missed them all is swallowed rather than falling through to the transcript.
func TestTheHarnessOfferAnswersToThePointer(t *testing.T) {
	agent, a := harnessOffered(t, "research the pricing tiers")
	// The LINE form, which is what an offer is on a frame with room for one row
	// — its two answers are spans on that row rather than rows of their own.
	a.width = 120
	harnessSettled(t, a)
	y := harnessRowY(t, a)
	if len(a.questionSpans) != 2 {
		t.Fatalf("the row recorded %d targets, want two", len(a.questionSpans))
	}
	at := a.questionSpans[0]
	drive(t, a, tea.MouseClickMsg{X: at.from + 1, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: at.from + 1, Y: y, Button: tea.MouseLeft})
	if len(agent.answers) != 1 || !agent.answers[0].run {
		t.Fatalf("the press answered %+v, want a run", agent.answers)
	}
}

// THE TURN ENDING TAKES IT AWAY. The session already released the turn; a
// question left on screen would be asking about work that is over — and it says
// so in one dim line rather than simply vanishing.
func TestTheHarnessOfferDiesWithTheTurn(t *testing.T) {
	agent, a := harnessOffered(t, "research the pricing tiers")
	harnessSettled(t, a)
	a.settle()
	if a.asksHarness() {
		t.Fatal("the offer outlived the turn")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("a dropped offer answered the session: %+v", agent.answers)
	}
	a.width = 160
	if got := plain(strings.Join(a.questionRows(a.width), "\n")); !strings.Contains(got, harnessOfferGoneWord) {
		t.Fatalf("the withdrawn offer said nothing:\n%s", got)
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

// ── the model the turn named ────────────────────────────────────────────────

// THE ROW SAYS WHAT YES WOULD RUN ON. A turn that named a model — "research the
// pricing tiers with opus" — is answered by a card that says so, because the
// question "run this harness?" is a different question when the answer costs
// what opus costs.
func TestTheHarnessOfferSaysWhichModelTheTurnNamed(t *testing.T) {
	agent, a := harnessOfferedWith(t, "research the pricing tiers with opus", session.Event{
		Kind: session.EventHarnessOffer, ID: 3, Text: "research",
		Hint:  "Research a question across sources and write a report",
		Model: "anthropic/claude-opus-5",
	})
	a.width = 120
	got := plain(frame(a))
	if !strings.Contains(got, "model: anthropic/claude-opus-5") {
		t.Fatalf("the row does not say what it would run on:\n%s", got)
	}
	// And the model the row SHOWED is what goes back with the answer.
	harnessSettled(t, a)
	drive(t, a, key("1"))
	if len(agent.answers) != 1 || agent.answers[0].model != "anthropic/claude-opus-5" {
		t.Fatalf("the answer carried %+v", agent.answers)
	}
}

// AN OFFER THAT NAMED NO MODEL SAYS NOTHING ABOUT MODELS. Nothing about the
// model is unusual, so there is nothing on the row about it — and the answer
// carries nothing either, which the session reads as "the one the offer had".
func TestTheHarnessOfferIsSilentAboutAnOrdinaryModel(t *testing.T) {
	agent, a := harnessOffered(t, "research the pricing tiers")
	a.width = 120
	if got := plain(frame(a)); strings.Contains(got, "model:") {
		t.Fatalf("the row invented a model line:\n%s", got)
	}
	harnessSettled(t, a)
	drive(t, a, key("1"))
	if len(agent.answers) != 1 || agent.answers[0].model != "" {
		t.Fatalf("the answer carried %+v", agent.answers)
	}
}

// A MODEL THIS INSTALL DOES NOT HAVE IS SAID, NOT SWALLOWED. The session's own
// words are printed as they arrived, and the question is the same question: yes
// still runs the harness, on the default.
func TestTheHarnessOfferPrintsTheSessionsModelNote(t *testing.T) {
	agent, a := harnessOfferedWith(t, "research the pricing tiers with gpt-9", session.Event{
		Kind: session.EventHarnessOffer, ID: 3, Text: "research",
		ModelNote: `model "gpt-9" not found, running default`,
	})
	a.width = 120
	got := plain(frame(a))
	if !strings.Contains(got, `model "gpt-9" not found, running default`) {
		t.Fatalf("the note never reached the row:\n%s", got)
	}
	if !strings.Contains(got, "[1] run it") {
		t.Fatalf("the note cost the row an answer:\n%s", got)
	}
	harnessSettled(t, a)
	drive(t, a, key("1"))
	if len(agent.answers) != 1 || !agent.answers[0].run {
		t.Fatalf("a noted model changed the answer: %+v", agent.answers)
	}
}

// AND THE MODEL OUTLIVES THE DESCRIPTION. A narrow reason loses what the harness
// is FOR before it loses what this run would cost, which is why the model is
// spelled first ([session.HarnessQuestion] says so), and it never costs a key.
func TestTheNarrowHarnessRowKeepsTheModelAndLosesTheDescription(t *testing.T) {
	_, a := harnessOfferedWith(t, "research the pricing tiers with opus", session.Event{
		Kind: session.EventHarnessOffer, ID: 3, Text: "research",
		Hint:  "Research a question across sources and write a report",
		Model: "anthropic/claude-opus-5",
	})
	// Wide enough for the question, the model and both keys — and short of the
	// description, which is the rung this reason has to choose at.
	a.width = 60
	narrow := plain(frame(a))
	if strings.Contains(narrow, "Research a question across sources and write a report") {
		t.Fatalf("the narrow row kept the description:\n%s", narrow)
	}
	if !strings.Contains(narrow, "model: anthropic/claude-opus-5") {
		t.Fatalf("the narrow row dropped the model before the description:\n%s", narrow)
	}
	for _, want := range []string{"1  run it", "2  not now"} {
		if !strings.Contains(narrow, want) {
			t.Fatalf("the narrow form lost %q:\n%s", want, narrow)
		}
	}
}

// harnessSettled puts the offer past the settle guard, which is where a person
// is before their first key: drawn, and [questionSettle] gone by
// (question.go's THE SETTLE GUARD).
func harnessSettled(t *testing.T, a *app) {
	t.Helper()
	// The DRAW is what stamps a question as seen ([app.markQuestionShown]), and
	// the stamp is what the guard is measured from — so the stamp is walked back
	// rather than the clock forward, which leaves every other clock on this
	// surface where the test put it.
	_ = a.questionRows(a.width)
	for i := range a.questions {
		a.questions[i].shown = a.questions[i].shown.Add(-questionSettle - time.Millisecond)
	}
}

// harnessRowY is the screen row the offer is drawn on, derived the way
// [app.chromeAt] derives it backwards so the test and the surface cannot
// disagree about where the row is.
func harnessRowY(t *testing.T, a *app) int {
	t.Helper()
	_, marks, _, _ := a.chrome(a.width)
	for at, mark := range marks {
		if mark.kind == chromeQuestion && mark.index == a.questionSpanRow {
			return at + a.height - len(marks)
		}
	}
	t.Fatalf("no row is marked as the offer's answers")
	return -1
}

// ── the steps of a run, while it runs ───────────────────────────────────────

func harnessStepEvent(step int, id, kind, out, err string) session.Event {
	trail := subharness.Trail{Step: step, Id: id, Kind: kind, Out: out, Err: err}
	return session.Event{Kind: session.EventHarnessStep, ID: 9, Text: "research", Step: &trail}
}

// A RUN IS SOMETHING YOU CAN WATCH. Between the announcement and the report was
// nothing at all, on a run that takes minutes; each step now shows itself as it
// lands, on the row the ellipsis would otherwise be pulsing on.
func TestAHarnessRunShowsEachStepAsItLands(t *testing.T) {
	agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventHarnessRun, ID: 9, Text: "research"},
		harnessStepEvent(1, "gather", "agent.loop", "read the changelog", ""),
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, "research the pricing tiers")

	got := plain(frame(a))
	if !strings.Contains(got, "harness · research") {
		t.Fatalf("the run was never announced:\n%s", got)
	}
	if !strings.Contains(got, "gather") || !strings.Contains(got, "read the changelog") {
		t.Fatalf("the step never showed itself:\n%s", got)
	}
}

// THE ROW IS REPLACED, NEVER STACKED. The report that follows carries the whole
// trail, so a run that left every step in the feed would write that trail twice.
func TestAHarnessRunsStepRowReplacesItself(t *testing.T) {
	agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventHarnessRun, ID: 9, Text: "research"},
		harnessStepEvent(1, "gather", "agent.loop", "read the changelog", ""),
		harnessStepEvent(2, "check", "verify", "the numbers agree", ""),
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, "research the pricing tiers")

	got := plain(frame(a))
	if strings.Contains(got, "gather") {
		t.Fatalf("the first step is still on screen beside the second:\n%s", got)
	}
	if !strings.Contains(got, "check") || !strings.Contains(got, "the numbers agree") {
		t.Fatalf("the second step is not the row:\n%s", got)
	}
	// It is display-only: nothing about a step reaches the conversation, which is
	// what keeps it out of the journal and out of every later request.
	for _, e := range a.entries {
		if strings.Contains(e.text, "the numbers agree") {
			t.Fatalf("a step became an entry: %+v", e)
		}
	}
}

// A FAILING STEP IS THE ONE MOST WORTH SEEING, so its error is what the row
// says — the run is about to end on it.
func TestAHarnessRunsStepRowSaysWhatWentWrong(t *testing.T) {
	agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventHarnessRun, ID: 9, Text: "research"},
		harnessStepEvent(2, "check", "verify", "", "the diff did not apply"),
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, "research the pricing tiers")

	if got := plain(frame(a)); !strings.Contains(got, "the diff did not apply") {
		t.Fatalf("the failing step said nothing:\n%s", got)
	}
}

// AND THE ROW GOES WHEN THE TURN DOES. The report is on screen by then with
// every step on it, and a live row about finished work is a row that lies.
func TestAHarnessRunsStepRowLeavesWithTheTurn(t *testing.T) {
	agent := &harnessAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventHarnessRun, ID: 9, Text: "research"},
		harnessStepEvent(1, "gather", "agent.loop", "read the changelog", ""),
		{Kind: session.EventTextDelta, Text: "the report"},
		{Kind: session.EventTurnDone},
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, "research the pricing tiers")

	got := plain(frame(a))
	if strings.Contains(got, "read the changelog") {
		t.Fatalf("the live step outlived its turn:\n%s", got)
	}
	if !strings.Contains(got, "the report") {
		t.Fatalf("the report did not land:\n%s", got)
	}
}
