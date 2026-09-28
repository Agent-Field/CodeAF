package tui3

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

func selectHomeTask(t *testing.T, a *app, id string) session.SessionRow {
	t.Helper()
	a.showPage(pageTasks)
	for _, at := range a.taskSheet.stops(a) {
		a.taskSheet.cursor = at
		if item, ok := a.taskSheetCurrent(); ok && item.entry.ID == id {
			return item.row
		}
	}
	t.Fatalf("home has no task %s", id)
	return session.SessionRow{}
}

func TestTaskClosePersistsAndCanBeRestoredFromTheTasksFilter(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	drive(t, a, key("right"))
	frame := taskSheetText(a)
	for _, word := range []string{"x close", "n new in project", "o open folder", "p copy project"} {
		if !strings.Contains(frame, word) {
			t.Fatalf("task options are missing %q:\n%s", word, frame)
		}
	}
	drive(t, a, key("a"))
	before, err := session.LoadMeta(owner.Dir)
	if err != nil || before.ArchivedTasks["t1"] {
		t.Fatalf("the retired a shortcut changed task visibility: %+v, %v", before, err)
	}
	drive(t, a, key("x"))
	meta, err := session.LoadMeta(owner.Dir)
	if err != nil || !meta.ArchivedTasks["t1"] || meta.Archived {
		t.Fatalf("task archive was not independent of its conversation: %+v, %v", meta, err)
	}
	for _, item := range a.tasksFiltered().items {
		if item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			t.Fatal("the open Sessions list still contains the task immediately after close")
		}
	}
	b := lab.open(180, 40)
	for _, line := range b.home.lines {
		if line.cell != nil && line.cell.row != nil && line.cell.row.task != nil && line.cell.row.task.ID == "t1" {
			t.Fatal("a fresh window restored a task that was put away")
		}
	}
	b.showPage(pageTasks)
	for _, item := range b.tasksFiltered().items {
		if item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			t.Fatal("the resting Tasks list still contains the put-away task")
		}
	}
	b.taskSheet.query.setText("read 40 filings")
	b.taskSheetTyped()
	found := false
	for _, at := range b.taskSheet.stops(b) {
		b.taskSheet.cursor = at
		if item, ok := b.taskSheetCurrent(); ok && item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			if !item.runs {
				t.Fatal("putting away the task stopped its running work")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the filter could not recover the archived task record")
	}
	drive(t, b, key("right"))
	if !strings.Contains(taskSheetText(b), "x reopen") {
		t.Fatal("the filtered task did not offer restore")
	}
	drive(t, b, key("x"))
	meta, _ = session.LoadMeta(owner.Dir)
	if meta.ArchivedTasks["t1"] {
		t.Fatal("restore did not persist")
	}
	selectHomeTask(t, lab.open(180, 40), "t1")
}

func TestTaskCloseFailureKeepsTheRowVisible(t *testing.T) {
	a := newSwitchLab(t).open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	a.putTaskAway(owner.Dir, session.TaskIndexEntry{ID: "t1", SessionID: "wrong-owner"}, true)
	selectHomeTask(t, a, "t1")
	meta, _ := session.LoadMeta(owner.Dir)
	if meta.ArchivedTasks["t1"] {
		t.Fatal("a mismatched task owner changed the row's visibility")
	}
}

func TestFinishedTasksOfferCloseAndFolderActions(t *testing.T) {
	lab := newSwitchLab(t)
	lab.presence("-beta", "bbbb000000000001", session.PresenceIdle, "", lab.now)
	lab.task("-beta", session.TaskIndexEntry{ID: "t1", SessionID: "bbbb000000000001",
		Title: "read 40 filings", Label: "read 40 filings", Status: string(session.TaskDone), EndedAt: lab.now})
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	drive(t, a, key("right"), key("x"))
	meta, _ := session.LoadMeta(owner.Dir)
	if !meta.ArchivedTasks["t1"] {
		t.Fatal("a finished task could not be put away")
	}
}

func TestProjectMenuShortcutsUseTheSelectedItemsProject(t *testing.T) {
	for _, task := range []bool{false, true} {
		name := "thread"
		if task {
			name = "task"
		}
		t.Run(name, func(t *testing.T) {
			var a *app
			var owner session.SessionRow
			if task {
				a = newSwitchLab(t).open(180, 40)
				owner = selectHomeTask(t, a, "t1")
			} else {
				a = placeAppOneColumn(t)
				placeFrameText(a)
				line, ok := a.home.focusedLine()
				if !ok || line.kind != homeSession {
					t.Fatal("fixture did not select a thread")
				}
				owner = line.row
			}
			var started string
			a.start = func(workspace string) (Conversation, error) {
				started = workspace
				return Conversation{}, errors.New("test captured the requested project")
			}
			drive(t, a, key("right"), key("t"), key("c"))
			if !a.strip.open || started != "" {
				t.Fatal("a retired menu shortcut still acted")
			}
			cmd, handled := a.stripKey(key("p"))
			if !handled || cmd == nil || !reflect.DeepEqual(cmd(), tea.Raw(osc52(owner.Workspace, a.tmux))()) {
				t.Fatal("p copy project did not copy the selected item's project")
			}
			drive(t, a, key("right"), key("n"))
			if started != owner.Workspace {
				t.Fatalf("n new in project started in %q, want %q", started, owner.Workspace)
			}
		})
	}
}

func TestTaskOptionsWrapWithoutDroppingActiveShortcuts(t *testing.T) {
	a, _ := tasksFootApp(t)
	drive(t, a, key("right"))
	rows := a.verbStripRow(32)
	for _, v := range a.strip.verbs {
		if !strings.Contains(plain(strings.Join(rows, "\n")), string(v.key)+" "+v.word) {
			t.Fatalf("narrow task options dropped %c %s", v.key, v.word)
		}
	}
}

func TestTaskOptionsSuspendTheLandingAnswerHints(t *testing.T) {
	a, _ := paneLandingLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok || len(a.taskPaneVerbs(item)) < 2 {
		t.Fatal("fixture has no landing answers")
	}
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatal("task options did not open")
	}
	verbs := a.taskPaneVerbs(item)
	if len(verbs) != 1 || verbs[0].key != questionEnterKey {
		t.Fatal("the pane advertised answer keys while task options owned them")
	}
	drive(t, a, key("left"))
	if len(a.taskPaneVerbs(item)) < 2 {
		t.Fatal("closing options did not restore the landing answers")
	}
}

func TestClosingAFilteredTaskRemovesItUntilTheFilterChanges(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	a.taskSheet.query.setText("read 40 filings")
	a.taskSheetTyped()
	for _, at := range a.taskSheet.stops(a) {
		a.taskSheet.cursor = at
		if item, ok := a.taskSheetCurrent(); ok && item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			break
		}
	}
	drive(t, a, key("right"), key("x"))
	if a.taskSheet.query.String() != "read 40 filings" {
		t.Fatal("closing a task discarded the filter")
	}
	for _, item := range a.tasksFiltered().items {
		if item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			t.Fatal("the filtered Sessions list still contains the task immediately after close")
		}
	}
	// A live update must not restore a row closed in the current search.
	a.railStamp++
	for _, item := range a.tasksFiltered().items {
		if item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			t.Fatal("a refresh restored the closed task")
		}
	}
	// An edit that leaves the search text as it was is not a new search: ctrl+k
	// with the caret already at the end of the box takes nothing.
	drive(t, a, key("ctrl+k"))
	if a.taskSheet.query.String() != "read 40 filings" {
		t.Fatalf("ctrl+k at the end changed the filter to %q", a.taskSheet.query.String())
	}
	for _, item := range a.tasksFiltered().items {
		if item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			t.Fatal("an edit that left the search text unchanged restored the closed task")
		}
	}
	a.taskSheet.query.setText("filings")
	a.taskSheetTyped()
	found := false
	for _, at := range a.taskSheet.stops(a) {
		a.taskSheet.cursor = at
		if item, ok := a.taskSheetCurrent(); ok && item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("a new search did not recover the closed task")
	}
	drive(t, a, key("right"))
	if !strings.Contains(taskSheetText(a), "x reopen") {
		t.Fatal("the recovered task did not offer reopen")
	}
	drive(t, a, key("x"))
	meta, err := session.LoadMeta(owner.Dir)
	if err != nil || meta.ArchivedTasks["t1"] {
		t.Fatalf("reopening the recovered task failed: %+v, %v", meta, err)
	}
}
