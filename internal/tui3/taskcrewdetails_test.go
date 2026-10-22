package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

type selectedCrewAgent struct {
	*fakeAgent
	selected uint64
}

func (f *selectedCrewAgent) RedoStronger(_ context.Context, id uint64) (uint64, string, error) {
	f.selected = id
	return 9, "stronger", nil
}

func TestTaskResultKeepsCrewAndTargetsSelectedStrongerRun(t *testing.T) {
	a, _, entry := retryFixture(t)
	f := &selectedCrewAgent{fakeAgent: &fakeAgent{}}
	a.agent = f
	crew := &crewroute.Decision{EstUSD: .52, Crew: []crewroute.Pick{{Seat: crewroute.Worker, Model: "worker-model"}, {Seat: crewroute.Planner, Model: "planner-model"}, {Seat: crewroute.Checker, Model: "checker-model"}}}
	a.tasks[7].crew = crew
	entry.Crew = crew
	a.taskSheet.detail = entry
	lines, hits, _, _ := a.taskCardFrame(120, 40)
	text := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"worker-model", "planner-model", "checker-model", "0.520", taskStrongerWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	var click = -1
	for y, hit := range hits {
		if hit == taskCardHitStronger {
			click = y
			break
		}
	}
	if click < 0 {
		t.Fatal("stronger action has no pointer target")
	}
	runCmd(a.taskCardPress(4, click))
	if f.selected != 7 {
		t.Fatalf("stronger run targeted %d, want selected 7", f.selected)
	}
}

func TestStrongerTaskActionNeverTargetsAnotherConversationOrBlockedCrew(t *testing.T) {
	a, _, entry := retryFixture(t)
	a.agent = &selectedCrewAgent{fakeAgent: &fakeAgent{}}
	entry.Crew = &crewroute.Decision{}
	if !a.taskCanRedoStronger(entry) {
		t.Fatal("own ended task unavailable")
	}
	entry.SessionID = "another-conversation"
	if a.taskCanRedoStronger(entry) || a.redoTaskStronger(entry) != nil {
		t.Fatal("foreign task could rerun locally")
	}
	entry.SessionID = "retry-session"
	entry.Crew.Stopped = "add credits"
	if a.taskCanRedoStronger(entry) {
		t.Fatal("stronger action offered despite credit stop")
	}
	entry.Crew.Stopped = ""
	entry.Status = string(session.TaskRunning)
	if a.taskCanRedoStronger(entry) {
		t.Fatal("running task offered stronger rerun")
	}
}

func TestActionableCrewStopStaysVisibleUntilResolved(t *testing.T) {
	a, _, entry := retryFixture(t)
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Repair export", State: session.TaskFailed, Crew: &crewroute.Decision{Stopped: "add credits to continue"}}})
	node := a.tasks[7]
	// This is a provider stop, not the person's intentional stop from the fixture.
	node.stopped = false
	node.ending = ""
	a.landedCard(node)
	card := a.doneCardAt(a.doneEntryFor(7))
	if card == nil || card.hidden() {
		t.Fatalf("actionable stop hidden: %+v", card)
	}
	if a.dismissDone(a.doneEntryFor(7)) {
		t.Fatal("actionable stop can be dismissed")
	}
	text := ansi.Strip(strings.Join(a.doneRows(card, 120, false), "\n"))
	if !strings.Contains(text, "add credits to continue") {
		t.Fatalf("remedy missing: %s", text)
	}
	if a.taskCanRedoStronger(entry) {
		t.Fatal("stronger retry offered for missing credit")
	}
	node.crew = nil
	a.landedCard(node)
	card = a.doneCardAt(a.doneEntryFor(7))
	if !card.hidden() {
		t.Fatal("ordinary failure raised attention")
	}
}

func TestCompletedCrewTaskOpensResultThroughTasksAndKeepsFullOutputDoor(t *testing.T) {
	a, _, entry := retryFixture(t)
	a.height = 40
	_, rooms, _ := roomApp(t)
	a.agent = &selectedCrewRoomAgent{roomFake: rooms}
	a.tasks[7].crew = &crewroute.Decision{EstUSD: .52, Crew: []crewroute.Pick{{Seat: crewroute.Worker, Model: "worker-model"}}}
	a.tasks[7].produced = "Actual selected task deliverable"
	a.tasks[7].cost = .31
	a.tasks[7].changed = []string{"export.go"}
	a.closeTaskRecord()
	a.showPage(pageTasks)
	a.taskSheet.regroup(a)
	a.taskSheet.cursor = a.tasksSettle(0)
	found := false
	for i := 0; i < 20; i++ {
		if item, ok := a.taskSheetCurrent(); ok && item.entry.ID == entry.ID {
			found = true
			break
		}
		a.taskSheetMove(1)
	}
	if !found {
		t.Fatal("completed task unavailable in Tasks history")
	}
	a.taskSheet.enter(a)
	if !a.taskSheet.detailOn || a.roomOpen() {
		t.Fatal("completed routed task bypassed its result page")
	}
	rows, hits, _, _ := a.taskCardFrame(120, 40)
	text := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"Actual selected task deliverable", "worker-model", "$0.31", "1 file changed", taskStrongerWord, taskResultOutputWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("result lost %q: %s", want, text)
		}
	}
	outputRow := -1
	for y, hit := range hits {
		if hit == taskCardHitOutput {
			outputRow = y
			break
		}
	}
	if outputRow < 0 {
		t.Fatal("full output action has no mouse target")
	}
	a.taskCardPress(4, outputRow)
	if !a.roomOpen() || a.room.id != 7 {
		t.Fatal("full output action failed to open selected transcript")
	}
}

// The same agent exposes both result actions throughout layout and the click.
type selectedCrewRoomAgent struct{ *roomFake }

func (f *selectedCrewRoomAgent) RedoStronger(_ context.Context, _ uint64) (uint64, string, error) {
	return 9, "stronger", nil
}

func TestTaskResultOutputUsesTheRunsCanonicalPlanPage(t *testing.T) {
	a, fake := railTaskPageApp(t, true)
	a.width, a.height = 120, 40
	a.tasks[2].state = session.TaskDone
	a.tasks[2].planTask = "2"
	entry := session.TaskIndexEntry{ID: "2", Title: a.tasks[2].title, Status: string(session.TaskDone), SessionID: a.taskSheetSelfID()}
	a.taskSheet.detail, a.taskSheet.detailOn = entry, true
	rows, hits, _, _ := a.taskCardFrame(120, 40)
	at := -1
	for y, hit := range hits {
		if hit == taskCardHitOutput {
			at = y
			break
		}
	}
	if at < 0 {
		t.Fatalf("result lacks full output target: %v", rows)
	}
	cmd := a.taskCardPress(4, at)
	drive(t, a, runCmd(cmd)...)
	if fake.pages.Load() != 1 || a.roomPlan() == nil || a.roomPlan().id != "2" {
		t.Fatalf("full output bypassed canonical run page: reads=%d room=%+v", fake.pages.Load(), a.roomPlan())
	}
	if !strings.Contains(planRoomText(t, a), "replace the parser") {
		t.Fatal("selected run output disappeared")
	}
}

func TestSelectedTaskResultSaysIdenticalOutcomeOnceAndKeepsDifferentAccount(t *testing.T) {
	const result = "The parser now handles an empty map. The regression test passes."
	for _, outcome := range []string{result, "The changes remain on the task branch for your review."} {
		t.Run(outcome, func(t *testing.T) {
			a, _, entry := retryFixture(t)
			node := a.tasks[7]
			node.crew = &crewroute.Decision{}
			node.produced, node.report = result, outcome
			entry.Outcome = outcome
			a.taskSheet.detail = entry
			text := ansi.Strip(strings.Join(a.taskCardBody(a.currentTaskEntry(entry), 120), "\n"))
			if count := strings.Count(text, result); count != 1 {
				t.Fatalf("selected result repeated %d times: %s", count, text)
			}
			if !strings.Contains(text, outcome) {
				t.Fatalf("distinct landing account lost: %s", text)
			}
			if node.produced != result || node.report != outcome || entry.Outcome != outcome {
				t.Fatal("presentation changed original result fields")
			}
		})
	}
}
