package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// THE DESIGN CARD, from the sides a person meets it: the page drawn above the
// question, the two keys that answer it, and the one way it differs from the
// offer above it — it does not die with a turn, because it was never about one.

// designingAgent is a session that designs harnesses: the offer's answers, plus
// the standing lane a design arrives on.
type designingAgent struct {
	*fakeAgent
	answers []harnessAnswer
	lane    chan session.Event
}

func (d *designingAgent) ResolveHarness(id uint64, run bool, model string) {
	d.answers = append(d.answers, harnessAnswer{id: id, run: run, model: model})
}

func (d *designingAgent) HarnessDesigns() <-chan session.Event { return d.lane }

// designedPage is what a designer came back with: two steps, a check, and the
// bounds a person is really approving.
func designedPage() subharness.Harness {
	return subharness.Harness{
		Id: subharness.Id{Name: "flake-triage", Desc: "chase a flaky test to a fix"},
		Program: subharness.Program{
			Nodes: []subharness.Node{
				{Id: "look", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
					"brief": "read the failing test and say what it does", "tools": "read", "max_turns": "3",
				}},
				{Id: "check", Kind: subharness.KindVerify, Fields: subharness.Fields{
					"ladder": subharness.VerifyAccept, "check": "the report names the failing test",
				}},
			},
			Edges: []subharness.Edge{{"look", "check"}},
		},
		Whitelist: []string{"read"},
		Verify:    subharness.Verify{Ladder: subharness.VerifyAccept},
		Dyn:       subharness.Dyn{Ladder: subharness.DynFixed},
	}
}

// designShown puts one finished design on the lane and pumps it in, the way the
// program loop does.
func designShown(t *testing.T, events ...session.Event) (*designingAgent, *app) {
	t.Helper()
	agent := &designingAgent{
		fakeAgent: &fakeAgent{model: "m"},
		lane:      make(chan session.Event, len(events)+1),
	}
	a := newTestApp(agent)
	// A card is a block and this surface's default test frame is twenty rows: a
	// page has to fit above the input for the assertions to be about the card
	// rather than about the room it was drawn in.
	a.height = 44
	cmd := a.watchDesigns()
	if cmd == nil {
		t.Fatal("the surface did not open the design lane")
	}
	for _, event := range events {
		agent.lane <- event
	}
	for range events {
		drive(t, a, runCmd(cmd)...)
	}
	return agent, a
}

func designDoneEvent() session.Event {
	page := designedPage()
	return session.Event{Kind: session.EventHarnessDesignDone, ID: 7, Text: page.Id.Name, Hint: page.Id.Desc, Harness: &page}
}

// THE PAGE IS DRAWN AND THE QUESTION SITS UNDER IT. Nobody approves a name; what
// a person can approve is the numbered list of steps and the bounds beneath it,
// which is the rendering every other surface uses (internal/subharness's card).
func TestTheDesignCardDrawsThePageAndAsksToSaveIt(t *testing.T) {
	_, a := designShown(t, designDoneEvent())
	if !a.asksHarness() {
		t.Fatalf("no question is up:\n%s", plain(frame(a)))
	}
	a.width = 100
	got := plain(frame(a))
	for _, want := range []string{
		"flake-triage · draft · chase a flaky test to a fix", // the card's own head
		"1   look",                     // the steps, in run order
		"2   check",                    // and the check under it
		"tools  read",                  // the bounds
		`save harness "flake-triage"?`, // the question, in the verb that fits it
		"[enter] save",
		"[esc] discard",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the card is missing %q:\n%s", want, got)
		}
	}
	// The block is the card plus its one question row, and the layout budget has
	// to agree with what was drawn or the frame would cut somebody's answer off.
	if height, rows := a.harnessAskHeight(), len(a.harnessAskRows(a.width)); height != rows {
		t.Fatalf("the block says %d rows and drew %d", height, rows)
	}
	// AND IT DOES NOT SAY "RUN". The design has not run and is not about to.
	if strings.Contains(got, `run harness "flake-triage"?`) {
		t.Fatalf("the card asked the offer's question:\n%s", got)
	}
}

// THE TWO ANSWERS, in the four keys the offer above it already answers to.
func TestTheDesignCardAnswersToItsKeys(t *testing.T) {
	for _, c := range []struct {
		key  string
		save bool
	}{{"enter", true}, {"y", true}, {"esc", false}, {"n", false}} {
		agent, a := designShown(t, designDoneEvent())
		drive(t, a, key(c.key))
		if len(agent.answers) != 1 {
			t.Fatalf("%s sent %d answers", c.key, len(agent.answers))
		}
		if got := agent.answers[0]; got.id != 7 || got.run != c.save {
			t.Fatalf("%s answered %+v, want id 7 save=%v", c.key, got, c.save)
		}
		if a.asksHarness() {
			t.Fatalf("%s left the card up", c.key)
		}
	}
}

// AND IT IS A BUTTON, on the question row and nowhere else: a press on the page
// above it is a press on a quotation, and answering from there would be
// answering a question the pointer was not on.
func TestTheDesignCardAnswersOnItsQuestionRowAlone(t *testing.T) {
	agent, a := designShown(t, designDoneEvent())
	// Wide enough for the question and both answers: a row that does not fit is
	// cut and records no targets at all, which is a law the offer states in full
	// (harness.go) and a different test than this one.
	a.width = 100
	y := harnessRowY(t, a)
	if len(a.harnessTaps) != 2 {
		t.Fatalf("the row recorded %d targets, want two", len(a.harnessTaps))
	}
	// The card sits above the question, so the marked row cannot be the first of
	// the block.
	if card := a.harnessCard(a.harnessAsks[0]); len(card) == 0 {
		t.Fatal("the ask carries no card")
	}
	at := a.harnessTaps[0]
	drive(t, a, tea.MouseClickMsg{X: at.span.from + 1, Y: y, Button: tea.MouseLeft})
	if len(agent.answers) != 1 || !agent.answers[0].run {
		t.Fatalf("the press answered %+v, want a save", agent.answers)
	}
}

// A DESIGN OUTLIVES TURNS, and this is the one place it parts from the offer.
// The turn that asked for it ended the moment it started (session's
// harness_build.go), so a settle that swept the card away would throw the answer
// out seconds before it was given.
func TestTheDesignCardSurvivesTheTurnEnding(t *testing.T) {
	agent, a := designShown(t, designDoneEvent())
	a.settle()
	if !a.asksHarness() {
		t.Fatal("the design card died with a turn it was never about")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("settling answered the card: %+v", agent.answers)
	}
	// And the offer beside it still does die with its turn: same queue, two
	// lifetimes.
	a.harnessAsks = append(a.harnessAsks, harnessAsk{id: 9, name: "research"})
	a.settle()
	if len(a.harnessAsks) != 1 || !a.harnessAsks[0].designed {
		t.Fatalf("the settle kept the wrong questions: %+v", a.harnessAsks)
	}
}

// THE LANE ALSO CARRIES WORDS: a design starting, and what became of one. Both
// are notes, because neither is a question.
func TestTheDesignLaneDrawsItsNotes(t *testing.T) {
	_, a := designShown(t,
		session.Event{Kind: session.EventHarnessDesign, Text: "triaging flaky tests", Hint: "designing"},
		session.Event{Kind: session.EventNotice, Text: `harness "flake-triage" v1 saved`},
	)
	a.width = 100
	got := plain(frame(a))
	if !strings.Contains(got, "harness · designing triaging flaky tests") {
		t.Fatalf("the design starting was never drawn:\n%s", got)
	}
	if !strings.Contains(got, `harness "flake-triage" v1 saved`) {
		t.Fatalf("the outcome was never drawn:\n%s", got)
	}
}

// AND THE NOTE NAMES THE MODEL WRITING THE PAGE when the session resolved one.
// A design is two calls on a model the person did not type — the designer role's
// (internal/roles), which sits high — and while it runs this note is the only
// thing on screen.
func TestTheDesignNoteNamesTheModelDesigning(t *testing.T) {
	_, a := designShown(t, session.Event{
		Kind:  session.EventHarnessDesign,
		Text:  "triaging flaky tests",
		Hint:  "designing",
		Model: "moonshot/kimi-k3",
	})
	a.width = 100
	got := plain(frame(a))
	if !strings.Contains(got, "harness · designing with moonshot/kimi-k3 · triaging flaky tests") {
		t.Fatalf("the design note does not name the model designing:\n%s", got)
	}
}

// ── the row while it is being written ───────────────────────────────────────

// THE ASSERTION IS REAL, and it is stated here because an optional interface
// nothing implements is a feature that silently never happens — which is what
// became of the running-harness chip beside this one (harnesspanel.go's
// [harnessLive] has no such line, and no session provides it).
var _ designLive = (*session.Agent)(nil)

// writingAgent is a session that is in the middle of writing harnesses.
type writingAgent struct {
	*fakeAgent
	writing []session.HarnessBeingDesigned
	// stopped is every id this session was asked to end, in order — the door the
	// chip's ✕ reaches (session's cancel.go).
	stopped []string
}

func (w *writingAgent) HarnessesBeingDesigned() []session.HarnessBeingDesigned { return w.writing }

func (w *writingAgent) Cancel(id string) (string, error) {
	w.stopped = append(w.stopped, id)
	return "harness design stopped; nothing was saved", nil
}

// designingApp is a surface watching one design that started a minute and a
// half ago, on a frame wide enough for the whole chip.
func designingApp(t *testing.T, writing ...session.HarnessBeingDesigned) (*writingAgent, *app) {
	t.Helper()
	agent := &writingAgent{fakeAgent: &fakeAgent{model: "m"}, writing: writing}
	a := newTestApp(agent)
	a.width, a.height = 120, 30
	now := time.Now()
	a.clock = func() time.Time { return now }
	for at := range agent.writing {
		agent.writing[at].Since = now.Add(-90 * time.Second)
	}
	return agent, a
}

func harnessBeingWritten(id uint64) session.HarnessBeingDesigned {
	return session.HarnessBeingDesigned{ID: id, Goal: designedGoal}
}

// designedGoal is a brief that fits the chip's own budget whole, so a test
// reading the row is reading the words and not the cut.
const designedGoal = "triage flaky tests"

// A HARNESS BEING WRITTEN RAISES THE STRIP AND SAYS SO IN WORDS. It is the one
// piece of work here with no turn under it: the turn ended the moment the design
// started, so without this row the screen says nothing at all for a minute or
// two.
func TestTheStripSaysAHarnessIsBeingDesigned(t *testing.T) {
	_, a := designingApp(t, harnessBeingWritten(3))
	if !a.stripShowing() {
		t.Fatal("a harness being designed did not raise the strip")
	}
	row := plain(a.stripRow(a.width))
	for _, want := range []string{"designing a harness", designedGoal, "1m 30s"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the row is missing %q: %q", want, row)
		}
	}
	// AND THE CLOCK KEEPS TURNING FOR IT. Nothing else on this surface is moving
	// — no node is running and no stream is open — so the spinner and the count
	// would freeze at the frame the design started on.
	if !a.tasksAnimating() {
		t.Fatal("the paint clock stopped while a harness was being designed")
	}
}

// AND IT GOES AWAY THE MOMENT THE SESSION STOPS NAMING THE WORK, which is every
// ending at once: the card, the failure, the decline, the stop and the window
// running out. The row is asked for from the agent on every frame and held
// nowhere, so there is no ending it can miss.
func TestTheStripDropsTheDesignAtEveryEnding(t *testing.T) {
	agent, a := designingApp(t, harnessBeingWritten(3))
	if !a.stripShowing() {
		t.Fatal("a harness being designed did not raise the strip")
	}
	agent.writing = nil
	if a.stripShowing() {
		t.Fatal("the strip stayed up after the design ended")
	}
	if row := a.stripRow(a.width); row != "" {
		t.Fatalf("the strip drew a row for nothing: %q", row)
	}
	if a.tasksAnimating() {
		t.Fatal("the paint clock kept turning for a design that had ended")
	}
	// AND A SURFACE WHOSE AGENT CANNOT DESIGN AT ALL never had one — which is
	// every remote session, where building a harness is switched off.
	plain := newTestApp(&fakeAgent{model: "m"})
	plain.width, plain.height = 120, 30
	if plain.designingHarness() || plain.stripShowing() {
		t.Fatal("a session with no designer drew a design")
	}
}

// THE CHIP OFFERS THE ONE THING THAT CAN BE DONE ABOUT IT, and it is not a door.
// There is no room to walk into and nothing written down, so the ✕ is the only
// column on it that answers a press.
func TestTheDesignChipOffersStoppingAndNoDoor(t *testing.T) {
	_, a := designingApp(t, harnessBeingWritten(3))
	a.stripRow(a.width) // the layout is what records the columns
	if a.stripDesignStop.to <= a.stripDesignStop.from {
		t.Fatalf("the chip recorded no ✕ to press: %+v", a.stripDesignStop)
	}
	// A press on the words is swallowed by the row and opens nothing.
	if _, took := a.stripPress(a.stripDesignStop.from-4, a.headHeight()); !took {
		t.Fatal("the strip did not take the press")
	}
	if a.stopping() || a.harnPanel.open || a.roomOpen() {
		t.Fatal("pressing the chip's words did something")
	}
	if _, took := a.stripPress(a.stripDesignStop.from, a.headHeight()); !took {
		t.Fatal("the strip did not take the press on the ✕")
	}
	if !a.stopping() {
		t.Fatal("the ✕ raised no card")
	}
	if got, want := a.stop.target.question(), "Stop this harness design? "+stopDesignDetail; got != want {
		t.Fatalf("the card asks %q, want %q", got, want)
	}
	if got, want := a.stop.target.id, session.CancelDesign+":3"; got != want {
		t.Fatalf("the card would stop %q, want %q", got, want)
	}
}

// AND ANSWERING IT REACHES THE SESSION'S OWN DOOR, with the id spelled the way
// that door takes it — a bare number there names a task.
func TestStoppingTheDesignFromTheChipReachesTheSession(t *testing.T) {
	agent, a := designingApp(t, harnessBeingWritten(3))
	a.stripRow(a.width)
	if _, took := a.stripPress(a.stripDesignStop.from, a.headHeight()); !took {
		t.Fatal("the strip did not take the press on the ✕")
	}
	// The cursor opens on "keep going", which is where every stop card opens it.
	drive(t, a, key("left"), key("enter"))
	if len(agent.stopped) != 1 || agent.stopped[0] != session.CancelDesign+":3" {
		t.Fatalf("the session was asked to stop %v", agent.stopped)
	}
	if a.stopping() {
		t.Fatal("the card stayed up after it was answered")
	}
}

// TWO AT ONCE ARE COUNTED AND OFFER NOTHING. One ✕ standing for two designs
// would end whichever the surface guessed.
func TestTwoDesignsAreCountedAndCannotBeStoppedFromTheChip(t *testing.T) {
	_, a := designingApp(t, harnessBeingWritten(3), harnessBeingWritten(4))
	row := plain(a.stripRow(a.width))
	if !strings.Contains(row, "designing 2 harnesses") {
		t.Fatalf("the row does not count them: %q", row)
	}
	if strings.Contains(row, designedGoal) {
		t.Fatalf("the row named one brief for two designs: %q", row)
	}
	if a.stripDesignStop.pressable() {
		t.Fatalf("a ✕ was drawn for two designs: %+v", a.stripDesignStop)
	}
}

// A NARROW FRAME GIVES UP THE BRIEF AND KEEPS THE FACT. The goal is context for
// a sentence the person typed a minute ago; that a harness is being written at
// all is the whole reason the row exists.
func TestANarrowDesignChipKeepsTheWords(t *testing.T) {
	_, a := designingApp(t, harnessBeingWritten(3))
	a.width = 34
	row := plain(a.stripRow(a.width))
	if !strings.Contains(row, "designing a harness") {
		t.Fatalf("the narrow row lost the words: %q", row)
	}
	if strings.Contains(row, "triage") {
		t.Fatalf("the narrow row kept the brief: %q", row)
	}
	if ansi.StringWidth(row) > a.width {
		t.Fatalf("the row is %d cells wide on a %d-cell frame: %q", ansi.StringWidth(row), a.width, row)
	}
}

// A SURFACE WITH NO DESIGNER UNDER IT OPENS NO LANE. Every scripted agent in
// this package is one, and none of them may be made un-representable by a
// feature they have never heard of.
func TestASessionWithNoDesignerOpensNoLane(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if cmd := a.watchDesigns(); cmd != nil {
		t.Fatal("a lane was opened on a session that has no designer")
	}
	if a.designLane != nil {
		t.Fatal("the surface kept a lane it never got")
	}
}

// A LONG PAGE IS CUT AND SAYS SO. A block that quietly stopped would be asking
// somebody to approve a shape while showing them part of one.
func TestALongDesignCardSaysWhatItCut(t *testing.T) {
	page := designedPage()
	// Enough steps that the card runs past the block's ceiling.
	for at := 0; at < harnessCardRows; at++ {
		id := "step" + itoa(at)
		page.Program.Nodes = append(page.Program.Nodes, subharness.Node{
			Id: id, Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "do a thing"},
		})
		page.Program.Edges = append(page.Program.Edges, subharness.Edge{"check", id})
	}
	_, a := designShown(t, session.Event{
		Kind: session.EventHarnessDesignDone, ID: 7, Text: page.Id.Name, Harness: &page,
	})
	a.width = 100
	rows := a.harnessAskRows(a.width)
	if len(rows) != harnessCardRows+1 {
		t.Fatalf("the block drew %d rows, want %d", len(rows), harnessCardRows+1)
	}
	if !strings.Contains(plain(strings.Join(rows, "\n")), "more lines") {
		t.Fatalf("the block cut the page without saying so:\n%s", plain(strings.Join(rows, "\n")))
	}
	if !strings.Contains(plain(rows[len(rows)-1]), "[enter] save") {
		t.Fatalf("the question is not the last row:\n%s", plain(rows[len(rows)-1]))
	}
}
