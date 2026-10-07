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

func TestTaskDeleteConfirmsBeforeRemovingAndNeverReopens(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	var deleted bool
	a.deleteTask = func(file, id string) error {
		if file != owner.Transcript || id != "t1" {
			t.Fatal("wrong deletion target")
		}
		deleted = true
		return nil
	}
	drive(t, a, key("right"))
	if !strings.Contains(taskSheetText(a), "x delete") {
		t.Fatal("task options do not offer deletion")
	}
	drive(t, a, key("x"))
	if !a.cdelete.on || deleted || a.cdelete.cursor != 0 {
		t.Fatal("x deleted without default-cancel confirmation")
	}
	drive(t, a, key("enter"))
	if deleted || a.cdelete.on {
		t.Fatal("default cancel was destructive")
	}
	drive(t, a, key("x"), key("down"), key("enter"))
	if !deleted {
		t.Fatal("yes did not reach the task deletion door")
	}
	a.taskSheet.query.setText("filings")
	a.taskSheetTyped()
	for _, item := range a.tasksFiltered().items {
		if item.entry.ID == "t1" && item.entry.SessionID == owner.ID {
			t.Fatal("search restored the deleted task")
		}
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
			drive(t, a, key("right"), key("t"))
			if task {
				drive(t, a, key("c"))
			}
			if !a.strip.open || started != "" {
				t.Fatal("a retired menu shortcut still acted")
			}
			copyKey, copied := "p", owner.Workspace
			if !task {
				copyKey, copied = "c", homeName(owner)
			}
			cmd, handled := a.stripKey(key(copyKey))
			if !handled || cmd == nil || !reflect.DeepEqual(cmd(), tea.Raw(osc52(copied, a.tmux))()) {
				t.Fatalf("%s copied the wrong text; want %q", copyKey, copied)
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

func TestTaskDeleteFilterTreatsXAsText(t *testing.T) {
	a := newSwitchLab(t).open(180, 40)
	selectHomeTask(t, a, "t1")
	a.taskSheet.query.setText("fi")
	a.taskSheetTyped()
	drive(t, a, key("x"))
	if a.cdelete.on || a.taskSheet.query.String() != "fix" {
		t.Fatal("x interrupted filter typing")
	}
}
