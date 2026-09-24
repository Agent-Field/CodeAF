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
