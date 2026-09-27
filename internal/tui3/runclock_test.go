package tui3

// ONE CLOCK FOR ONE RUN. A run's rows publish the record's start and, when it
// lands, its end, and every surface that draws how long the run has taken reads
// those two instants: the side list's clock counts from the start whenever this
// window met the run, and the landed card's span is the end less the start.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A RUN'S ROW COUNTS FROM THE RECORD'S START, NOT FROM WHEN THIS WINDOW MET IT.
// A run's rows carry no age (session's task_run_belt.go), and a window that
// attached twenty minutes into senior-dev's run started the rail's clock at
// that moment: at twenty-eight and a half minutes the rail read `8m 30s`.
func TestARunsRailClockCountsFromTheRecordsStart(t *testing.T) {
	a, _ := planAppWith(t, nil, nil)
	started := taskFixtureNow
	now := started.Add(20 * time.Minute)
	a.clock = func() time.Time { return now }
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "rewrite the auth middleware", session.TaskRunning,
		session.TaskNotice{StartedAt: started})})
	now = started.Add(28*time.Minute + 30*time.Second)
	node := a.tasks[7]
	if got := plain(a.railTelemetry(node, 40)); !strings.HasPrefix(got, "28m 30s") {
		t.Fatalf("the rail reads %q twenty-eight and a half minutes into the run, want 28m 30s", got)
	}
	// A START STAMPED BY A CLOCK AHEAD OF THIS ONE IS NOT TRUSTED over the age the
	// update reported, which needs no agreement between two clocks.
	b, _ := planAppWith(t, nil, nil)
	at := taskFixtureNow
	b.clock = func() time.Time { return at }
	drive(t, b, streamEventMsg{gen: b.gen, ev: update(8, "ahead", session.TaskRunning,
		session.TaskNotice{StartedAt: at.Add(time.Minute), Elapsed: 5 * time.Second})})
	if got := b.tasks[8].began; !got.Equal(at.Add(-5 * time.Second)) {
		t.Fatalf("a start from a clock ahead anchored the row at %s, want the reported age", got)
	}
}

// THE LANDED CARD MEASURES THE RECORD'S OWN SPAN. The same window, met twenty
// minutes late, landed a twenty-nine-minute run as `9m08s` under a card whose
// own stamps said 29 minutes; and with the age the session now reports, the
// card and the stamps agree.
func TestALandedCardMeasuresTheRecordsOwnSpan(t *testing.T) {
	started := taskFixtureNow
	ended := started.Add(29*time.Minute + 8*time.Second + 400*time.Millisecond)
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
	}{
		{"stamps alone", 0},
		{"stamps and the reported age", ended.Sub(started)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := planAppWith(t, nil, nil)
			now := started.Add(20 * time.Minute)
			a.clock = func() time.Time { return now }
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "rewrite the auth middleware", session.TaskRunning,
				session.TaskNotice{StartedAt: started})})
			now = ended.Add(2 * time.Second)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "rewrite the auth middleware", session.TaskDone,
				session.TaskNotice{StartedAt: started, EndedAt: ended, Elapsed: tc.elapsed})})
			card := a.doneCardAt(len(a.entries) - 1)
			if card == nil {
				t.Fatal("the run's landing drew no card")
			}
			if tail := plain(a.doneTail(card)); !strings.Contains(tail, "29m08s") {
				t.Fatalf("the landed card reads %q, want the record's span 29m08s", tail)
			}
			if got := a.roomClock(a.tasks[7]); got != "29m 8s" {
				t.Fatalf("the landed run's clock reads %q, want 29m 8s", got)
			}
		})
	}
}

// A FINISHED SPAN READ OFF THE STORE IS ROUNDED LIKE EVERY OTHER. A page whose
// run this window holds no node for reads the row's own pair, and it cut a
// 61.5-second run to `1m 1s` while the card and the note the chat was handed
// said `1m 2s`.
func TestAStoredRowsFinishedSpanIsRoundedLikeTheCards(t *testing.T) {
	a, _ := planAppWith(t, nil, nil)
	started := taskFixtureNow
	row := session.PlanTaskRow{ID: "t-9", Program: "senior-dev", Status: "done",
		Started: started, Ended: started.Add(61*time.Second + 500*time.Millisecond)}
	if got := a.taskPlanAge(row); got != "1m 2s" {
		t.Fatalf("a finished 61.5-second run reads %q, want 1m 2s", got)
	}
}

// AN ORDINARY TASK SETTLED AGAIN KEEPS THE AGE ITS WORK TOOK. The engine moves
// a node's end to the moment a person accepts it (and to every later round that
// settles it again) and keeps the age it reported at the landing, so a task that
// worked five minutes and was accepted an hour later read `1h 5m` on its room
// and `1h05m` on its card when the two stamps outranked the age. The stamps are
// what is left for a row that reports no age — a run's row — and there they
// still measure the run.
func TestASettledTasksClockIsTheAgeItReported(t *testing.T) {
	a, _ := planAppWith(t, nil, nil)
	started := taskFixtureNow
	now := started.Add(time.Minute)
	a.clock = func() time.Time { return now }
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "tidy the parser", session.TaskRunning,
		session.TaskNotice{StartedAt: started, Elapsed: time.Minute})})
	now = started.Add(5 * time.Minute)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "tidy the parser", session.TaskUnverified,
		session.TaskNotice{StartedAt: started, EndedAt: now, Elapsed: 5*time.Minute + 300*time.Millisecond})})
	if got := a.roomClock(a.tasks[4]); got != "5m" {
		t.Fatalf("the landed task's clock reads %q, want 5m", got)
	}
	now = started.Add(time.Hour + 5*time.Minute)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "tidy the parser", session.TaskDone,
		session.TaskNotice{StartedAt: started, EndedAt: now, Elapsed: 5*time.Minute + 300*time.Millisecond})})
	if got := a.roomClock(a.tasks[4]); got != "5m" {
		t.Fatalf("a task that worked 5m and was accepted an hour later reads %q, want 5m", got)
	}
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the accepted task drew no card")
	}
	if tail := plain(a.doneTail(card)); !strings.Contains(tail, "5m00s") {
		t.Fatalf("the accepted task's card reads %q, want 5m00s", tail)
	}
}

// A REPORTED AGE OUTRANKS ANOTHER MACHINE'S START. An engine on another
// machine stamps a node's start on its own clock, and one running ninety
// seconds behind this window's made a node ten seconds into its work read
// `1m 40s` on the rail — and jump back when it landed. The age an update
// reports needs no agreement between two clocks; the start is the anchor only
// for a row that reports no age, which is a run's.
func TestARunningClockTrustsTheReportedAgeOverAnotherMachinesStart(t *testing.T) {
	a, _ := planAppWith(t, nil, nil)
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(5, "behind", session.TaskRunning,
		session.TaskNotice{StartedAt: now.Add(-10*time.Second - 90*time.Second), Elapsed: 10 * time.Second})})
	if got := a.tasks[5].began; !got.Equal(now.Add(-10 * time.Second)) {
		t.Fatalf("a start from a clock behind anchored the row at %s, want the reported age", got)
	}
	if got := plain(a.railTelemetry(a.tasks[5], 40)); !strings.HasPrefix(got, "10s") {
		t.Fatalf("the rail reads %q ten seconds into the work, want 10s", got)
	}
}

// A ROW WHOSE ROOM IS OPEN DRAWS NO CLOCK, RATHER THAN ONE STOPPED AT THE CLICK.
// Standing in senior-dev's room froze its row's age at the second the room
// opened: the side list read `2s` for a minute and more beside a page whose
// header read `1m 21s`. The row now drops its clock while the room is open and
// reads the whole true age again the moment the person leaves.
func TestARowWhoseRoomIsOpenDrawsNoStoppedClock(t *testing.T) {
	a, _ := planAppWith(t, nil, nil)
	started := taskFixtureNow
	now := started.Add(2 * time.Second)
	a.clock = func() time.Time { return now }
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "rewrite the auth middleware", session.TaskRunning,
		session.TaskNotice{StartedAt: started, CostUSD: 0.04})})
	a.freezeNode(7)
	now = started.Add(81 * time.Second)
	got := plain(a.railTelemetry(a.tasks[7], 40))
	if strings.Contains(got, "2s") || strings.Contains(got, "1m") || !strings.Contains(got, "$0.04") {
		t.Fatalf("a row whose room is open reads %q, want its spend and no clock", got)
	}
	a.thawNode(7)
	if got := plain(a.railTelemetry(a.tasks[7], 40)); !strings.HasPrefix(got, "1m 21s") {
		t.Fatalf("the row read %q once its room closed, want the whole age 1m 21s", got)
	}
}

// THE SIDE ROW HIDES A RUN'S CLOCK WHILE ITS ROOM IS OPEN. The room keeps the
// node's clock frozen for its own detail rows, so the compact side row must not
// print that frozen age beside the room's live header.
func TestAProgramSideRowHidesItsClockWhileTheRoomIsOpen(t *testing.T) {
	a, _ := programRoomApp(t, 180, 36)
	started := programRunBegan
	now := started.Add(20 * time.Minute)
	a.clock = func() time.Time { return now }
	row := func() string {
		return plain(a.railEntryRow(railEntry{node: a.tasks[7], group: railRunning}, 70))
	}
	if got := row(); !strings.Contains(got, "20m") {
		t.Fatalf("the running program's side row reads %q, want its live age", got)
	}
	openProgramRoomNow(t, a)
	now = started.Add(21 * time.Minute)
	if got := row(); strings.Contains(got, "20m") || strings.Contains(got, "21m") {
		t.Fatalf("the open program's side row reads %q, want no frozen clock", got)
	}
	a.closeRoom()
	if got := row(); !strings.Contains(got, "21m") {
		t.Fatalf("the program's side row reads %q after leaving, want its live age", got)
	}
}
