package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE STOP, AND THE SECONDS AFTER IT.
//
// [app.interrupt] has always been instant in the session — the turn's context is
// cancelled on the keystroke — and the question these tests are about is whether
// the SCREEN was. It was not: the engine keeps the stream open until its turn
// loop lets go, which for a `bash` holding a leaked pipe or a `jobs` kill is
// three or four seconds, and for the whole of that window the surface went on
// drawing what arrived. What is pinned here is the law that closed it: the frame
// after esc shows no motion, claims "working" nowhere, and draws nothing new
// until the turn is over ([app.windingDown], [keptAfterStop], [stoppingWord]).

// stoppingApp is a turn caught mid-flight, with the two things on screen that a
// stop is most likely to interrupt: a tool that is running and a reply that is
// still arriving.
func stoppingApp(t *testing.T) (*app, *fakeAgent) {
	t.Helper()
	a, agent, _ := stoppingAppOnAHeldClock(t)
	return a, agent
}

// stoppingAppOnAHeldClock is the same turn WITH THE CLOCK IN THE TEST'S HAND,
// the way stopbound_test.go's [boundedStopApp] holds its own.
//
// THE HEAD DRAWS THE TIME OF DAY ON EVERY FRAME (pulse.go's [pulseClock], off
// [app.now]), so a test that compares two whole frames is comparing the wall
// clock too whether it meant to or not. Left on [time.Now] that comparison
// fails whenever its two captures straddle a minute boundary, which is about
// three quarters of one percent of runs at these durations: measured at one
// failure in eighty one, and read on a pull request as a regression in a change
// that could not reach the path (#1370).
//
// THE CLOCK IS PINNED RATHER THAN THE HEAD BEING EXCLUDED FROM THE COMPARISON,
// because the comparison catching ANY drawing after a stop is the whole of what
// these tests are for, and an exclusion would buy the same green by making the
// assertion weaker. [TestTheStoppedFramesComparisonStillCoversTheHead] is what
// holds that line.
//
// It is pinned HERE and not in [newTestApp], which has calling files in the
// hundreds: pinning it there would change the observable time for every test in
// this package at once, including the ones asserting on elapsed durations and
// relative words, which is a package-wide behaviour change and not a flake fix.
func stoppingAppOnAHeldClock(t *testing.T) (*app, *fakeAgent, func(time.Duration)) {
	t.Helper()
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	now := time.Now()
	a.clock = func() time.Time { return now }
	drive(t, a, submittedMsg{ch: make(chan session.Event)})
	a.state = stateWorking
	a.turnBegan = a.now()
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolAnnounced,
			Tool: "bash", CallID: "c1", Hint: "go test ./..."}},
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolBegin,
			Tool: "bash", CallID: "c1"}},
		streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "half a sentence")},
	)
	return a, agent, func(d time.Duration) { now = now.Add(d) }
}

// ── 1. the frame stills on the key ──────────────────────────────────────────

// THE FIRST FRAME AFTER esc HAS NOTHING MOVING ON IT. The status line's spinner,
// the tool row's spinner and the tool row's climbing clock are all drawn only
// inside stateWorking, so the state word leaving is what stills them — and the
// end stamp [app.interrupt] writes is what keeps them stilled whatever the
// session does next.
func TestTheFrameStillsOnTheKeyAndClaimsWorkingNowhere(t *testing.T) {
	a, agent := stoppingApp(t)
	before := plain(frame(a))
	if !strings.Contains(before, stateWorking.String()) {
		t.Fatalf("the turn was not working before the key:\n%s", before)
	}

	drive(t, a, key("esc"))
	if agent.stops != 1 {
		t.Fatalf("esc did not stop the turn (%d)", agent.stops)
	}
	after := plain(frame(a))
	if strings.Contains(after, stateWorking.String()) {
		t.Fatalf("the frame after esc still says working:\n%s", after)
	}
	// THE SPINNER IS THE MOTION, and there are two of it on this frame — the
	// status line's and the tool row's. Neither cell may survive the key: a still
	// frame with one thing turning in it is the surface insisting on something
	// the person has just ended.
	if strings.ContainsAny(after, spinnerFrames()) {
		t.Fatalf("a spinner is still turning after esc:\n%s", after)
	}
	// AND THE ROW CARRIES ITS OWN END, rather than merely being drawn quietly
	// because the session happens not to be working.
	row := toolRow(t, a)
	if row.ended.IsZero() {
		t.Fatal("the running call was left with no end on it")
	}
}

// The status line says what is happening in the window the engine's teardown
// takes, and gives the word up for the fact the moment the stream closes.
func TestTheStatusLineSaysStoppingUntilTheStreamCloses(t *testing.T) {
	a, _ := stoppingApp(t)
	drive(t, a, key("esc"))

	if !a.windingDown() {
		t.Fatal("the surface is not winding down after esc")
	}
	word, painted := a.stateWord()
	if word != stoppingWord {
		t.Fatalf("the status line says %q, want %q", word, stoppingWord)
	}
	// THE DIM VOICE AND NOT THE ALARM. Nothing is wrong and nothing is wanted.
	if painted != a.pal.dim(stoppingWord) {
		t.Fatal("the stopping word is not in the dim voice")
	}
	if got := plain(frame(a)); !strings.Contains(got, stoppingWord) {
		t.Fatalf("the frame does not carry the word:\n%s", got)
	}

	drive(t, a, streamClosedMsg{gen: a.gen})
	if a.windingDown() {
		t.Fatal("the surface is still winding down after the close")
	}
	if word, _ := a.stateWord(); word != stateInterrupted.String() {
		t.Fatalf("the closed turn says %q, want %q", word, stateInterrupted.String())
	}
}

// ── 2. nothing new is drawn after the stop ──────────────────────────────────

// THE DEFECT THIS PINS. The engine goes on speaking while it winds the turn
// down, and every word of it used to land: a delta arriving after the note found
// no live block — the note had closed it — and opened a SECOND assistant block
// UNDERNEATH the `interrupted` line, so what a person read was a model carrying
// on after they stopped it. A call the model was half-way through spelling out
// drew a fresh tool row in the same place, for work that was never going to run.
func TestNothingArrivingAfterTheStopIsDrawn(t *testing.T) {
	a, _ := stoppingApp(t)
	drive(t, a, key("esc"))
	was := len(a.entries)
	said := stoppedFrame(a)

	drive(t, a,
		streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, " and one more clause")},
		streamEventMsg{gen: a.gen, ev: text(session.EventReasoning, "still thinking")},
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolForming,
			Tool: "write", CallID: "c2", ArgsText: `{"path":"notes.go"`}},
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolAnnounced,
			Tool: "write", CallID: "c2", Hint: "write notes.go"}},
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventNudge, Hint: "stuck? nudged · write"}},
	)
	if got := len(a.entries); got != was {
		t.Fatalf("the conversation grew from %d blocks to %d after the stop", was, got)
	}
	if got := stoppedFrame(a); got != said {
		t.Fatalf("the frame moved after the stop:\nwas:\n%s\nnow:\n%s", said, got)
	}
}

// stoppedFrame is WHAT THE TEST ABOVE COMPARES, and the reason it has a name is
// that it must stay the WHOLE frame.
//
// Narrowing it is the cheap way to stop that test flaking and it costs the test
// most of what it is for: comparing everything is how it catches drawing
// nobody predicted, which is the only kind a stop leaks.
// [TestTheStoppedFramesComparisonStillCoversTheHead] is red the moment this
// stops covering the head, which is the part that was moving.
//
// AND IT EXISTS IN ORDER TO BE SHARED. The guard is a guard only because it
// calls the same function the real test calls, so inlining this back into its
// callers reads like removing a pointless indirection, leaves every test
// green, and detaches the guard from the comparison it guards in the same
// stroke. Keep the call.
func stoppedFrame(a *app) string { return plain(frame(a)) }

// AND THE FRAME ABOVE IS COMPARED AGAINST A CLOCK THAT DOES NOT MOVE ON ITS
// OWN. Two readings of this surface's time taken one after the other are the
// same reading, so nothing in the head can differ between two captures that
// nothing happened between.
//
// This is the test that is red without the pinned clock, and it is red at
// nanosecond resolution rather than at the minute boundary, which is the whole
// point: the defect it stands for only SHOWS itself about once in eighty one
// runs, and a check that can only fail that often is not a check.
func TestTheStoppedSurfaceReadsAClockThatDoesNotMoveOnItsOwn(t *testing.T) {
	a, _ := stoppingApp(t)
	drive(t, a, key("esc"))

	first := a.now()
	if second := a.now(); !second.Equal(first) {
		t.Fatalf("two readings of the stopped surface's clock differ by %v, so the head can move between two frames nothing happened between", second.Sub(first))
	}
	was := stoppedFrame(a)
	if got := stoppedFrame(a); got != was {
		t.Fatalf("two frames of a still surface differ:\nwas:\n%s\nnow:\n%s", was, got)
	}
}

// AND THE COMPARISON STILL COVERS THE HEAD, which is the guard on the FIX
// rather than on the product.
//
// There are two ways to make [TestNothingArrivingAfterTheStopIsDrawn] stop
// flaking and they produce the same green: pin the clock, or stop comparing the
// part of the frame the clock is drawn in. The second one also stops that test
// noticing a real change to the head after a stop, which is inside what it
// exists to catch. So this drives the clock across a minute boundary and
// insists the compared frame SEES it.
func TestTheStoppedFramesComparisonStillCoversTheHead(t *testing.T) {
	a, _, elapse := stoppingAppOnAHeldClock(t)
	drive(t, a, key("esc"))

	was := stoppedFrame(a)
	elapse(time.Minute)
	got := stoppedFrame(a)
	if got == was {
		t.Fatalf("a minute passed and the compared frame did not move, so the head is no longer inside the comparison:\n%s", got)
	}
	wasHead, gotHead := firstLine(was), firstLine(got)
	if wasHead == gotHead {
		t.Fatalf("the frame moved somewhere other than the head, which is not the field this guard is about:\nwas:\n%s\nnow:\n%s", was, got)
	}
}

// A CALL THAT WAS ALREADY DRAWN STILL GETS ITS ENDING. The close cannot open
// anything — it writes into the row that is on screen — and a `go test` that
// finished in the instant before the cancel reached it is owed the result it
// actually produced rather than standing forever as a call nobody knows the end
// of ([keptAfterStop]).
func TestALateToolCloseStillLandsOnTheRowItBelongsTo(t *testing.T) {
	a, _ := stoppingApp(t)
	drive(t, a, key("esc"))
	was := len(a.entries)

	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolEnd,
		Tool: "bash", Output: "ok  	 	0.4s"}})
	if got := len(a.entries); got != was {
		t.Fatalf("the close opened a block: %d blocks, was %d", got, was)
	}
	row := toolRow(t, a)
	if row.status != toolOK {
		t.Fatalf("the row says %v, want the result the call reported", row.status)
	}
	if row.detail.Output == "" {
		t.Fatal("the call's own output was thrown away")
	}
}

// AND THE MONEY IS STILL COUNTED. A turn that was stopped spent what it spent,
// and the usage rides on the two events that end one.
func TestTheStoppedTurnStillTakesItsUsage(t *testing.T) {
	a, _ := stoppingApp(t)
	drive(t, a, key("esc"))

	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone,
		Usage: session.Usage{Input: 900, Output: 100, CostUSD: 0.25}}})
	if a.cost != 0.25 {
		t.Fatalf("the stopped turn's cost is %v, want what it spent", a.cost)
	}
	if a.tokens != 1000 {
		t.Fatalf("the stopped turn's tokens are %d, want what it spent", a.tokens)
	}
}

// ── 3. the esc mash ─────────────────────────────────────────────────────────

// THE EVERYDAY GESTURE: a person mashes esc at a turn they want stopped. The
// first one stops it; the second, inside [rewindArmWindow], opens the rewind
// over a conversation they were not thinking about cutting; the third leaves.
//
// NOTHING DESTRUCTIVE CAN COME OF IT. The mode does nothing without enter, esc
// puts the sentence they were typing back exactly as it was, and the engine is
// never asked to cut anything.
func TestMashingEscStopsTheTurnAndLeavesTheConversationWhole(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	drive(t, a, submittedMsg{ch: make(chan session.Event)})
	a.state = stateWorking
	for _, r := range "half a thought" {
		drive(t, a, key(string(r)))
	}

	drive(t, a, key("esc"))
	if agent.stops != 1 {
		t.Fatalf("the first esc did not stop the turn (%d)", agent.stops)
	}
	// Counted AFTER the stop, because the stop writes its own `interrupted` line
	// and that line is the one thing a mash is supposed to leave behind.
	before := len(a.entries)
	drive(t, a, key("esc"))
	if !a.rew.on {
		t.Fatal("the second esc did not open the rewind")
	}
	// THE MODE OPENS CALM. Nothing has been cut, and the draft is being held
	// rather than spent.
	if len(agent.cuts) != 0 {
		t.Fatalf("opening the mode cut the conversation at %v", agent.cuts)
	}
	if got := string(a.rew.draft); got != "half a thought" {
		t.Fatalf("the mode is holding %q, want the sentence in the box", got)
	}

	drive(t, a, key("esc"))
	if a.rew.on {
		t.Fatal("the third esc did not leave the mode")
	}
	if len(agent.cuts) != 0 {
		t.Fatalf("mashing esc cut the conversation at %v", agent.cuts)
	}
	if got := string(a.input.value); got != "half a thought" {
		t.Fatalf("the sentence came back as %q", got)
	}
	if len(a.entries) != before {
		t.Fatalf("mashing esc changed the conversation: %d blocks, was %d", len(a.entries), before)
	}
	// AND THE STOP IS STILL THE STOP. The rewind rode on top of it and took
	// nothing from it.
	if agent.stops != 1 {
		t.Fatalf("the mash stopped the turn %d times", agent.stops)
	}
}

// A MASH THAT KEEPS GOING IS STILL SAFE. Past the third key the pair starts over
// — arm, open, leave — and the sentence survives every round of it.
func TestAnEndlessEscMashNeverCutsAnything(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	drive(t, a, submittedMsg{ch: make(chan session.Event)})
	a.state = stateWorking
	for _, r := range "keep me" {
		drive(t, a, key(string(r)))
	}

	for range 9 {
		drive(t, a, key("esc"))
	}
	if len(agent.cuts) != 0 {
		t.Fatalf("nine escs cut the conversation at %v", agent.cuts)
	}
	// The mode is either up holding the sentence or down with it back in the
	// box; both are the same promise, and one of the two is always true.
	held := string(a.input.value)
	if a.rew.on {
		held = string(a.rew.draft)
	}
	if held != "keep me" {
		t.Fatalf("the sentence is %q after nine escs", held)
	}
}

// ── 4. the same law in a task's room ────────────────────────────────────────

// THE ROOM SAYS THE SAME WORD. Stopping a node is not esc — in a room esc is the
// door and never a stop (stop.go) — but the window after the answer is the
// conversation's window exactly: the child's context is cut and the child is
// winding up, and for the whole of it this header used to read "working" about
// work the person had just ended.
func TestAStoppedNodesRoomSaysStoppingRatherThanWorking(t *testing.T) {
	a, _ := stopApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{Stopped: true})})
	node := a.tasks[7]
	if node == nil || !node.stopped {
		t.Fatal("the surface did not keep who ended the work")
	}
	if got := a.roomStateWord(node); got != stoppingWord {
		t.Fatalf("the header calls a stopping node %q, want %q", got, stoppingWord)
	}
	// And through the line a person actually reads.
	a.room = a.newRoom(7, "Fix the nil-map crash")
	head := plain(roomHeadAll(a, 120))
	if !strings.Contains(head, stoppingWord) || strings.Contains(head, stateWorking.String()) {
		t.Fatalf("the room header is %q", head)
	}
	// AND IT LANDS ON THE FACT. Once the engine has moved the node the word is
	// the one the roster and the card already spell it with.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskFailed, session.TaskNotice{Stopped: true, Merge: "aborted"})})
	if got := a.roomStateWord(a.tasks[7]); got != taskStoppedByPerson {
		t.Fatalf("the landed node says %q, want %q", got, taskStoppedByPerson)
	}
}

// toolRow is the one tool block these tests put on the screen.
func toolRow(t *testing.T, a *app) *entry {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			return &a.entries[i]
		}
	}
	t.Fatal("there is no tool row on the screen")
	return nil
}

// spinnerFrames is every cell the status line and the tool rows can be turning,
// as one string a frame can be tested against. It is taken from the token set
// rather than written down, because a spinner whose frames were listed twice is
// a test that stops looking at the animation the moment somebody changes it.
func spinnerFrames() string {
	var out strings.Builder
	for i := range 16 {
		out.WriteString(tokens.Spinner(i))
	}
	return out.String()
}
