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
		for _, wide := range []bool{false, true} {
			if width == 110 && wide {
				continue
			}
			t.Run(fmt.Sprintf("width=%d/wide=%t", width, wide), func(t *testing.T) {
				a := doneColumn1556(t, width)
				if wide {
					drive(t, a, altT())
					drive(t, a, key(railWidenChord))
				}
				railClick(t, a, a.bodyWidth()+3, railHeadY(t, a, railDone))
				lines := railText(a, a.viewHeight())
				view := strings.Join(lines, "\n")
				if !strings.Contains(view, "Done 2 "+glyphOpen) {
					t.Fatalf("the open group lost its count:\n%s", view)
				}
				rows := doneAttemptRows1556(lines)
				if len(rows) != 2 {
					t.Fatalf("Done 2 draws %d attempt rows, want two:\n%s", len(rows), view)
				}
				stopped, done := a.taskStateMark(a.tasks[1]), a.taskStateMark(a.tasks[2])
				if stopped == done {
					t.Fatalf("the stopped and done marks are the same: %q", stopped)
				}
				for _, mark := range []string{stopped, done} {
					seen := false
					for _, at := range rows {
						row := lines[at]
						if strings.Contains(row, mark+" ") {
							seen = true
							if !strings.Contains(row, "[senior-dev]") && !strings.Contains(row, "[sd]") {
								t.Fatalf("attempt lost its role badge: %q", row)
							}
						}
					}
					if !seen {
						t.Fatalf("no attempt wears state mark %q:\n%s", mark, view)
					}
				}
				head, foot := -1, len(lines)
				for i, line := range lines {
					if strings.Contains(line, "Done 2 "+glyphOpen) {
						head = i
					}
					if strings.Contains(line, "+ /task") {
						foot = i
					}
				}
				if head < 0 || rows[0] <= head || rows[1] >= foot {
					t.Fatalf("both attempts must stand under Done:\n%s", view)
				}
			})
		}
	}
}

func TestAFoldedGroupFoldsTheRunsItsRowsCarry(t *testing.T) {
	a := doneColumn1556(t, 180)
	lines := railText(a, a.viewHeight())
	if view := strings.Join(lines, "\n"); !strings.Contains(view, "Done 2 "+glyphShut) || len(doneAttemptRows1556(lines)) != 0 {
		t.Fatalf("folded Done shows an attempt or loses its count:\n%s", view)
	}
	railClick(t, a, a.bodyWidth()+3, railHeadY(t, a, railDone))
	if lines = railText(a, a.viewHeight()); len(doneAttemptRows1556(lines)) != 2 {
		t.Fatalf("opening Done does not reveal both attempts:\n%s", strings.Join(lines, "\n"))
	}
}

func TestAStoppedRowKeepsItsRoleBadge(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 180, 40
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(1, "rename LoadConfig", session.TaskRunning,
		session.TaskNotice{Program: "senior-dev"})})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(1, "rename LoadConfig", session.TaskFailed,
		session.TaskNotice{Program: "senior-dev", Stopped: true, Ending: session.TaskEndingStopped})})
	railClick(t, a, a.bodyWidth()+3, railHeadY(t, a, railDone))
	row, ok := railRowFor(a, a.viewHeight(), "rename LoadConfig")
	if !ok || !strings.Contains(row, "[senior-dev]") {
		t.Fatalf("the stopped row lost its worker badge in the drawn column:\n%s",
			strings.Join(railText(a, a.viewHeight()), "\n"))
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
	a.width, a.height = 180, 40
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(1, "finished work", session.TaskDone, session.TaskNotice{})})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(2, "running work", session.TaskRunning, session.TaskNotice{})})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(3, "queued work", session.TaskQueued, session.TaskNotice{})})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(4, "waiting work", session.TaskQueued,
		session.TaskNotice{DependsOn: []uint64{2}})})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(5, "failed work", session.TaskRunning, session.TaskNotice{})})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(5, "failed work", session.TaskFailed,
		session.TaskNotice{Ending: session.TaskEndingError})})
	view := strings.Join(railText(a, a.viewHeight()), "\n")
	for _, want := range []string{"Done 1 " + glyphShut, "Running 1", "running work", "Queued 1 " + glyphShut,
		"Waiting 1 " + glyphShut, "failed work incomplete"} {
		if !strings.Contains(view, want) {
			t.Fatalf("folded Done changed another group or its count; missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "finished work") {
		t.Fatalf("folded Done still draws its row:\n%s", view)
	}
	railClick(t, a, a.bodyWidth()+3, railHeadY(t, a, railDone))
	if view = strings.Join(railText(a, a.viewHeight()), "\n"); !strings.Contains(view, "finished work") || !strings.Contains(view, "Done 1 "+glyphOpen) {
		t.Fatalf("opening Done did not reveal its counted row:\n%s", view)
	}
}
