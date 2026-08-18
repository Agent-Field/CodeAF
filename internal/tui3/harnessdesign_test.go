package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// THE LANE ALSO CARRIES THE ANNOUNCE, and the announce NAMES THE TASK: one line,
// with the number that opens the design's own room on the end of it.
func TestTheDesignLaneNamesTheTaskItStarted(t *testing.T) {
	_, a := designShown(t, session.Event{
		Kind: session.EventHarnessDesign, Text: "triaging flaky tests", Hint: "designing",
		Task: &session.TaskNotice{ID: 4},
	})
	a.width = 100
	got := plain(frame(a))
	if !strings.Contains(got, "harness · designing triaging flaky tests — task 4") {
		t.Fatalf("the announce does not name the task:\n%s", got)
	}
}

// AND AN ANNOUNCE WITH NO NODE ON IT SAYS NOTHING EXTRA. An older engine, or one
// over --host where designing is off, sends no task — and a separator standing
// in front of no number is punctuation pretending to be information.
func TestTheDesignAnnounceWithNoTaskDrawsNoTail(t *testing.T) {
	_, a := designShown(t, session.Event{
		Kind: session.EventHarnessDesign, Text: "triaging flaky tests", Hint: "designing",
	})
	a.width = 100
	got := plain(frame(a))
	if !strings.Contains(got, "harness · designing triaging flaky tests") {
		t.Fatalf("the announce was never drawn:\n%s", got)
	}
	if strings.Contains(got, "— task") {
		t.Fatalf("the announce invented a task:\n%s", got)
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

// ── the design as a node ────────────────────────────────────────────────────
//
// A design used to have a chip of its own on the strip, because it had nothing
// else: no id, no room, no row. It is a task now (session's harness_task.go), so
// what is tested here is that it wears the ordinary furniture correctly — the
// phase in place of the state word, and a stop that does not promise a branch.

// designingNode is one node on the roster as the engine publishes a design:
// running, its phase named, and its kind saying what it is.
func designingNode(t *testing.T, phase string) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 30
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID:    4,
		Title: "harness · " + designedGoal,
		Kind:  session.TaskKindHarness,
		State: session.TaskRunning,
		Doing: phase,
	}})
	return a
}

// designedGoal is a brief that fits the strip's own budget whole, so a test
// reading a row is reading the words and not the cut.
const designedGoal = "triage flaky tests"

// A DESIGN TAKES AN ORDINARY PLACE ON THE STRIP, and the strip rises for it the
// way it rises for any running node.
func TestADesignRaisesTheStripAsATask(t *testing.T) {
	a := designingNode(t, "designing")
	if !a.stripShowing() {
		t.Fatal("a harness being designed did not raise the strip")
	}
	if row := plain(a.stripRow(a.width)); !strings.Contains(row, "harness") {
		t.Fatalf("the strip does not name the design: %q", row)
	}
	// AND THE CLOCK KEEPS TURNING FOR IT. Nothing else on this surface is moving,
	// so the spinner would freeze at the frame the design started on.
	if !a.tasksAnimating() {
		t.Fatal("the paint clock stopped while a harness was being designed")
	}
}

// THE PHASE IS THE STATE WORD. "running" is this program's word for what a
// design is doing and "designing" is the person's, so the row and the room's
// header both say the second one.
func TestADesignsPhaseIsWhatTheSurfaceSays(t *testing.T) {
	for _, phase := range []string{"designing", "awaiting your look"} {
		a := designingNode(t, phase)
		node := a.tasks[4]
		if node == nil {
			t.Fatal("the design never reached the roster")
		}
		if got := a.roomStateWord(node); got != phase {
			t.Fatalf("the room header says %q, want %q", got, phase)
		}
		rows := a.railUnder(node, 60)
		if len(rows) == 0 || !strings.Contains(plain(rows[0]), phase) {
			t.Fatalf("the rail row does not say %q: %q", phase, plain(strings.Join(rows, " / ")))
		}
		// AND IT DOES NOT WEAR "finishing · " OR "waiting · " IN FRONT OF IT. Those
		// are this surface's words for which part of running a node is in; a phase
		// is not part of running, it is what the node is doing, so the row opens
		// with the phase itself.
		if got := plain(rows[0]); !strings.HasPrefix(got, phase) {
			t.Fatalf("the phase was prefixed with a word about running: %q", got)
		}
	}
}

// STOPPING A DESIGN PROMISES WHAT IS ACTUALLY TRUE OF IT. An ordinary node's
// card says the branch it wrote on is kept; a design has no branch and wrote no
// files, and what a person stopping one needs to know is that the registry is
// untouched.
func TestStoppingADesignDoesNotPromiseABranch(t *testing.T) {
	a := designingNode(t, "designing")
	target := a.stopTaskTarget(a.tasks[4])
	if target.empty() {
		t.Fatal("a running design cannot be stopped")
	}
	if target.id != session.CancelTask+":4" {
		t.Fatalf("the stop is aimed at %q", target.id)
	}
	if target.detail != stopDesignDetail {
		t.Fatalf("the card says %q", target.detail)
	}
	// And an ordinary node is untouched by any of this.
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 5, Title: "Fix the crash", State: session.TaskRunning,
	}})
	if got := a.stopTaskTarget(a.tasks[5]).detail; got != stopTaskDetail {
		t.Fatalf("an ordinary task's card says %q", got)
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
