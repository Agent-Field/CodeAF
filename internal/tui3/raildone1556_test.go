package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// doneColumn1556 reads the side column after two attempts at one request have
// each written a run. Their titles match, but their store roots and node ids do
// not: the stopped attempt and the merged attempt both belong in Done.
func doneColumn1556(t *testing.T, width int) *app {
	t.Helper()
	const title = "rename LoadConfig to LoadSettings everywhere"
	rows := []session.PlanTaskRow{
		{ID: "t-1", Title: title, Status: "failed", Program: "senior-dev"},
		{ID: "t-2", Title: title, Status: "done", Program: "senior-dev"},
	}
	a, _ := planAppWith(t, rows, nil)
	a.width, a.height = width, 40
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, title, session.TaskRunning,
		session.TaskNotice{Program: "senior-dev", PlanTask: rows[0].ID})})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, title, session.TaskFailed,
		session.TaskNotice{Program: "senior-dev", PlanTask: rows[0].ID, Stopped: true, Ending: session.TaskEndingStopped})})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, title, session.TaskRunning,
		session.TaskNotice{Program: "senior-dev", PlanTask: rows[1].ID})})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, title, session.TaskDone,
		session.TaskNotice{Program: "senior-dev", PlanTask: rows[1].ID, Merge: mergeWordMerged})})
	readPlanRows(t, a)
	return a
}

// doneAttemptRows1556 finds the two visible attempts by the portion of their
// shared title that survives even the narrow column's fitting rule.
func doneAttemptRows1556(lines []string) []int {
	var out []int
	for i, line := range lines {
		if strings.Contains(line, "rename Load") {
			out = append(out, i)
		}
	}
	return out
}

func TestDoneOpenDrawsEveryRowItsHeadingCounts(t *testing.T) {
	for _, width := range []int{110, 140, 180} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := doneColumn1556(t, width)
			for _, open := range []bool{false, true} {
				a.side.open[railDone] = open
				view := strings.Join(railText(a, a.viewHeight()), "\n")
				if strings.Contains(view, "Done 2") || strings.Contains(view, "rename Load") {
					t.Fatalf("completed attempts occupy sidebar: %s", view)
				}
				for _, id := range []uint64{1, 2} {
					if a.doneCardAt(a.doneEntryFor(id)) == nil {
						t.Fatalf("result %d lost", id)
					}
				}
			}
		})
	}
}

func TestAFoldedGroupFoldsTheRunsItsRowsCarry(t *testing.T) {
	a := doneColumn1556(t, 180)
	a.side.open[railDone] = true
	if view := strings.Join(railText(a, a.viewHeight()), "\n"); strings.Contains(view, "rename Load") || strings.Contains(view, "Done 2") {
		t.Fatalf("opening obsolete fold raised completed work: %s", view)
	}
	if len(a.tasks) != 2 {
		t.Fatal("completed results lost")
	}
}

func TestAStoppedRowKeepsItsRoleBadge(t *testing.T) {
	a := doneColumn1556(t, 180)
	card := a.doneCardAt(a.doneEntryFor(1))
	if card == nil || card.program != "senior-dev" || card.status.State != session.TaskFailed {
		t.Fatalf("stopped result lost identity: %+v", card)
	}
	if _, ok := railRowFor(a, a.viewHeight(), "rename LoadConfig"); ok {
		t.Fatal("stopped result occupies sidebar")
	}
}

// The existing TestARunsPartsHangUnderTheNodeRowThatCarriesIt pins the running
// carrier, and TestRailPlanDrawsEveryPartAsATaskRow pins a run with no node.
// This guard pins the older node whose run identity was never recorded.
func TestAnOlderNodeCarriesOneRunByTitle(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-9", Title: "Review the loader", Status: "claimed"}
	kid := session.PlanTaskRow{ID: "t-10", Parent: root.ID, Title: "Inspect a call", Status: "claimed"}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, kid}, nil)
	a.width, a.height = 180, 40
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, root.Title, session.TaskRunning,
		session.TaskNotice{Program: "senior-dev"})})
	readPlanRows(t, a)
	lines := railText(a, a.viewHeight())
	view := strings.Join(lines, "\n")
	if got := strings.Count(view, root.Title); got != 1 {
		t.Fatalf("the older node and its run draw %d roots, want one:\n%s", got, view)
	}
	rootAt, kidAt := -1, -1
	for i, line := range lines {
		if strings.Contains(line, root.Title) {
			rootAt = i
		}
		if strings.Contains(line, kid.Title) {
			kidAt = i
		}
	}
	if rootAt < 0 || kidAt <= rootAt || !strings.Contains(lines[rootAt], "[senior-dev]") {
		t.Fatalf("the older node does not carry its part and badge:\n%s", view)
	}
}

// A node may name one part rather than its run's root. The store's parent
// links still make that node the carrier of the whole run.
func TestANodeNamingAPartCarriesItsRun(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-20", Title: "Map the packet", Status: "claimed"}
	part := session.PlanTaskRow{ID: "t-21", Parent: root.ID, Title: "Check the packet", Status: "claimed"}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, part}, nil)
	a.width, a.height = 180, 40
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(20, root.Title, session.TaskRunning,
		session.TaskNotice{Program: "senior-dev", PlanTask: part.ID})})
	readPlanRows(t, a)
	lines := railText(a, a.viewHeight())
	view := strings.Join(lines, "\n")
	if strings.Count(view, root.Title) != 1 {
		t.Fatalf("the part id did not join its node to the run:\n%s", view)
	}
	rootAt, partAt := -1, -1
	for i, line := range lines {
		if strings.Contains(line, root.Title) {
			rootAt = i
		}
		if strings.Contains(line, part.Title) {
			partAt = i
		}
	}
	if rootAt < 0 || partAt <= rootAt || !strings.Contains(lines[rootAt], "[senior-dev]") {
		t.Fatalf("the run's part is not under its node's badged row:\n%s", view)
	}
}

func TestDoneFoldKeepsItsCountAndOtherGroups(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "finished work", session.TaskDone, session.TaskNotice{Result: "saved answer"}))
	a.taskUpdate(update(2, "running work", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(3, "queued work", session.TaskQueued, session.TaskNotice{}))
	for _, open := range []bool{false, true} {
		a.side.open[railDone] = open
		view := strings.Join(railText(a, a.viewHeight()), "\n")
		if strings.Contains(view, "finished work") || strings.Contains(view, "Done 1") {
			t.Fatalf("finished history occupies sidebar: %s", view)
		}
		if !strings.Contains(view, "running work") || !strings.Contains(view, "Queued 1") {
			t.Fatalf("active work lost: %s", view)
		}
	}
	if card := a.doneCardAt(a.doneEntryFor(1)); card == nil || card.result != "saved answer" {
		t.Fatal("finished result lost")
	}
}
