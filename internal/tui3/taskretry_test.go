package tui3

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

type retryFake struct {
	*fakeAgent
	ids []uint64
	err error
}

func (f *retryFake) RetryTask(id uint64) error { f.ids = append(f.ids, id); return f.err }

func retryFixture(t *testing.T) (*app, *retryFake, session.TaskIndexEntry) {
	t.Helper()
	f := &retryFake{fakeAgent: &fakeAgent{}}
	a := newTestApp(f)
	a.file = "/tmp/retry-session/session.jsonl"
	a.width = 120
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Repair export", State: session.TaskFailed, Report: "old failure", Ending: session.TaskEndingStopped, Stopped: true, EndedAt: time.Now()}})
	entry := session.TaskIndexEntry{ID: "7", Title: "Repair export", Label: "Repair export", SessionID: "retry-session", Status: string(session.TaskFailed), Outcome: "old failure"}
	a.comp.tasks = []session.TaskIndexEntry{entry}
	a.taskSheet.detail, a.taskSheet.detailOn = entry, true
	return a, f, entry
}

func TestTaskRetryEnterUpdatesEveryViewWithoutAnotherTask(t *testing.T) {
	a, f, entry := retryFixture(t)
	rows, _, _, _ := a.taskCardFrame(120, 40)
	foot := ansi.Strip(rows[len(rows)-1])
	if !strings.HasPrefix(strings.TrimSpace(foot), "enter retry · m puts it in your message") {
		t.Fatal(foot)
	}
	count, at := len(a.entries), a.doneEntryFor(7)
	a.doneRows(a.entries[at].done, 120, false)
	cmd := a.taskCardKey("enter")
	if cmd == nil {
		t.Fatal("enter did not retry")
	}
	if a.taskCardKey("enter") != nil {
		t.Fatal("second enter duplicated pending request")
	}
	a.Update(cmd())
	if len(f.ids) != 1 || f.ids[0] != 7 {
		t.Fatal(f.ids)
	}
	for _, state := range []session.TaskState{session.TaskQueued, session.TaskRunning, session.TaskDone} {
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Repair export", State: state}})
		node := a.tasks[7]
		if len(a.taskOrder) != 1 || len(a.tasks) != 1 || len(a.entries) != count || a.doneEntryFor(7) != at {
			t.Fatal("retry duplicated or moved the task")
		}
		if node.stopped || node.ending != "" || node.report != "" {
			t.Fatal("retry kept old failure")
		}
		if a.currentTaskEntry(entry).Status != string(state) || a.taskSheetOwnRows()[0].Status != string(state) {
			t.Fatal("card or task list retained old state")
		}
		if a.taskCanRetry(entry) {
			t.Fatal("retry still offered on active or completed work")
		}
		rows, _, _, _ = a.taskCardFrame(120, 40)
		text := ansi.Strip(strings.Join(rows, "\n"))
		if strings.Contains(text, "old failure") || strings.Contains(text, "enter retry") {
			t.Fatal(text)
		}
		painted := ansi.Strip(strings.Join(a.doneRows(a.entries[at].done, 120, false), "\n"))
		if strings.Contains(painted, "old failure") || !strings.Contains(painted, a.taskStatus(node).Word) {
			t.Fatal(painted)
		}
		if a.entries[at].done.status.Word != a.taskStatus(node).Word {
			t.Fatal("conversation receipt disagrees with sidebar")
		}
	}
}

func TestTaskRetryFailureIsVisibleAndForeignTasksStayUntouched(t *testing.T) {
	a, f, entry := retryFixture(t)
	f.err = errors.New("engine unavailable")
	cmd := a.taskCardKey("enter")
	a.Update(cmd())
	rows, _, _, _ := a.taskCardFrame(120, 40)
	if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "could not retry · engine unavailable") {
		t.Fatal("retry failure hidden")
	}
	if a.tasks[7].state != session.TaskFailed {
		t.Fatal("failed retry changed state")
	}
	entry.SessionID = "other-conversation"
	a.taskSheet.detail = entry
	if a.taskCardKey("enter") != nil || a.taskCanRetry(entry) {
		t.Fatal("retry targeted a foreign task")
	}
}

func TestTaskRetryDoesNotOfferUnsupportedKinds(t *testing.T) {
	a, _, entry := retryFixture(t)
	for _, kind := range []session.TaskKind{session.TaskKindJob, session.TaskKindAdaptive} {
		a.tasks[7].kind = kind
		if a.taskCanRetry(entry) {
			t.Fatalf("unsupported kind %s offers retry", kind)
		}
	}
	a.tasks[7].kind = ""
	a.tasks[7].run = "separate-run"
	if a.taskCanRetry(entry) {
		t.Fatal("a run's child was offered the task worker's retry door")
	}

}

func TestTaskRetryRestartsAnOpenHostedPageWithoutLosingItsReading(t *testing.T) {
	a, _, _ := retryFixture(t)
	a.room = a.newRoom(7, "Repair export")
	a.room.done = true
	a.room.entries = []entry{{kind: entryAssistant, text: "first attempt"}}
	a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) { return session.TaskRecord{}, nil }
	gen := a.room.gen
	a.input.setText("")
	if a.roomHint() != taskRetryWord {
		t.Fatal("task page did not advertise enter retry")
	}
	cmd, took := a.roomKey(key("enter"))
	if !took || cmd == nil {
		t.Fatal("empty enter did not retry from task page")
	}
	a.Update(cmd())
	resumed := a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Repair export", State: session.TaskRunning}})
	if resumed == nil || a.room.done || a.room.gen == gen {
		t.Fatal("task page did not resume its reading")
	}
	if len(a.room.entries) != 1 || a.room.entries[0].text != "first attempt" {
		t.Fatal("retry replaced the earlier reading")
	}
	a.Update(roomClosedMsg{gen: gen})
	if a.room.done {
		t.Fatal("old stream closure stopped the new page")
	}
}

func TestTaskRetryOffersEveryStoredRunnerAndUnsuccessfulEnding(t *testing.T) {
	for _, kind := range []session.TaskKind{"", session.TaskKindQuick, session.TaskKindHarness, session.TaskKindSubharness} {
		for _, ending := range []session.TaskEnding{session.TaskEndingStopped, session.TaskEndingInterrupted, session.TaskEndingWire, session.TaskEndingRefused} {
			a, _, entry := retryFixture(t)
			a.tasks[7].kind = kind
			a.tasks[7].ending = ending
			if !a.taskCanRetry(entry) {
				t.Fatalf("no retry for %s / %s", kind, ending)
			}
		}
	}
}

// A PROGRAM'S FAILED TASK OFFERS NO RETRY, because the engine's retry reopens
// one of its own nodes and a program's run is not one.
func TestAProgramsFailedTaskOffersNoRetry(t *testing.T) {
	a, _, entry := retryFixture(t)
	if !a.taskCanRetry(entry) {
		t.Fatal("the fixture's ordinary failed task is not offered a retry")
	}
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Repair export", State: session.TaskFailed, Report: "old failure", Program: "senior-dev", EndedAt: time.Now()}})
	entry.Program = "senior-dev"
	a.taskSheet.detail = entry
	if a.taskCanRetry(entry) {
		t.Fatal("a program's failed task is offered a retry the engine refuses")
	}
	rows, _, _, _ := a.taskCardFrame(120, 40)
	if text := ansi.Strip(strings.Join(rows, "\n")); strings.Contains(text, taskRetryWord) {
		t.Fatalf("the card offers %q on a program's task:\n%s", taskRetryWord, text)
	}
}
