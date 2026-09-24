package tui3

// A program's room (programroom.go), driven through the surface's own loop: the
// box that sends nothing and says so, the brief's fold, the page that follows
// the run on the paint clock and stops when it settles, the stop that goes
// through the store's own door, the room at a phone's width, and the one clock
// the room, the rail and the landed card all read for one run.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// programRoomAgent is the plan fixture with the room doors on it and a count of
// every page read and every line sent to a node, so a test can say what a
// gesture asked of the engine.
type programRoomAgent struct {
	*railPlanCounter
	steered []string
}

func (f *programRoomAgent) SteerTask(_ uint64, text string) (session.SteerReceipt, error) {
	f.steered = append(f.steered, text)
	return session.SteerReceipt{}, nil
}

// programRoomApp is a window in the conversation "the run" whose task 7 was
// handed to senior-dev. The rows are spelled the way the store spells them
// (`t-7`, session's planStoreID); the page is keyed by the number the room
// reads it by. The rail's row for the node started at [programRunBegan].
func programRoomApp(t *testing.T, width, height int) (*app, *programRoomAgent) {
	t.Helper()
	row := programRow()
	page := programPage(row, programTurns())
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{"7": page})
	agent := &programRoomAgent{railPlanCounter: &railPlanCounter{planFake: fake}}
	a.agent = agent
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.width, a.height = width, height
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, row.Title, session.TaskRunning, session.TaskNotice{StartedAt: programRunBegan})})
	readPlanRows(t, a)
	return a, agent
}

// openProgramRoomNow opens task 7's room by the card's door and lets its first
// read come back.
func openProgramRoomNow(t *testing.T, a *app) {
	t.Helper()
	a.openRoomFor(7, "rewrite the auth middleware")
	drain(t, a, a.takeRoomPump())
	if a.programOf() == nil {
		t.Fatalf("the card did not open the program's room: room=%v", a.room != nil)
	}
}

// THE BOX SENDS NOTHING AND SAYS SO. A program reads no message: the
// placeholder says it with the door the words can go through, and enter over a
// sentence sends it nowhere — not to the node, not to the store as a note —
// says the same line on the page, and leaves the sentence in the box.
func TestAProgramsRoomSendsNothingAndSaysSo(t *testing.T) {
	a, agent := programRoomApp(t, 120, 28)
	openProgramRoomNow(t, a)
	said := "senior-dev" + programRoomNoMessages + refusalGap + refusalMainDoor
	if frame, _, _ := a.frame(); !strings.Contains(plain(frame), said) {
		t.Fatalf("the empty box does not say %q:\n%s", said, plain(frame))
	}
	for _, r := range "pause it" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.steered) != 0 || len(agent.noted) != 0 {
		t.Fatalf("enter on a program's room sent %v to the node and %v to the store", agent.steered, agent.noted)
	}
	if got := a.input.String(); got != "pause it" {
		t.Fatalf("enter took the sentence out of the box: %q", got)
	}
	if !strings.Contains(roomText(a), said) {
		t.Fatalf("enter did not say %q on the page:\n%s", said, roomText(a))
	}
	// AND A SECOND ENTER DOES NOT SAY IT TWICE.
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if n := strings.Count(roomText(a), said); n != 1 {
		t.Fatalf("the page says the refusal %d times", n)
	}
}

// A LONG BRIEF FOLDS, AND ctrl+o OPENS AND CLOSES IT, in the room exactly as on
// the stored page: the fold line names how many lines and the key.
func TestCtrlOFoldsAProgramRoomsBrief(t *testing.T) {
	a, agent := programRoomApp(t, 120, 40)
	page := agent.planFake.pages["7"]
	page.Description = strings.Repeat("the store interface changes and every caller moves with it. ", 12)
	agent.planFake.pages["7"] = page
	openProgramRoomNow(t, a)
	fold := briefFoldWhat + railSep + briefFoldKey
	if !strings.Contains(roomText(a), fold) {
		t.Fatalf("a long brief is not folded with its key:\n%s", roomText(a))
	}
	drive(t, a, key("ctrl+o"))
	if strings.Contains(roomText(a), fold) {
		t.Fatalf("ctrl+o did not unfold the brief:\n%s", roomText(a))
	}
	drive(t, a, key("ctrl+o"))
	if !strings.Contains(roomText(a), fold) {
		t.Fatalf("a second ctrl+o did not fold the brief again:\n%s", roomText(a))
	}
}

// THE ROOM FOLLOWS THE RUN ON THE PAINT CLOCK AND STOPS WHEN IT SETTLES. While
// the run works, the page is read once a beat and no more; the landing is read
// once, so the room ends on the page the store ended on; after that no beat
// reads it again, and the clock that carried the reads stops turning for it.
func TestAProgramRoomFollowsWhileRunningAndStopsAfterItSettles(t *testing.T) {
	a, agent := programRoomApp(t, 120, 28)
	openProgramRoomNow(t, a)
	if !a.programRoomFollows() {
		t.Fatal("a room on a running program is not on the paint clock")
	}
	reads := agent.railPlanCounter.pages
	for range 5 {
		drive(t, a, frameMsg{})
	}
	if agent.railPlanCounter.pages != reads {
		t.Fatalf("frames inside one beat read the page %d times", agent.railPlanCounter.pages-reads)
	}
	for beat := 1; beat <= 3; beat++ {
		planBeat(t, a)
		if got := agent.railPlanCounter.pages - reads; got != beat {
			t.Fatalf("after %d beats the page was read %d times, want once a beat", beat, got)
		}
	}
	// THE RUN LANDS: the store ends its root and the conversation's row settles.
	page := agent.planFake.pages["7"]
	page.Row.Status, page.Row.Ended = "done", a.now()
	page.Notes = []session.PlanTaskNote{{Body: "the work landed on branch senior-dev/auth"}}
	agent.planFake.pages["7"] = page
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, page.Row.Title, session.TaskDone,
		session.TaskNotice{StartedAt: programRunBegan, EndedAt: a.now()})})
	before := agent.railPlanCounter.pages
	drive(t, a, frameMsg{})
	if agent.railPlanCounter.pages != before+1 {
		t.Fatalf("the landing was read %d times, want once", agent.railPlanCounter.pages-before)
	}
	if !strings.Contains(roomText(a), "the work landed on branch senior-dev/auth") {
		t.Fatalf("the room did not end on the page the store ended on:\n%s", roomText(a))
	}
	if !strings.Contains(roomText(a), roomFinishedRefusal.what) {
		t.Fatalf("a landed program's room draws no foot:\n%s", roomText(a))
	}
	if a.programRoomFollows() {
		t.Fatal("a settled program's room keeps the paint clock turning")
	}
	settled := agent.railPlanCounter.pages
	for range 3 {
		planBeat(t, a)
	}
	if agent.railPlanCounter.pages != settled {
		t.Fatalf("a settled program's room was read %d more times", agent.railPlanCounter.pages-settled)
	}
}

// THE STORE ENDS BEFORE THE ROW LANDS, AND THE ROOM KEEPS READING. The engine
// ends the store's root at the program's exit and writes the landing — where
// the work went and how to bring it in — after it, and only then settles the
// conversation's row. A read in that gap came back ended, took the room off the
// clock, and the landing's own notice found nothing left to read: the landed
// room never showed the note. The node's landing is what ends the room, and a
// read that was still out when it landed is not the last one.
func TestAProgramRoomReadsTheLandingTheStoreEndedAhead(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  bool // a beat's read is still out when the landing arrives
	}{
		{"the store's ending read on a beat", false},
		{"a read still out when the row lands", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, agent := programRoomApp(t, 120, 28)
			openProgramRoomNow(t, a)
			page := agent.planFake.pages["7"]
			page.Row.Status, page.Row.Ended = "done", a.now()
			agent.planFake.pages["7"] = page
			planBeat(t, a)
			if a.room.done || !a.programRoomFollows() {
				t.Fatalf("a store that ended ahead of the row took the room off the clock: done=%v", a.room.done)
			}
			var out tea.Cmd
			if tc.out {
				at := a.now().Add(elsewhereEvery)
				a.clock = func() time.Time { return at }
				out = a.programRoomFollow()
				if out == nil {
					t.Fatal("the beat issued no read")
				}
			}
			const landing = "the work landed on branch senior-dev/auth"
			page.Notes = []session.PlanTaskNote{{Body: landing}}
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, page.Row.Title, session.TaskDone,
				session.TaskNotice{StartedAt: programRunBegan, EndedAt: a.now()})})
			if tc.out {
				// The read that was out answers with the page before the landing.
				stale := page
				stale.Notes = nil
				agent.planFake.pages["7"] = stale
				drain(t, a, out)
			}
			agent.planFake.pages["7"] = page
			for range 2 {
				drive(t, a, frameMsg{})
			}
			if !strings.Contains(roomText(a), landing) {
				t.Fatalf("the landed room never shows the landing note:\n%s", roomText(a))
			}
			if a.programRoomFollows() {
				t.Fatal("a landed program's room keeps the paint clock turning")
			}
		})
	}
}

// STOP ON A PROGRAM'S ROOM IS THE RUN'S STOP, through the store's own door. `x`
// over an empty box and /stop both raise the card aimed at the run's own task
// by the store's id, the target the stored page's `x` has always raised, and
// saying yes cancels that task in the store — never a node cancel the run is
// not a node of.
func TestStopOnAProgramsRoomRaisesThePlanCard(t *testing.T) {
	for _, way := range []struct {
		name  string
		press func(t *testing.T, a *app)
	}{
		{"x over an empty box", func(t *testing.T, a *app) { drive(t, a, key(stopRaiseKey)) }},
		{"/stop", func(t *testing.T, a *app) {
			for _, r := range "/stop" {
				drive(t, a, key(string(r)))
			}
			drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
		}},
	} {
		t.Run(way.name, func(t *testing.T) {
			a, agent := programRoomApp(t, 120, 28)
			openProgramRoomNow(t, a)
			if frame, _, _ := a.frame(); !strings.Contains(plain(frame), roomStopMark) && !strings.Contains(plain(frame), roomStopMarkASCII) {
				t.Fatalf("the facts row offers no Stop:\n%s", plain(frame))
			}
			way.press(t, a)
			if a.stop == nil {
				t.Fatal("no stop card was raised")
			}
			if got := a.stop.target; got.plan != "t-7" || got.id != "" || got.noun != stopTaskNoun {
				t.Fatalf("the card is aimed at %+v, want the run's own task by the store's id", got)
			}
			drain(t, a, a.stopTake(0))
			if len(agent.cancelled) != 1 || agent.cancelled[0] != "t-7" {
				t.Fatalf("saying yes cancelled %v in the store, want [t-7]", agent.cancelled)
			}
		})
	}
	// A LANDED RUN OFFERS NO STOP.
	a, _ := programRoomApp(t, 120, 28)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "rewrite the auth middleware", session.TaskDone,
		session.TaskNotice{StartedAt: programRunBegan, EndedAt: a.now()})})
	openProgramRoomNow(t, a)
	if target := a.stopHere(); !target.empty() {
		t.Fatalf("a landed program's room offers a stop: %+v", target)
	}
}

// THE ROOM AT A PHONE'S WIDTH keeps the conversation's strip and the trail, its
// facts row keeps the step and the spend, the actions stand each step's word on
// a line of its own with its actions hung under it, and no row of the frame is
// wider than the frame.
func TestAProgramsRoomAtFortyFourColumns(t *testing.T) {
	a, _ := programRoomApp(t, 44, 30)
	openProgramRoomNow(t, a)
	frame, _, _ := a.frame()
	text := plain(frame)
	for _, want := range []string{"Home", "the run", "implement · $1.24", "IMPLEMENT", "edited internal/auth/"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the room at 44 columns lost %q:\n%s", want, text)
		}
	}
	for i, line := range strings.Split(frame, "\n") {
		if cells := ansi.StringWidth(line); cells > 44 {
			t.Fatalf("row %d is %d cells in a 44-cell frame: %q", i, cells, plain(line))
		}
	}
}

// ── ONE CLOCK FOR ONE RUN ───────────────────────────────────────────────────
//
// runclock_test.go holds the rail's anchor and the landed card's span on their
// own; this is the three surfaces read side by side for a program's run.

// AND THE ROOM, THE RAIL AND THE CARD READ ONE FIGURE. Opened while the run
// works, the room's facts row reads the rail's clock, anchored at the record's
// start, and not the store's stamps, which bracket other events (the store is
// seeded before the run's copy is made); once the run has landed the facts row,
// the stored page's pinned line and the landed card read one span.
func TestAProgramRunReadsOneFigureOnTheRoomTheRailAndTheCard(t *testing.T) {
	a, agent := programRoomApp(t, 120, 28)
	// The store was seeded sixteen seconds before the run's hand-off.
	page := agent.planFake.pages["7"]
	page.Row.Started = programRunBegan.Add(-16 * time.Second)
	agent.planFake.pages["7"] = page
	rail := strings.Split(plain(a.railTelemetry(a.tasks[7], 40)), railSep)[0]
	openProgramRoomNow(t, a)
	facts, _ := a.programFactsWord(120)
	if !strings.HasSuffix(facts, rowSep+rail) || rail != "14m 3s" {
		t.Fatalf("the room's facts read %q and the rail %q, want both 14m 3s", facts, rail)
	}
	// THE RUN LANDS, its process gone twenty-nine minutes and eight seconds after
	// the hand-off.
	ended := programRunBegan.Add(29*time.Minute + 8*time.Second)
	now := ended.Add(3 * time.Second)
	a.clock = func() time.Time { return now }
	page.Row.Status, page.Row.Ended = "done", ended.Add(2*time.Second)
	agent.planFake.pages["7"] = page
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, page.Row.Title, session.TaskDone,
		session.TaskNotice{StartedAt: programRunBegan, EndedAt: ended, Elapsed: ended.Sub(programRunBegan)})})
	drive(t, a, frameMsg{})
	facts, _ = a.programFactsWord(120)
	if !strings.HasSuffix(facts, rowSep+"29m 8s") {
		t.Fatalf("the landed room's facts read %q, want the span 29m 8s", facts)
	}
	if pinned := a.taskPlanPinned(agent.planFake.pages["7"], 120); !strings.HasSuffix(pinned, rowSep+"29m 8s") {
		t.Fatalf("the stored page pins %q, want the span 29m 8s", pinned)
	}
	var card *taskDone
	for i := len(a.entries) - 1; i >= 0 && card == nil; i-- {
		card = a.doneCardAt(i)
	}
	if card == nil || !strings.Contains(plain(a.doneTail(card)), "29m08s") {
		t.Fatalf("the landed card does not read the span 29m08s: %+v", card)
	}
}

// THE CLOCK STOPS AT THE PROGRAM'S EXIT, NOT AT THE LANDING. After senior-dev's
// process ends the engine waits for the receipts of calls it still owes (up to
// seventy seconds on a cut call) and lands the work, and only then settles the
// conversation's row; the stored page's row already carries the exit. The room
// and the stored page counted on through that wait — `21m 5s` over a run of
// twenty minutes — and jumped back when the row settled.
func TestAProgramRoomsClockStopsAtTheProgramsExit(t *testing.T) {
	a, agent := programRoomApp(t, 120, 28)
	openProgramRoomNow(t, a)
	exit := programRunBegan.Add(20 * time.Minute)
	page := agent.planFake.pages["7"]
	page.Row.Ended = exit
	agent.planFake.pages["7"] = page
	now := exit.Add(65 * time.Second)
	a.clock = func() time.Time { return now }
	drain(t, a, a.programRoomRead())
	if a.tasks[7].state != session.TaskRunning {
		t.Fatalf("the fixture's row has settled: %s", a.tasks[7].state)
	}
	facts, _ := a.programFactsWord(120)
	if !strings.HasSuffix(facts, rowSep+"20m") {
		t.Fatalf("the room's facts read %q sixty-five seconds after a twenty-minute run's exit, want 20m", facts)
	}
	if pinned := a.taskPlanPinned(page, 120); !strings.HasSuffix(pinned, rowSep+"20m") {
		t.Fatalf("the stored page pins %q after the program exited, want 20m", pinned)
	}
	// A RUN STILL WORKING COUNTS ON, whatever the store's row says about the end
	// of a run it has not been told of.
	page.Row.Ended = time.Time{}
	if got := a.taskPlanAge(page.Row); got != "21m 5s" {
		t.Fatalf("a running program's page reads %q, want 21m 5s", got)
	}
}

// THE ROOM TURNS TO THE RAW CALLS AND BACK ON ONE KEY, and its key row says
// which: the calls while the room shows the actions, and the actions while it
// shows the calls — beside the stop while there is work to stop.
func TestAProgramRoomTurnsToItsRawCallsAndBack(t *testing.T) {
	a, _ := programRoomApp(t, 120, 30)
	openProgramRoomNow(t, a)
	if hint := a.roomHint(); hint != roomStopHint+railSep+programCallsWord {
		t.Fatalf("the room's key row reads %q, want the stop and the calls", hint)
	}
	if text := roomText(a); strings.Contains(text, "I'll read the middleware") || !strings.Contains(text, programTabSaid) {
		t.Fatalf("the room does not open on the actions:\n%s", text)
	}
	drive(t, a, key(programCallsKey))
	if text := roomText(a); !strings.Contains(text, "I'll read the middleware and the store first.") || !strings.Contains(text, "deepseek-v4-flash") {
		t.Fatalf("the key did not turn the room to its calls:\n%s", text)
	}
	if hint := a.roomHint(); !strings.HasSuffix(hint, programActionsWord) {
		t.Fatalf("the room's key row reads %q, want the way back to the actions", hint)
	}
	drive(t, a, key(programCallsKey))
	if text := roomText(a); strings.Contains(text, "I'll read the middleware") || !strings.Contains(text, programTabSaid) {
		t.Fatalf("the key did not turn the room back to its actions:\n%s", text)
	}
}
