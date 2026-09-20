package tui3

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestTasksAgeHeaderReversesWithoutLosingSelection(t *testing.T) {
	a := tasksTableApp(t)
	before := tasksPage(a.tasksFiltered(), 122)
	selected, ok := a.taskSheet.rowAt(a, a.taskSheet.cursor)
	if !ok {
		t.Fatal("no selection")
	}
	x, y := tasksLabelAt(t, a, "age")
	a.taskSheetPress(x, y)
	if !a.taskSheet.order.back || !strings.Contains(tasksPage(a.tasksFiltered(), 122), "age ↑") {
		t.Fatal("age did not reverse")
	}
	after, ok := a.taskSheet.rowAt(a, a.taskSheet.cursor)
	if !ok || after != selected {
		t.Fatal("sort changed selected conversation")
	}
	groups := a.tasksFiltered().tree().in(tasksCompleted)
	for i := 1; i < len(groups); i++ {
		if groups[i-1].chat.rank.at.After(groups[i].chat.rank.at) {
			t.Fatal("oldest-first order is wrong")
		}
	}
	a.taskSheet.query.setText("corpus")
	filtered := a.tasksFiltered()
	if !filtered.tree().sort.back {
		t.Fatal("filter reset sort")
	}
	a.taskSheet.query.setText("")
	a.taskSheetPress(x, y)
	if got := tasksPage(a.tasksFiltered(), 122); got != before {
		t.Fatal("second click did not restore newest first")
	}
}

func TestTasksConnectTreesAndSeparateProjectFromTitle(t *testing.T) {
	now := time.Now()
	row := session.SessionRow{ID: "chat", Title: "Same title as Home", Project: "example", At: now, Transcript: "/chat/session.jsonl"}
	row.Tasks.Rows = []session.TaskIndexEntry{
		{SessionID: "chat", ID: "1", Label: "parent", Status: string(session.TaskDone), EndedAt: now},
		{SessionID: "chat", ID: "2", Parent: "1", Label: "child", Status: string(session.TaskDone), EndedAt: now},
		{SessionID: "chat", ID: "3", Label: "sibling", Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour)},
	}
	r := readTasks(session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}, tasksMine{}, session.LastDays(now, 7), tasksSort{}, time.Time{}, now)
	pal := newPalette(tokens.NoColor, false)
	lines := r.lay(120)
	for i, line := range lines {
		painted := plain(r.paint(lines, i, 120, pal, false))
		switch {
		case line.kind == tasksLineChat:
			if strings.Contains(painted, "Home · example") {
				t.Fatal("project still attached to title")
			}
			if !strings.HasSuffix(strings.TrimSpace(painted), pal.glyph(tokens.GExpanded)) {
				t.Fatalf("fold is not on right: %q", painted)
			}
			_, _, projectAt := tasksColumns(120, tasksByAge)
			if !strings.HasPrefix(ansi.Cut(painted, projectAt, projectAt+tasksProjectCells(120)), "example") {
				t.Fatalf("project outside column: %q", painted)
			}
		case line.kind == tasksLineTask && line.item.entry.ID == "2":
			want := pal.glyph(tokens.GTreeVert) + " " + pal.glyph(tokens.GTreeLast) + pal.glyph(tokens.GTreeDash)
			if !strings.Contains(painted, want) {
				t.Fatalf("ancestor connector absent: %q", painted)
			}
		case line.kind == tasksLineTask && line.item.entry.ID == "3":
			if !strings.Contains(painted, pal.glyph(tokens.GTreeLast)+pal.glyph(tokens.GTreeDash)) {
				t.Fatalf("last connector absent: %q", painted)
			}
		}
	}
}

func TestTasksRightHandFoldReceivesItsClick(t *testing.T) {
	a := tasksTableApp(t)
	_, hits, _, _ := a.taskSheetFrame(a.width, a.height)
	for y, hit := range hits {
		if hit.kind != taskSheetHitRow {
			continue
		}
		lines := a.tasksFiltered().lay(a.taskSheetListWidth())
		if hit.index < 0 || hit.index >= len(lines) || lines[hit.index].kind != tasksLineChat || !lines[hit.index].folds {
			continue
		}
		family := lines[hit.index].family
		a.taskSheetPress(a.taskSheetListWidth()-tasksColumnAir-1, y)
		if a.tasksFiltered().opens(family) {
			t.Fatal("click did not collapse root")
		}
		a.taskSheetPress(a.taskSheetListWidth()-tasksColumnAir-1, y)
		if !a.tasksFiltered().opens(family) {
			t.Fatal("click did not reopen root")
		}
		return
	}
	t.Fatal("no foldable conversation")
}
