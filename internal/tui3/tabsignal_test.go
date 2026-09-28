package tui3

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE MARK ON A TAB ───────────────────────────────────────────────────────
//
// A status mark is a claim, and on a strip it is eight claims at once. These
// hold the three that can go wrong: a mark drawn for something that is not a
// question for the person, a mark drawn about a conversation this window knows
// nothing about, and a reading that touches the disk or the agent's lock on
// every frame.

// countingAgent answers the two questions this file exists to keep OFF the
// frame, and counts every time it is asked. A number above zero here is the
// defect, whatever else passes.
type countingAgent struct {
	*fakeAgent
	asked   atomic.Int64
	indexed atomic.Int64
	waiting bool
}

func (c *countingAgent) NeedsPerson() bool {
	c.asked.Add(1)
	return c.waiting
}

func (c *countingAgent) TaskIndex() []session.TaskIndexEntry {
	c.indexed.Add(1)
	return nil
}

// signalLab is a window in one conversation with two more behind it, each with
// a watcher whose cached state a test can set the way the keeper's own loop
// would.
func signalLab(t *testing.T) (*app, *behindWatch, *countingAgent) {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file, a.workspace, a.title = "/tmp/lab/one.jsonl", "/tmp/lab", "Shipping the parser"
	emptyMachine(a)
	held := &countingAgent{fakeAgent: &fakeAgent{model: "m"}}
	watch := &behindWatch{key: "/tmp/lab/two.jsonl", agent: held}
	a.behind = map[string]*kept{
		"/tmp/lab/two.jsonl": {
			conv:  Conversation{Agent: held, SessionFile: "/tmp/lab/two.jsonl", Workspace: "/tmp/lab"},
			side:  &aside{since: a.now().Add(-time.Minute), title: "the tree walk"},
			watch: watch,
		},
		// AND ONE THIS WINDOW ONLY REMEMBERS: no agent, no watcher. It is the
		// shared-handle case and the closed case both.
		"/tmp/lab/three.jsonl": {
			conv: Conversation{SessionFile: "/tmp/lab/three.jsonl", Workspace: "/tmp/lab"},
			side: &aside{title: "an older one"},
		},
	}
	return a, watch, held
}

// ── WHAT EACH STATE IS ──────────────────────────────────────────────────────

// AT REST IS NOTHING. The emptiness law: no dot, no dimmed placeholder, no
// decorative badge on the tab a person is not being asked anything by.
func TestAnIdleConversationWearsNoMark(t *testing.T) {
	a, _, _ := signalLab(t)
	if got := a.tabSignalFor("/tmp/lab/two.jsonl", false); got != tabIdle {
		t.Fatalf("a held conversation with nothing happening reads %v", got)
	}
	if mark := tabSignalGlyph(tabIdle, false); mark != "" {
		t.Fatalf("idle drew %q, and it must draw nothing", mark)
	}
	if word := tabSignalWord(tabIdle); word != "" {
		t.Fatalf("idle said %q, and it must say nothing", word)
	}
}

// A TURN IN FLIGHT IS THE WORKING MARK, and it comes off the watcher's cache
// rather than off the agent.
func TestAHeldConversationTurningWearsTheWorkingMark(t *testing.T) {
	a, watch, _ := signalLab(t)
	watch.turning.Store(true)
	if got := a.tabSignalFor("/tmp/lab/two.jsonl", false); got != tabWorking {
		t.Fatalf("a conversation part way through a turn reads %v", got)
	}
	if mark := tabSignalGlyph(tabWorking, false); mark != tokens.GlyphWorking {
		t.Fatalf("working drew %q, want the mark every other surface uses", mark)
	}
}

// AND A QUESTION OUTRANKS IT. A conversation can be both at once, and the half a
// person can act on is the half worth the cell.
func TestAQuestionOutranksWorkOnTheSameTab(t *testing.T) {
	a, watch, _ := signalLab(t)
	watch.turning.Store(true)
	watch.waits.Store(true)
	if got := a.tabSignalFor("/tmp/lab/two.jsonl", false); got != tabNeedsPerson {
		t.Fatalf("a conversation working AND asking reads %v, want the question", got)
	}
	if mark := tabSignalGlyph(tabNeedsPerson, false); mark != tokens.GlyphNeedsHuman {
		t.Fatalf("a question drew %q, want the mark every other surface uses", mark)
	}
}

// A CONVERSATION THIS WINDOW ONLY REMEMBERS CLAIMS NOTHING. Over a shared engine
// handle the far side holds one conversation at a time and ended the others, and
// a strip drawing them as live would be inventing every one of them.
func TestAConversationThisWindowOnlyRemembersClaimsNothing(t *testing.T) {
	a, _, _ := signalLab(t)
	if got := a.tabSignalFor("/tmp/lab/three.jsonl", false); got != tabIdle {
		t.Fatalf("a conversation with no agent behind it reads %v", got)
	}
	if got := a.tabSignalFor("/tmp/lab/never-seen.jsonl", false); got != tabIdle {
		t.Fatalf("a conversation this window never held reads %v", got)
	}
}

// ── THE READING IS FREE ─────────────────────────────────────────────────────

// THE STRIP ASKS NO FILE AND NO LOCK. [session.Agent.NeedsPerson] takes the
// agent's mutex and allocates a map; [session.Agent.TaskIndex] reads a file, or
// over `--host` calls another machine. The strip is laid out on every frame, so
// either one here would be that cost thirty times a second per tab.
func TestTheMarksAskNeitherTheDiskNorTheAgentsLock(t *testing.T) {
	a, watch, held := signalLab(t)
	watch.turning.Store(true)
	for i := 0; i < 200; i++ {
		a.tabSignalFor("/tmp/lab/two.jsonl", false)
		a.tabSignalFor("/tmp/lab/three.jsonl", false)
		a.tabSignalFor(a.file, true)
	}
	if n := held.asked.Load(); n != 0 {
		t.Fatalf("six hundred marks asked the agent whether it needs a person %d times", n)
	}
	if n := held.indexed.Load(); n != 0 {
		t.Fatalf("six hundred marks read the task index %d times", n)
	}
}

// AND THE WATCHER FILLS THE CACHE AT THE EDGES IT WAS ALREADY WATCHING. This is
// the other half of the test above: the reading is free BECAUSE the loop that
// was already computing these two facts writes them down.
func TestTheWatcherRecordsWhatTheStripReads(t *testing.T) {
	waiting := &countingAgent{fakeAgent: &fakeAgent{model: "m"}, waiting: true}
	out := make(chan behindStirMsg, stirDepth)
	watch := startBehindWatch("/tmp/lab/two.jsonl", waiting, out)
	defer watch.stop()
	deadline := time.Now().Add(2 * time.Second)
	for !watch.waits.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !watch.waits.Load() {
		t.Fatal("the watcher never recorded that its conversation is waiting on a person")
	}
}

// THE SAME CONVERSATION MUST NOT CHANGE ITS ANSWER WHEN IT MOVES. A standing
// answer is held as needing a person, and the surface already carries that
// question when the conversation comes forward. An automatic proposal remains
// excluded on both sides because its deadline will answer it.
func TestTheSameConversationAgreesInFrontAndBehind(t *testing.T) {
	a, watch, _ := signalLab(t)
	watch.waits.Store(true)
	held := a.tabSignalFor("/tmp/lab/two.jsonl", false)
	a.questions = append(a.questions, questionShown{question: session.Question{
		ID: 7, Kind: session.QuestionStanding,
	}})
	if front := a.tabSignalFor(a.file, true); front != held {
		t.Fatalf("landed your-call conversation reads %v in front and %v behind", front, held)
	}

	watch.waits.Store(false)
	a.questions = nil
	a.task = &taskCard{id: 21, title: "Repair the reporting pipeline",
		born: a.now(), deadline: a.now().Add(10 * time.Second)}
	if front, held := a.tabSignalFor(a.file, true), a.tabSignalFor("/tmp/lab/two.jsonl", false); front != held {
		t.Fatalf("deadline proposal conversation reads %v in front and %v behind", front, held)
	}
}

// ── AND WHAT IS NOT A QUESTION ──────────────────────────────────────────────
//
// This is the one that has to be right before a mark is worth drawing at all.

// AN AUTOMATIC PROPOSAL IS A COUNTDOWN AND NOT A QUESTION. It proceeds whether
// or not anybody looks at it, which is exactly what the engine's own reading
// says about it (session's [Agent.waitingOnPerson] skips a proposal carrying a
// deadline) — and a strip that spelled every internal wait `?` would put an
// amber question mark on every tab at once and teach a person to ignore the one
// that means them.
func TestAnAutomaticProposalIsNotAQuestionForThePerson(t *testing.T) {
	a, _, _ := signalLab(t)
	a.task = &taskCard{id: 21, title: "Repair the reporting pipeline",
		born: a.now(), deadline: a.now().Add(10 * time.Second)}
	if !a.awaitingTask() {
		t.Fatal("the fixture did not raise a proposal at all")
	}
	if got := a.frontSignal(); got == tabNeedsPerson {
		t.Fatal("a proposal that proceeds on its own was drawn as needing the person")
	}
	// AND ONE WITH THE CLOCK OFF IS A QUESTION, which is the same law read the
	// other way: nothing happens until somebody answers.
	a.task = &taskCard{id: 21, title: "Repair the reporting pipeline", born: a.now()}
	if got := a.frontSignal(); got != tabNeedsPerson {
		t.Fatalf("a proposal with no clock on it reads %v, want the question", got)
	}
}

// AND WORK IN FLIGHT IN THE CONVERSATION ON SCREEN IS WORK. The front tab has no
// watcher — coming forward stops it — so its answer comes off the surface, and
// it has to mean the same thing as the cached one.
func TestTheConversationInFrontReadsItsOwnState(t *testing.T) {
	a, _, _ := signalLab(t)
	if got := a.tabSignalFor(a.file, true); got != tabIdle {
		t.Fatalf("an idle conversation in front reads %v", got)
	}
	a.state = stateWorking
	if got := a.tabSignalFor(a.file, true); got != tabWorking {
		t.Fatalf("a conversation running a turn reads %v", got)
	}
	raiseAsk(a, 7, "bash")
	if got := a.tabSignalFor(a.file, true); got != tabNeedsPerson {
		t.Fatalf("a conversation holding a question reads %v", got)
	}
}

// ── THE CELL IT IS DRAWN IN ─────────────────────────────────────────────────

// THE SLOT IS THE SAME WIDTH IN EVERY STATE. A mark that appeared and
// disappeared would move every name to its right by two cells each time a turn
// started — on a row a person reaches for by position, which is the one thing
// tabs may not do.
func TestTheSlotIsTheSameWidthInEveryState(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		want := -1
		for _, sig := range []tabSignal{tabIdle, tabWorking, tabNeedsPerson} {
			got := ansi.StringWidth(tabSignalSlot(sig, ascii))
			if want < 0 {
				want = got
			}
			if got != want {
				t.Fatalf("ascii=%v: state %v draws %d cells, and its neighbours draw %d", ascii, sig, got, want)
			}
		}
		if want != tabSignalWidth {
			t.Fatalf("ascii=%v: the slot is %d cells and [tabSignalWidth] says %d", ascii, want, tabSignalWidth)
		}
	}
}

// AND A TERMINAL WITH NO BOX CHARACTERS STILL TELLS THE THREE APART. `?` is
// already ASCII; the half-filled circle takes the stand-in this surface uses for
// everything that is running.
func TestTheMarksSurviveATerminalWithNoBoxCharacters(t *testing.T) {
	seen := map[string]tabSignal{}
	for _, sig := range []tabSignal{tabWorking, tabNeedsPerson} {
		mark := tabSignalGlyph(sig, true)
		if mark == "" {
			t.Fatalf("%v drew nothing in ascii", sig)
		}
		if strings.ContainsFunc(mark, func(r rune) bool { return r > 127 }) {
			t.Fatalf("%v drew %q, which is not ascii", sig, mark)
		}
		if other, clash := seen[mark]; clash {
			t.Fatalf("%v and %v both draw %q", sig, other, mark)
		}
		seen[mark] = sig
	}
}

// ── WORK THAT OUTLIVES THE TURN THAT STARTED IT ─────────────────────────────
//
// A task node runs in its own worktree under its own agent. The turn that
// proposed it ends the moment the proposal is answered, so a strip that asked
// only "is a turn in flight" drew the busiest conversation in the window at
// rest — for the whole of the minutes the work actually took. These hold both
// halves of the fix, and the line neither half may cross: it is the WORKING
// mark, never the question.

// taskLaneAgent is an agent whose task lane a test drives by hand, which is how
// the watcher's own loop can be held to what it does with a notice.
type taskLaneAgent struct {
	*fakeAgent
	lane chan session.Event
}

func (t *taskLaneAgent) WatchTaskUpdates() (<-chan session.Event, func()) {
	return t.lane, func() {}
}

// running and settled are the two notices these tests send, said once.
func signalRunning(id uint64) session.Event {
	return session.Event{Kind: session.EventTaskUpdate,
		Task: &session.TaskNotice{ID: id, Title: "Repair the reporting pipeline", State: session.TaskRunning}}
}

func signalSettled(id uint64, state session.TaskState) session.Event {
	return session.Event{Kind: session.EventTaskUpdate,
		Task: &session.TaskNotice{ID: id, Title: "Repair the reporting pipeline", State: state}}
}

// THE CONVERSATION IN FRONT IS STILL WORKING WHEN ITS TURN IS OVER. Its roster
// is the one this surface already keeps for the update de-dup ([app.taskSeen]),
// and the state in it is the whole of the reading.
func TestBackgroundWorkKeepsTheFrontTabWorkingAfterTheTurnEnds(t *testing.T) {
	a, _, _ := signalLab(t)
	a.state = stateIdle
	if got := a.frontSignal(); got != tabIdle {
		t.Fatalf("a conversation with no work at all reads %v", got)
	}
	a.taskSeen = map[uint64]session.TaskState{21: session.TaskRunning}
	if got := a.tabSignalFor(a.file, true); got != tabWorking {
		t.Fatalf("a finished turn with a node still running reads %v, want work", got)
	}
	// AND A NODE WAITING ON A DEPENDENCY IS WORK TOO. Nobody is being asked
	// anything; something is simply happening.
	a.taskSeen = map[uint64]session.TaskState{21: session.TaskQueued}
	if got := a.frontSignal(); got != tabWorking {
		t.Fatalf("a node held on the frontier reads %v, want work", got)
	}
}

// AND A SETTLED ROSTER SAYS NOTHING AGAIN. Each of the three settled states puts
// the tab back to the emptiness law on its own.
func TestASettledRosterTakesTheMarkOffTheFrontTab(t *testing.T) {
	a, _, _ := signalLab(t)
	a.state = stateIdle
	for _, state := range []session.TaskState{session.TaskDone, session.TaskFailed, session.TaskUnverified} {
		a.taskSeen = map[uint64]session.TaskState{21: state}
		if got := a.frontSignal(); got != tabIdle {
			t.Fatalf("a node that ended %v reads %v, want nothing at all", state, got)
		}
	}
	// ONE NODE ENDING DOES NOT END THE OTHERS, which is the reading a count
	// would get wrong: a node publishes `running` many times and settles once.
	a.taskSeen = map[uint64]session.TaskState{21: session.TaskDone, 22: session.TaskRunning}
	if got := a.frontSignal(); got != tabWorking {
		t.Fatalf("one node done and one still running reads %v, want work", got)
	}
}

// A HELD CONVERSATION READS THE SAME FACT OFF THE WATCHER, and the two have to
// agree: a conversation may not change what it claims by being switched to.
func TestBackgroundWorkKeepsAHeldTabWorkingAfterItsTurnEnds(t *testing.T) {
	a, watch, _ := signalLab(t)
	if !watch.noteTask(signalRunning(21).Task) {
		t.Fatal("the first running node did not move the cached reading")
	}
	// THE TURN IS OVER — this is exactly the state the old strip drew as idle.
	watch.turning.Store(false)
	if got := a.tabSignalFor("/tmp/lab/two.jsonl", false); got != tabWorking {
		t.Fatalf("a held conversation with a node still running reads %v, want work", got)
	}
	// AND A QUESTION STILL OUTRANKS IT. Background work is the quieter claim.
	watch.waits.Store(true)
	if got := a.tabSignalFor("/tmp/lab/two.jsonl", false); got != tabNeedsPerson {
		t.Fatalf("a held conversation working AND asking reads %v, want the question", got)
	}
}

// AND A TERMINAL NOTICE CLEARS IT. This is the half that goes wrong quietly: a
// mark that goes up correctly and never comes down is worse than no mark.
func TestATerminalNoticeClearsTheCachedWork(t *testing.T) {
	for _, state := range []session.TaskState{session.TaskDone, session.TaskFailed, session.TaskUnverified} {
		a, watch, _ := signalLab(t)
		watch.noteTask(signalRunning(21).Task)
		if !watch.noteTask(signalSettled(21, state).Task) {
			t.Fatalf("%v did not move the cached reading back", state)
		}
		if watch.tasking.Load() {
			t.Fatalf("a node that ended %v left the conversation marked as working", state)
		}
		if got := a.tabSignalFor("/tmp/lab/two.jsonl", false); got != tabIdle {
			t.Fatalf("a held conversation whose only node ended %v reads %v", state, got)
		}
	}
}

// THE EDGE IS THE CONVERSATION'S, NOT THE NODE'S. A graph publishing a node a
// second must not wake the surface a second, and the last node settling is the
// only clearing that counts.
func TestOnlyTheConversationsOwnEdgeIsWorthAStir(t *testing.T) {
	_, watch, _ := signalLab(t)
	if !watch.noteTask(signalRunning(21).Task) {
		t.Fatal("the first node did not raise the edge")
	}
	if watch.noteTask(signalRunning(21).Task) {
		t.Fatal("the same node saying `running` again raised a second edge")
	}
	if watch.noteTask(signalRunning(22).Task) {
		t.Fatal("a second node starting raised an edge on a conversation already working")
	}
	if watch.noteTask(signalSettled(21, session.TaskDone).Task) {
		t.Fatal("one node of two ending cleared the conversation")
	}
	if !watch.tasking.Load() {
		t.Fatal("a conversation with one node left running stopped claiming work")
	}
	if !watch.noteTask(signalSettled(22, session.TaskDone).Task) {
		t.Fatal("the last node ending did not raise the edge")
	}
	if watch.tasking.Load() {
		t.Fatal("every node ended and the conversation still claims work")
	}
}

// A STATE THIS SURFACE CANNOT READ IS NOT EVIDENCE THAT WORK STOPPED. A
// proposal arrives on this lane before its node exists and carries no state at
// all; treating "not a state I know" as settled is how a strip goes dark on a
// conversation that is still working. A background job is left out for the
// reason the roster leaves it out — it is counted in its own place.
func TestAnUnreadableNoticeLeavesTheReadingAlone(t *testing.T) {
	_, watch, _ := signalLab(t)
	watch.noteTask(signalRunning(21).Task)
	for _, notice := range []*session.TaskNotice{
		nil,
		{ID: 21},
		{ID: 21, State: session.TaskRunning, Kind: session.TaskKindJob},
		{ID: 22, State: session.TaskDone, Kind: session.TaskKindJob},
	} {
		if watch.noteTask(notice) {
			t.Fatalf("a notice this surface cannot read (%+v) moved the reading", notice)
		}
	}
	if !watch.tasking.Load() {
		t.Fatal("a running node was forgotten by a notice that said nothing about it")
	}
}

// AND THE WATCHER'S OWN LOOP DOES THE FOLDING, off the lane it was already
// draining. This is the other half of "the reading is free": no file, no lock,
// no call to another machine — one map write on an event that arrived anyway.
func TestTheWatcherFoldsTaskNoticesOffTheLaneItAlreadyDrains(t *testing.T) {
	lane := make(chan session.Event, 4)
	agent := &taskLaneAgent{fakeAgent: &fakeAgent{model: "m"}, lane: lane}
	out := make(chan behindStirMsg, stirDepth)
	watch := startBehindWatch("/tmp/lab/two.jsonl", agent, out)
	defer watch.stop()

	lane <- signalRunning(21)
	await := func(t *testing.T, want bool, what string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for watch.tasking.Load() != want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if watch.tasking.Load() != want {
			t.Fatalf("the watcher never recorded that %s", what)
		}
	}
	await(t, true, "its conversation has work running behind the turn")
	select {
	case note := <-out:
		if note.key != "/tmp/lab/two.jsonl" {
			t.Fatalf("the stir named %q", note.key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("work starting behind a finished turn never asked the surface to look")
	}

	lane <- signalSettled(21, session.TaskDone)
	await(t, false, "the work behind the turn ended")
}

// ── ONE PREDICATE ON BOTH SIDES OF THE STRIP (#1316) ─────────────────────────

// frontAskAgent answers the engine's one question about a person from a flag a
// test can move while a watcher is reading it, and counts every time it is
// asked, which is how a frame that asks the agent is caught.
type frontAskAgent struct {
	*fakeAgent
	waits atomic.Bool
	asked atomic.Int64
}

func (g *frontAskAgent) NeedsPerson() bool {
	g.asked.Add(1)
	return g.waits.Load()
}

// THE SAME CONVERSATION WEARS THE SAME MARK IN FRONT AND BEHIND. The tab beside
// the one in front read [session.Agent.NeedsPerson], which counts a landed
// `your call`, the model's own blocking question, the sub-harness intake card
// and a running sub-harness's question; the tab in front read the surface's own
// short list, which counted none of them. So a `?` stood on the tab beside, and
// went away the moment a person brought that conversation forward to answer it.
// This drives ONE agent in one state through both readings, and the answer is
// the engine's on both sides, with the frame still asking the agent nothing.
func TestOneConversationWearsTheSameMarkInFrontAndBehind(t *testing.T) {
	agent := &frontAskAgent{fakeAgent: &fakeAgent{model: "m"}}
	agent.waits.Store(true)

	out := make(chan behindStirMsg, stirDepth)
	watch := startBehindWatch("/tmp/lab/two.jsonl", agent, out)
	deadline := time.Now().Add(2 * time.Second)
	for !watch.waits.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	watch.stop()
	behind := watch.signal()
	if behind != tabNeedsPerson {
		t.Fatalf("the held tab reads %v for a conversation the engine says is waiting on a person", behind)
	}

	a := newTestApp(agent)
	a.file, a.workspace = "/tmp/lab/two.jsonl", "/tmp/lab"
	emptyMachine(a)
	drive(t, a, frameMsg{})
	if front := a.tabSignalFor(a.file, true); front != behind {
		t.Fatalf("one conversation reads %v in front and %v behind", front, behind)
	}

	// THE FRAME READS WHAT THE LOOP WROTE DOWN. The strip is laid out on every
	// frame, and the agent's predicate takes its lock.
	before := agent.asked.Load()
	for i := 0; i < 200; i++ {
		a.tabSignalFor(a.file, true)
	}
	if n := agent.asked.Load() - before; n != 0 {
		t.Fatalf("two hundred front marks asked the agent %d times", n)
	}

	// AND AN ANSWER TAKES THE MARK OFF THE FRONT TAB AT THE NEXT MESSAGE.
	agent.waits.Store(false)
	drive(t, a, frameMsg{})
	if front := a.tabSignalFor(a.file, true); front == tabNeedsPerson {
		t.Fatal("the front tab kept its question after the engine stopped waiting")
	}
}
