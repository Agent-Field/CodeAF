package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

func assertDrawnPlanWord(t *testing.T, text, title, want string) {
	t.Helper()
	line, ok := planLine(text, title)
	if !ok {
		t.Fatalf("the row %q was not drawn:\n%s", title, text)
	}
	if !strings.Contains(line, want) {
		t.Fatalf("the row %q reads %q, want drawn word %q", title, line, want)
	}
}

func TestAStoppedRootPageDrawsStopped(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-root", Title: "the run", Status: "cancelled", Stopped: true}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{"t-root": {Row: row}})
	openPlanRoomNow(t, a, "t-root")
	text := planRoomText(t, a)
	if !strings.Contains(text, "stopped") || strings.Contains(text, "incomplete") {
		t.Fatalf("the stopped root's room draws the wrong state word:\n%s", text)
	}
}

func TestAStoppedPartRowDrawsStopped(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "t-root", Title: "the run", Status: "running"},
		{ID: "t-part", Parent: "t-root", Title: "the stopped part", Status: "cancelled", Stopped: true},
	}
	assertDrawnPlanWord(t, planTextFor(t, rows), "the stopped part", "stopped")
}

func TestAPartThatFailedByItselfDrawsIncomplete(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "t-root", Title: "the run", Status: "running"},
		{ID: "t-part", Parent: "t-root", Title: "the failed part", Status: "failed"},
	}
	text := planTextFor(t, rows)
	assertDrawnPlanWord(t, text, "the failed part", "incomplete")
	if line, _ := planLine(text, "the failed part"); strings.Contains(line, "stopped") {
		t.Fatalf("a self-failed part reads as stopped: %q", line)
	}
}

func TestAStoppedRootCrossesTheHostedWireAndDrawsStopped(t *testing.T) {
	a, _, path := hostedPlanApp(t, false)
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StopRoot("stopped"); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	openHostedPage(t, a)
	text := planRoomText(t, a)
	if !strings.Contains(text, "stopped") || strings.Contains(text, "incomplete") {
		t.Fatalf("the hosted stopped-root page draws the wrong state word:\n%s", text)
	}
	// The assertion is about the frame. Enter is included to ensure the opened
	// hosted page has passed through the same page gesture a person uses.
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestAStoppedPlanRowUsesTheOrdinaryStoppedGlyph(t *testing.T) {
	plan := planStatus(session.PlanTaskRow{Status: "cancelled", Stopped: true})
	ordinary := session.ProjectTask(session.TaskFacts{
		State:   session.TaskFailed,
		Stopped: true,
		Ending:  session.TaskEndingStopped,
	})
	if got, want := tierSlot(plan), tierSlot(ordinary); got != want {
		t.Fatalf("stopped plan glyph slot = %d, ordinary stopped glyph slot = %d", got, want)
	}
}

// A STOPPED TASK HAS ENDED, and everything that asks whether a task can still
// move has to hear that from the one word the row draws. The word became
// `stopped` and the question went on listing only `done` and `incomplete`, so a
// task a person had just stopped was offered its stop again, and the store's
// refusal was the answer.
func TestAStoppedTaskIsOfferedNoVerbAndNoNextStep(t *testing.T) {
	stopped := session.PlanTaskRow{ID: "t-part", Parent: "t-root", Title: "the stopped part", Status: "cancelled", Stopped: true}
	if !planEnded(stopped) {
		t.Fatal("a task a person stopped does not count as ended")
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{{ID: "t-root", Title: "the run", Status: "running"}, stopped}, nil)
	if words := a.tasksPlanKeyWords(stopped); len(words) != 0 {
		t.Fatalf("a stopped task is offered %v", words)
	}
	if planCanMove([]session.PlanTaskRow{stopped}) {
		t.Fatal("a stopped task is read as one that can still move by itself")
	}
}
