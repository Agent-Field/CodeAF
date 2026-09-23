package tui3

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE STOP CARD ───────────────────────────────────────────────────────────
//
// What these hold shut, in the order a person meets them: the card is always
// asked and its cursor starts on the safe answer, the key and the button reach
// the same card, esc is never a stop, the id the session is handed names the
// right kind of work, and stopped work looks stopped afterwards.

// stopFake is a session that can end work: [roomFake] widened by the one door
// [stopAgent] asserts.
type stopFake struct {
	*roomFake
	asked  []string
	line   string
	err    error
	orch   *orchFake
	stopFn func(string)
}

func (f *stopFake) Cancel(id string) (string, error) {
	f.asked = append(f.asked, id)
	if f.stopFn != nil {
		f.stopFn(id)
	}
	if f.err != nil {
		return "", f.err
	}
	if f.line == "" {
		return "stopping task 7 — its branch is kept", nil
	}
	return f.line, nil
}

// The run doors, so one fake can be a task room AND a run's page.
func (f *stopFake) OrchestrateSnapshot(id string) (orchestrate.Snapshot, bool) {
	if f.orch == nil {
		return orchestrate.Snapshot{}, false
	}
	return f.orch.OrchestrateSnapshot(id)
}
func (f *stopFake) OrchestrateNodeJournal(runID, nodeID string) (string, bool) {
	if f.orch == nil {
		return "", false
	}
	return f.orch.OrchestrateNodeJournal(runID, nodeID)
}
func (f *stopFake) SteerOrchestrate(id, text string) error { return f.orch.SteerOrchestrate(id, text) }
func (f *stopFake) ResolveOrchestrate(id, answer string) (string, error) {
	return f.orch.ResolveOrchestrate(id, answer)
}

// stopApp is [roomApp] with the stopping door open: one node running, the
// roster holding the keyboard and its cursor on that node.
func stopApp(t *testing.T) (*app, *stopFake) {
	t.Helper()
	base, room, _ := roomApp(t)
	agent := &stopFake{roomFake: room}
	base.agent = agent
	base.railTake(true)
	base.railWhere = railSpot{id: 7}
	base.touch()
	return base, agent
}

// stopText is the block as a reader sees it, laid out at the width the frame
// gives it. The card is the question block's now (question.go), so this is the
// same door every other question on this surface is read through.
func stopText(a *app) string {
	return plain(strings.Join(a.questionRows(a.width), "\n"))
}

// showStop puts the raised card on screen and lets it settle: the block takes no
// key from a question it has never drawn, and drops one that lands inside the
// settle guard (question.go).
func showStop(a *app) {
	a.questionRows(a.width)
	settleAsk(a)
}

// stopPick is where the cursor is on the raised card.
func stopPick(t *testing.T, a *app) int {
	t.Helper()
	head, ok := a.questionHead()
	if !ok {
		t.Fatal("no card is up")
	}
	return head.pick
}

// ── the card ────────────────────────────────────────────────────────────────

// THE DESTRUCTIVE ANSWER IS NEVER THE DEFAULT. This is the whole reason the card
// exists, and it is the one assertion that must not be able to drift.
func TestTheStopCardOpensOnKeepGoing(t *testing.T) {
	a, agent := stopApp(t)
	drive(t, a, key("x"))
	if !a.stopping() {
		t.Fatalf("x on the focused chip raised nothing")
	}
	showStop(a)
	if at := stopPick(t, a); at != stopKeepAt {
		t.Fatalf("the cursor opened on %q", stopAnswers[at])
	}
	text := stopText(a)
	for _, want := range []string{"Stop this task?", stopTaskDetail, "stop it", "keep going", "esc keep going"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card is missing %q:\n%s", want, text)
		}
	}
	// AND ENTER, THE KEY PEOPLE PRESS TO MAKE A QUESTION GO AWAY, does not stop
	// anything.
	showStop(a)
	drive(t, a, key("enter"))
	if a.stopping() {
		t.Fatalf("enter left the card up")
	}
	if len(agent.asked) != 0 {
		t.Fatalf("enter on the default answer stopped %v", agent.asked)
	}
}

// THERE IS NO BYPASS. One card, always — and the second `x` is a keystroke the
// card swallows rather than a shortcut through it.
func TestTheStopCardHasNoBypass(t *testing.T) {
	a, agent := stopApp(t)
	drive(t, a, key("x"))
	showStop(a)
	drive(t, a, key("x"), key("x"))
	if !a.stopping() {
		t.Fatalf("leaning on the key put the card away")
	}
	if len(agent.asked) != 0 {
		t.Fatalf("work was stopped without an answer: %v", agent.asked)
	}
}

// esc IS "keep going" AND NEVER THE ACT, and under the card esc is still what
// it always was: the way back.
func TestEscOnTheStopCardIsNeverAStop(t *testing.T) {
	a, agent := stopApp(t)
	drive(t, a, key("x"))
	showStop(a)
	drive(t, a, key("esc"))
	if a.stopping() {
		t.Fatalf("esc left the card up")
	}
	if len(agent.asked) != 0 {
		t.Fatalf("esc stopped %v", agent.asked)
	}
	// The roster still has the keyboard: the card was dismissed, not the page
	// under it.
	if !a.railHold {
		t.Fatalf("esc on the card gave away the keyboard as well")
	}
}

// →, THEN ENTER, IS THE ONLY KEYBOARD PATH TO A STOP, and what it hands the
// session is a TASK id.
func TestTheStopCardStopsTheFocusedTask(t *testing.T) {
	a, agent := stopApp(t)
	agent.line = "stopping task 7 — its branch is kept"
	drive(t, a, key("x"))
	showStop(a)
	drive(t, a, key("left"), key("enter"))
	if a.stopping() {
		t.Fatalf("the card is still up")
	}
	if len(agent.asked) != 1 || agent.asked[0] != session.CancelTask+":7" {
		t.Fatalf("the session was asked to stop %v", agent.asked)
	}
	if !strings.Contains(taskText(a), "stopping task 7") {
		t.Fatalf("the engine's line never reached the conversation:\n%s", taskText(a))
	}
}

// The cursor wraps in either direction without ending any work.
func TestTheStopCardCursorWrapsAtBothEnds(t *testing.T) {
	a, agent := stopApp(t)
	drive(t, a, key("x"))
	showStop(a)
	drive(t, a, key("right"))
	if at := stopPick(t, a); at != 0 {
		t.Fatalf("right did not wrap from keep going: %q", stopAnswers[at])
	}
	drive(t, a, key("left"))
	if at := stopPick(t, a); at != stopKeepAt {
		t.Fatalf("left did not wrap back to keep going: %q", stopAnswers[at])
	}
	if len(agent.asked) != 0 {
		t.Fatalf("navigation stopped work: %v", agent.asked)
	}
}

// A REFUSAL IS KEPT AND SAID. A stop that could not be given is work still
// running, and swallowing the reason leaves a person pressing the same key.
func TestAStopThatWasRefusedSaysWhy(t *testing.T) {
	a, agent := stopApp(t)
	agent.err = errors.New("there is no task 7 in this session")
	drive(t, a, key("x"))
	showStop(a)
	drive(t, a, key("left"), key("enter"))
	if !strings.Contains(taskText(a), "there is no task 7") {
		t.Fatalf("the refusal was swallowed:\n%s", taskText(a))
	}
}

// A LETTER IS A LETTER THE MOMENT THERE IS A SENTENCE.
func TestXOverATypedSentenceIsJustAnX(t *testing.T) {
	a, _ := stopApp(t)
	drive(t, a, key("f"), key("i"), key("x"))
	if a.stopping() {
		t.Fatalf("x raised the card out of the middle of a word")
	}
	if got := a.input.String(); got != "fix" {
		t.Fatalf("the box reads %q", got)
	}
}

// AND THERE IS NOTHING TO STOP ON WORK THAT HAS LANDED: the key falls through
// and reaches the box as the letter it is.
func TestXOverSettledWorkRaisesNothing(t *testing.T) {
	a, _ := stopApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{})})
	drive(t, a, key("x"))
	if a.stopping() {
		t.Fatalf("a landed node was offered a stop")
	}
	if got := a.input.String(); got != "x" {
		t.Fatalf("the key was eaten; the box reads %q", got)
	}
}

// ONE STOPPABLE ROW NEEDS NO CURSOR AT ALL (#892), and this is the whole of the
// ruling: with nothing to choose between, the keystroke cannot be aimed at the
// wrong thing, and what keeps a bare key from ending an hour of work is the card
// asking — not the roster holding the keyboard. So a bare `x`, over an empty box,
// with no `alt+t` first, raises that row's card.
func TestOneVisibleStoppableRowAnswersToABareX(t *testing.T) {
	base, _, _ := roomApp(t)
	agent := &stopFake{roomFake: base.agent.(*roomFake)}
	base.agent = agent
	// No hold, no cursor: the roster was never asked for.
	if base.railHold {
		t.Fatalf("the fixture started with the roster holding the keyboard")
	}
	drive(t, base, key("x"))
	if !base.stopping() {
		t.Fatalf("a bare x over the only stoppable row raised nothing")
	}
	showStop(base)
	if base.stop.target.noun != stopTaskNoun {
		t.Fatalf("the card offered to stop a %q", base.stop.target.noun)
	}
	// The card, not the key, ends the work — and the safe answer is where it opens.
	if at := stopPick(t, base); at != stopKeepAt {
		t.Fatalf("the cursor opened on %q", stopAnswers[at])
	}
	// AND THE KEY STILL EATS THE LETTER: it reached the target, so it never
	// fell through to the composer as the letter it was.
	if got := base.input.String(); got != "" {
		t.Fatalf("the key typed %q on its way to the card", got)
	}
}

// TWO OR MORE ROWS ARE THE CASE THE FOCUS REQUIREMENT WAS WRITTEN FOR: nothing
// to aim at, so the key is the letter it is (#892).
func TestTwoVisibleStoppableRowsKeepTheFocusRequirement(t *testing.T) {
	base, _, _ := roomApp(t)
	agent := &stopFake{roomFake: base.agent.(*roomFake)}
	base.agent = agent
	drive(t, base, streamEventMsg{gen: base.gen, ev: update(8, "Sweep the imports",
		session.TaskRunning, session.TaskNotice{})})
	if base.railHold {
		t.Fatalf("the fixture started with the roster holding the keyboard")
	}
	drive(t, base, key("x"))
	if base.stopping() {
		t.Fatalf("a bare x guessed between two stoppable rows")
	}
	if got := base.input.String(); got != "x" {
		t.Fatalf("the key was eaten; the box reads %q", got)
	}
	// AND alt+t STILL AIMS IT, which is the path the manual names: give the
	// roster the keyboard, and the cursor answers for which row.
	base.railTake(true)
	base.railWhere = railSpot{id: 7}
	base.touch()
	base.input = editor{} // the letter is cleared; the question is what stops
	drive(t, base, key("x"))
	if !base.stopping() {
		t.Fatalf("x under the hold raised nothing")
	}
	showStop(base)
	if base.stop.target.noun != stopTaskNoun {
		t.Fatalf("the card offered to stop a %q", base.stop.target.noun)
	}
}

// ── the pointer ─────────────────────────────────────────────────────────────

// THE ✕ IN THE ROOM'S HEADER RAISES THE SAME CARD the key does. Same question,
// two hands.
func TestTheHeaderMarkRaisesTheSameCard(t *testing.T) {
	a, _ := stopApp(t)
	a.openRoomFor(7, "Fix the nil-map crash")
	a.touch()
	width, _ := a.size()
	head := strings.Join(a.roomHeadRows(width), "\n")
	if !strings.Contains(plain(head), roomStopMark) {
		t.Fatalf("the header carries no ✕:\n%s", plain(head))
	}
	if !a.roomStop.pressable() {
		t.Fatalf("the ✕ was drawn but answers to no columns")
	}
	drive(t, a, press(a.roomStop.from, a.roomFactsRow()))
	if !a.stopping() {
		t.Fatalf("Stop raised nothing")
	}
	if a.stop.target.noun != stopTaskNoun {
		t.Fatalf("Stop in a task's room offered to stop a %q", a.stop.target.noun)
	}
}

// AND THE CARD'S OWN ANSWERS ARE PRESSABLE, which is the other half of key/tap
// parity: everything the keyboard can answer, a finger can.
//
// A PRESS IS NOT A KEYSTROKE. The move-then-take law is about keys — a hand
// resting on the row a question lands under — and a pointer put on a named
// answer and clicked is somebody aiming at that answer, so it takes it outright.
func TestTheStopCardAnswersToAPress(t *testing.T) {
	a, agent := stopApp(t)
	drive(t, a, key("x"))
	showStop(a) // the layout is what writes the bands
	if len(a.questionBands) != len(stopAnswers) {
		t.Fatalf("the card recorded %d targets", len(a.questionBands))
	}
	band := a.questionBands[0]
	drive(t, a, press(band.span.from, chromeRowY(t, a, band.row)))
	if a.stopping() {
		t.Fatalf("the press did not answer the card")
	}
	if len(agent.asked) != 1 {
		t.Fatalf("the press asked for %v", agent.asked)
	}
}

// THE STRIP CARRIES NO ✕ AND NO CURSOR ANY MORE, and neither is a loss: the row
// is only ever on screen where the roster is NOT (taskstrip.go's
// [app.stripShowing]), so a mark drawn from the roster's own cursor would be a
// mark that can never be true. The button a pointer stops work with is the
// room's own header, at every width (room.go).
func TestTheStripCarriesNoStopMarkWhereTheRosterStands(t *testing.T) {
	a, _ := stopApp(t)
	a.width, a.height = 140, 30
	a.touch()
	if a.stripShowing() {
		t.Fatalf("the strip stood up beside the roster")
	}
	if row := plain(a.stripRow(a.width)); strings.Contains(row, roomStopMark) || row != "" {
		t.Fatalf("the strip drew a row over the roster:\n%q", row)
	}
	// AND THE ROOM'S HEADER STILL HAS IT, which is where a pointer ends work.
	a.openRoom(7, "Fix the nil-map crash")
	if !strings.Contains(plain(strings.Join(a.roomHeadRows(a.bodyWidth()), "\n")), roomStopMark) {
		t.Fatalf("the room's header lost its ✕:\n%q", plain(strings.Join(a.roomHeadRows(a.bodyWidth()), "\n")))
	}
}

// ── an adaptive run ─────────────────────────────────────────────────────────

// A RUN'S PAGE OFFERS TO STOP THE RUN, and the id it hands over says so.
func TestTheRunPageStopsTheRun(t *testing.T) {
	a, agent := runStopApp(t)
	drive(t, a, key("x"))
	if !a.stopping() {
		t.Fatalf("x on a run's page raised nothing")
	}
	// The head is the QUESTION and nothing else; what stopping does not take
	// away is the promise on its own row under it (stop.go).
	if got := a.stop.target.question(); got != "Stop this run?" {
		t.Fatalf("the card asks %q", got)
	}
	if got := a.stop.target.promise(); got != stopRunDetail {
		t.Fatalf("the card promises %q", got)
	}
	drive(t, a, key("left"), key("enter"))
	if len(agent.asked) != 1 || agent.asked[0] != session.CancelRun+":r1" {
		t.Fatalf("the session was asked to stop %v", agent.asked)
	}
}

// A STOPPED RUN LOOKS STOPPED: the state word, the header's mark, the cancelled
// chips greyed and still on the page.
func TestAStoppedRunReadsAsStopped(t *testing.T) {
	a, agent := runStopApp(t)
	snap := agent.orch.snaps["r1"]
	snap.Stopped, snap.Done = true, true
	snap.Nodes[1].State = orchestrate.Cancelled
	snap.Nodes[2].State = orchestrate.Cancelled
	agent.orch.snaps["r1"] = snap
	orchPollNow(t, a)

	if got := a.orchStateWord(); got != orchStoppedWord {
		t.Fatalf("the header says the run is %q", got)
	}
	if got := plain(a.orchHeadMark()); got != glyphStopped {
		t.Fatalf("the header's mark is %q, want the stop mark", got)
	}
	page := strings.Join(orchLines(a), "\n")
	for _, node := range []string{"rfcs", "client"} {
		if !strings.Contains(page, node) {
			t.Fatalf("the cancelled node %q was taken off the page:\n%s", node, page)
		}
	}
	if !strings.Contains(page, glyphStopped) {
		t.Fatalf("no cancelled chip wears the stop mark:\n%s", page)
	}
	// AND THERE IS NOTHING LEFT TO STOP: the ✕ goes with the run that is over.
	if a.stopOffered() {
		t.Fatalf("a finished run is still offering a stop")
	}
}

// THE GATE'S "stop" ROW IS THE SAME STOP, routed through the same door the key
// uses (session's ResolveOrchestrate).
func TestTheFuelGateStopUsesTheSameWord(t *testing.T) {
	a, agent := runStopApp(t)
	run := a.orchOf()
	run.gate = &orchGate{id: 1, text: "$2.00 of $2.00"}
	run.pick = orchTarget{answer: orchStop}
	spend(t, a, a.orchAnswer(orchStop))
	if len(agent.orch.answers) != 1 || !strings.HasSuffix(agent.orch.answers[0], orchestrate.GateStop) {
		t.Fatalf("the gate's stop went to %v", agent.orch.answers)
	}
}

// runStopApp opens a run's page on a session that can also stop things.
func runStopApp(t *testing.T) (*app, *stopFake) {
	t.Helper()
	base, room, _ := roomApp(t)
	orch := &orchFake{
		fakeAgent: room.taskFake.fakeAgent,
		snaps:     map[string]orchestrate.Snapshot{"r1": orchRun4()},
	}
	agent := &stopFake{roomFake: room, orch: orch}
	base.agent = agent
	base.width, base.height = 140, 30
	base.openOrchRoom("r1", "answer the retry question")
	base.touch()
	if base.orchOf() == nil {
		t.Fatal("the run's page did not open")
	}
	return base, agent
}

// A RUN THIS SESSION CANNOT SEE OFFERS NOTHING TO STOP.
//
// A conversation reopened tomorrow redraws the rows of the runs it started, as
// history (session's task_store.go). The session holds no orchestrator behind
// them, so the page opens with no shape and the snapshot is the zero one —
// neither done nor stopped — and the ✕ used to be drawn over it and answer
// "there is no run … in this session".
func TestARunThatEndedWithTheLastProcessOffersNoStop(t *testing.T) {
	a, _ := runStopApp(t)
	a.openOrchRoom("r9", "audit the pricing code")
	a.touch()
	run := a.orchOf()
	if run == nil {
		t.Fatal("the run's page did not open")
	}
	if run.known {
		t.Fatalf("the page for a run this session never held came up knowing its shape")
	}
	if a.stopOffered() {
		t.Fatal("a run that ended with the last process is still offering a stop")
	}
}

// ── a stopped task, afterwards ──────────────────────────────────────────────

// A NODE A PERSON STOPPED IS NOT A NODE THAT FAILED, in either place a person
// reads its state.
func TestAStoppedTaskWearsTheStopMarkAndTheStopWord(t *testing.T) {
	a, _ := stopApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskFailed, session.TaskNotice{Stopped: true, Merge: "aborted"})})
	node := a.tasks[7]
	if node == nil || !node.stopped {
		t.Fatalf("the surface did not keep who ended the work")
	}
	if got := plain(a.railGlyph(node)); got != glyphStopped {
		t.Fatalf("the roster draws %q, want the stop mark", got)
	}
	if got := a.roomStateWord(node); got != taskStoppedByPerson {
		t.Fatalf("the header says %q", got)
	}
}

// press is one left-button click at a column and a row.
func press(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}
