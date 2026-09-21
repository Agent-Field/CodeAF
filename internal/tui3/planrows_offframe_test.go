package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// frameSideOfTheTasksReading is every road a FRAME or the place's own beat takes
// onto the reading, walked once: the freshness check itself, the one door every
// drawing path comes through, the place's tick, the rail, the tab strip and the
// whole frame.
func frameSideOfTheTasksReading(a *app) {
	a.taskSheet.regroup(a)
	a.tasksFiltered()
	placeTasks{}.tick(a, a.now())
	a.railDrawnView(a.viewHeight())
	a.tabList()
	a.frame()
}

// NO FRAME READS THE RUN'S ROWS OVER THE WIRE, WHATEVER MADE THE READ DUE.
//
// The side list's rows were read inside the freshness check every drawing path
// comes through, so each thing that made a read due made it from a frame: the
// first reading of a conversation, a row of this window's own graph moving, the
// main chat's own state changing, and the run's beat. Over a connection that
// read is a call to another process with a ten second deadline, and a frame that
// waits on it holds every key a person presses. An earlier change moved one of
// the four off the frame and left three.
//
// This is the real remote client on the real wire with its reads counted. Each
// trigger is raised, the frame side is walked, and the count must not move;
// then the one message a window always gets next is delivered, and the count
// moves by exactly one.
func TestNoFrameReadsTheRunsRowsWhateverMadeTheReadDue(t *testing.T) {
	a, counted, _ := hostedPlanApp(t, false)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }

	triggers := []struct {
		name  string
		raise func()
		// owed says whether the trigger makes a read of the rows due at all: the
		// main chat's own state re-files the reading and asks the engine nothing.
		owed bool
	}{
		{name: "the first reading of the conversation", raise: func() {}, owed: true},
		{name: "a row of this window's own graph moved", raise: func() { a.railStamp++ }, owed: true},
		{name: "the run's beat came due", raise: func() { now = now.Add(elsewhereEvery) }, owed: true},
		{name: "the main chat's own title changed", raise: func() { a.title = "a new name" }, owed: false},
	}
	for _, trigger := range triggers {
		trigger.raise()
		before := counted.rows.Load()
		frameSideOfTheTasksReading(a)
		frameSideOfTheTasksReading(a)
		if got := counted.rows.Load() - before; got != 0 {
			t.Fatalf("%s: the frame side read the run's rows over the wire %d times, want none", trigger.name, got)
		}
		readPlanRows(t, a)
		want := int64(0)
		if trigger.owed {
			want = 1
		}
		if got := counted.rows.Load() - before; got != want {
			t.Fatalf("%s: the message after it cost %d reads over the wire, want %d", trigger.name, got, want)
		}
		if len(a.taskSheet.mine.plan) == 0 {
			t.Fatalf("%s: the reading holds no row of the live run", trigger.name)
		}
	}
}

// slowPlanRows is a link that takes its time: the rows are answered only when
// the test lets them go, the way a read over a bad connection is.
type slowPlanRows struct {
	*planFake
	asked   chan struct{}
	release chan struct{}
}

func (s *slowPlanRows) PlanTasks() []session.PlanTaskRow {
	s.asked <- struct{}{}
	<-s.release
	return s.planFake.PlanTasks()
}

// A READ THAT IS OUT HOLDS NOTHING. While the rows are on their way the frame
// draws what it held, a second read is not asked for, and a verb that landed in
// the meantime is answered by exactly one more read once the first is home.
func TestWhileTheRowsAreOnTheirWayTheFrameDrawsWhatItHeld(t *testing.T) {
	a, fake := planAppWith(t, []session.PlanTaskRow{{ID: "1", Title: "the run", Status: "running"}}, nil)
	slow := &slowPlanRows{planFake: fake, asked: make(chan struct{}, 4), release: make(chan struct{})}
	a.agent = slow
	a.taskSheet.regroup(a)

	fake.plan = append(fake.plan, session.PlanTaskRow{ID: "p1", Parent: "1", Title: "a part the run added", Status: "running"})
	a.railStamp++
	cmd := a.refreshPlanRows()
	if cmd == nil {
		t.Fatal("a moved stamp asked for no read")
	}
	answered := make(chan func(), 1)
	go func() {
		msg := cmd()
		answered <- func() { drive(t, a, msg) }
	}()
	<-slow.asked

	// The read is out. The frame side is walked and returns at once with the row
	// it held; nothing further is asked, and the stamp moves again under it.
	frameSideOfTheTasksReading(a)
	if got := len(a.taskSheet.mine.plan); got != 1 {
		t.Fatalf("with the read still out the reading holds %d rows, want the one it held", got)
	}
	if again := a.refreshPlanRows(); again != nil {
		t.Fatal("a second read was asked for while the first was still out")
	}
	a.railStamp++

	close(slow.release)
	(<-answered)()
	a.taskSheet.regroup(a)
	if got := len(a.taskSheet.mine.plan); got != 2 {
		t.Fatalf("the answered read was not folded into the reading: %d rows, want 2", got)
	}
	// THE STAMP MOVED WHILE THE FIRST READ WAS OUT, so the message that folded it
	// in asked once more, and that answer settled the matter: one further read,
	// and nothing owed after it.
	if got := len(slow.asked); got != 1 {
		t.Fatalf("a stamp that moved while the read was out cost %d further reads, want exactly one", got)
	}
	if again := a.refreshPlanRows(); again != nil {
		t.Fatal("a read is still owed after the stamp's own read came home")
	}
}
