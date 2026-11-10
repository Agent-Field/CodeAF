package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

const currentLandingTitle = "Create R1b"

func currentLandingApp(t *testing.T, shape string, replay bool, old session.TaskState) (*app, session.TaskNotice) {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.homeRoot = t.TempDir()
	a.width, a.height, a.workMode = 200, 80, config.WorkFold
	history := []session.DisplayEntry{
		{Role: "user", Text: "Create R1b"},
		{Role: "assistant", Text: "The task is running.", Answer: true},
	}
	if replay {
		// The journal retains the first receipt; the checkpoint replays the current node below.
		history = append(history, session.DisplayEntry{Role: "aside", Text: "First landing report", RoutineCompletion: true,
			CompletionTasks: []session.TaskReplyTag{{ID: 7, Title: currentLandingTitle, State: old}}})
	}
	a.entries, a.turn = a.replayBlocks(history, chatReplay(0))
	notice := session.TaskNotice{Merge: "inplace"}
	if shape == "run" {
		notice.PlanTask = "t-7"
	}
	if !replay {
		a.taskUpdate(update(7, currentLandingTitle, session.TaskRunning, notice))
		if old == session.TaskFailed {
			notice.Merge, notice.Branch = "kept", "task/r1b"
		}
		a.taskUpdate(update(7, currentLandingTitle, old, notice))
		// A resumed conversation can separate landings into different batches.
		a.turn++
		a.entries = append(a.entries, entry{kind: entryAssistant, turn: a.turn, text: "I will keep the work.", settled: true})
	}
	return a, notice
}

func checkCurrentLanding(t *testing.T, a *app, state string, wantRow bool) {
	t.Helper()
	for {
		opened := false
		d := a.conversation()
		for _, r := range a.visible(a.bodyWidth()) {
			if r.hit == hitWorkFold && !a.workFoldOpen(d, r.turn) {
				a.openWorkfold(r.turn)
				opened = true
			}
		}
		if !opened {
			break
		}
		a.touch()
	}
	rows := 0
	cards := map[int]bool{}
	for _, r := range a.visible(a.bodyWidth()) {
		if r.hit == hitWorkCompletion && r.completion.task == 7 {
			rows++
			if text := plain(r.text); !strings.Contains(text, currentLandingTitle+" · "+state+" · open result") {
				t.Errorf("current row = %q", text)
			}
		}
		if r.hit == hitDone && a.entries[r.entry].done.id == 7 {
			cards[r.entry] = true
			if got := a.entries[r.entry].done.status.Word; got != state {
				t.Errorf("current card = %q, want %q", got, state)
			}
		}
	}
	wantRows, wantCards := 0, 1
	if wantRow {
		wantRows, wantCards = 1, 0
	}
	if rows != wantRows || len(cards) != wantCards {
		t.Errorf("task has %d rows and %d cards, want %d and %d\n%s", rows, len(cards), wantRows, wantCards, taskText(a))
	}
}

func TestTaskCurrentLandingSpeaksOnce(t *testing.T) {
	for _, shape := range []string{"run", "node"} {
		for _, replay := range []bool{false, true} {
			name := shape + "/live"
			if replay {
				name = shape + "/replay"
			}
			t.Run(name, func(t *testing.T) {
				a, notice := currentLandingApp(t, shape, replay, session.TaskDone)
				notice.Merge, notice.Branch = "conflict", "task/r1b"
				a.taskUpdate(update(7, currentLandingTitle, session.TaskUnverified, notice))
				checkCurrentLanding(t, a, "your call", false)
				notice.Merge, notice.Ending = "kept", session.TaskEndingRefused
				a.taskUpdate(update(7, currentLandingTitle, session.TaskFailed, notice))
				checkCurrentLanding(t, a, "incomplete", false)
			})
		}
	}
}

func TestTaskNewestQuietLandingOwnsRow(t *testing.T) {
	for _, replay := range []bool{false, true} {
		name := "live"
		if replay {
			name = "replay"
		}
		t.Run(name, func(t *testing.T) {
			a, notice := currentLandingApp(t, "node", replay, session.TaskFailed)
			notice.Merge, notice.Branch = "merged", ""
			a.taskUpdate(update(7, currentLandingTitle, session.TaskDone, notice))
			checkCurrentLanding(t, a, "done", true)
		})
	}
}

func TestDismissedTaskLandingKeepsCurrentRow(t *testing.T) {
	for _, replay := range []bool{false, true} {
		name := "live"
		if replay {
			name = "replay"
		}
		t.Run(name, func(t *testing.T) {
			a, notice := currentLandingApp(t, "node", replay, session.TaskDone)
			notice.Merge, notice.Branch = "kept", "task/r1b"
			notice.Ending = session.TaskEndingRefused
			a.taskUpdate(update(7, currentLandingTitle, session.TaskFailed, notice))
			if !a.dismissDone(a.doneEntryFor(7)) {
				t.Fatal("settled card could not be dismissed")
			}
			checkCurrentLanding(t, a, "incomplete", true)
		})
	}
}

func TestTaskCurrentLandingCombinesHistoricalReceipts(t *testing.T) {
	a, notice := currentLandingApp(t, "node", true, session.TaskFailed)
	// Journal revisions retain separate receipts, but still describe one task's current state.
	blocks, _ := a.replayBlocks([]session.DisplayEntry{{Role: "aside", Text: "Latest landing report", RoutineCompletion: true,
		CompletionTasks: []session.TaskReplyTag{{ID: 7, Title: currentLandingTitle, State: session.TaskDone, Revision: 2}}}}, chatReplay(a.turn))
	a.entries = append(a.entries, blocks...)
	a.taskUpdate(update(7, currentLandingTitle, session.TaskDone, notice))
	checkCurrentLanding(t, a, "done", true)
}
