package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestHomeCloseKeepsProgressAndDeleteNeedsExplicitYes(t *testing.T) {
	for _, width := range []int{50, 80, 120, 180} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, files := homeTabsFixture(t)
			a.width = width
			worker := &fakeAgent{model: "m"}
			watch := &behindWatch{}
			watch.tasking.Store(true)
			a.behind = map[string]*kept{a.convKey(files[1]): {conv: Conversation{Agent: worker, SessionFile: files[1]}, watch: watch}}
			a.input.setText("keep the other draft")
			a.home.point(files[1])
			drive(t, a, key("right"), key("x"))
			if worker.closes != 0 || worker.stops != 0 {
				t.Fatal("close interrupted work")
			}
			if !a.tabShut[a.convKey(files[1])] {
				t.Fatal("close left the tab open")
			}
			a.home.point(files[1])
			line, ok := a.home.focusedLine()
			if !ok || line.row.Transcript != files[1] {
				t.Fatal("close hid the Home row")
			}
			if working, _ := a.homeChatState(line.cell); !working {
				t.Fatal("closed task work no longer shows in progress")
			}
			drive(t, a, key("right"), key("x"))
			if a.strip.prompt == "" {
				t.Fatal("delete did not ask for confirmation")
			}
			if text := plain(strings.Join(a.verbStripRow(width), "\n")); !strings.Contains(text, "delete is permanent") || !strings.Contains(text, "y yes") || !strings.Contains(text, "n no") {
				t.Fatalf("missing prompt: %s", text)
			}
			drive(t, a, key("enter"))
			if a.strip.prompt == "" || worker.closes != 0 {
				t.Fatal("Enter confirmed permanent deletion")
			}
			drive(t, a, key("n"))
			if !a.strip.open || a.strip.prompt != "" || len(a.strip.verbs) != 4 {
				t.Fatal("no did not return to the four actions")
			}
			if _, err := os.Stat(files[1]); err != nil {
				t.Fatal("cancel deleted the conversation")
			}
			drive(t, a, key("x"), key("y"))
			if worker.closes != 1 || worker.stops != 1 {
				t.Fatalf("delete did not stop and close the agent: %+v", worker)
			}
			if _, err := os.Stat(filepath.Dir(files[1])); !os.IsNotExist(err) {
				t.Fatalf("conversation folder remains: %v", err)
			}
			if a.input.String() != "keep the other draft" {
				t.Fatal("deletion changed another draft")
			}
			a.refreshHome()
			a.home.box.setText("Conversation 2")
			a.home.build()
			for _, line := range a.home.lines {
				if line.kind == homeSession && line.row.Transcript == files[1] {
					t.Fatal("deleted conversation reappeared in search")
				}
			}
			for _, tab := range a.closedTabs {
				if tab.file == files[1] {
					t.Fatal("deleted conversation remains reopenable")
				}
			}
		})
	}
}

func TestSessionsConversationHasTheSameCloseDeleteAndCancelActions(t *testing.T) {
	a, files := homeTabsFixture(t)
	a.showPage(pageTasks)
	selectChat := func() {
		t.Helper()
		for _, at := range a.taskSheet.stops(a) {
			a.taskSheet.cursor = at
			if chat, ok := a.taskSheetChat(); ok && chat.row.Transcript == files[1] {
				return
			}
		}
		t.Fatal("missing conversation in Sessions")
	}
	selectChat()
	drive(t, a, key("right"), key("right"))
	if !a.strip.open || len(a.strip.verbs) != 4 {
		t.Fatal("Sessions does not expose four conversation actions")
	}
	drive(t, a, key("x"))
	selectChat()
	drive(t, a, key("right"), key("right"), key("x"))
	if a.strip.prompt == "" {
		t.Fatal("Sessions delete bypassed confirmation")
	}
	drive(t, a, key("n"))
	if !a.strip.open || len(a.strip.verbs) != 4 {
		t.Fatal("cancel did not restore Sessions actions")
	}
	drive(t, a, key("x"), key("y"))
	if _, err := os.Stat(files[1]); !os.IsNotExist(err) {
		t.Fatalf("Sessions did not delete: %v", err)
	}
}

func TestNestedTaskDeleteKeepsItsConversationAndSibling(t *testing.T) {
	lab := newSwitchLab(t)
	lab.task("-beta", session.TaskIndexEntry{ID: "root", SessionID: "bbbb000000000001", Title: "Parent task", Status: string(session.TaskDone), EndedAt: lab.now})
	lab.task("-beta", session.TaskIndexEntry{ID: "t1", Parent: "root", SessionID: "bbbb000000000001", Title: "read 40 filings", Status: string(session.TaskRunning)})
	lab.task("-beta", session.TaskIndexEntry{Parent: "root", ID: "sibling", SessionID: "bbbb000000000001", Title: "Keep sibling", Status: string(session.TaskDone), EndedAt: lab.now})
	a := lab.open(180, 60)
	owner := selectHomeTask(t, a, "t1")
	drive(t, a, key("right"), key("x"))
	a.taskSheet.query.setText("read 40 filings")
	a.taskSheetTyped()
	for _, at := range a.taskSheet.stops(a) {
		a.taskSheet.cursor = at
		if item, ok := a.taskSheetCurrent(); ok && item.entry.ID == "t1" {
			break
		}
	}
	drive(t, a, key("right"), key("right"), key("x"))
	if a.strip.prompt == "" {
		t.Fatal("nested delete did not ask")
	}
	drive(t, a, key("n"))
	if !a.strip.open || len(a.strip.verbs) != 4 {
		t.Fatal("nested cancel did not restore actions")
	}
	drive(t, a, key("x"), key("y"))
	if _, err := os.Stat(owner.Transcript); err != nil {
		t.Fatalf("deleted the parent: %v", err)
	}
	meta, _ := session.LoadMeta(owner.Dir)
	if meta.DeletedTasks["sibling"] {
		t.Fatal("task deletion included a sibling")
	}
	if !meta.DeletedTasks["t1"] {
		t.Fatal("task deletion was not permanent")
	}
	for _, item := range a.tasksFiltered().items {
		if item.entry.SessionID == owner.ID && item.entry.ID == "t1" {
			t.Fatal("deleted task reappeared")
		}
	}
	records := session.ReadTaskIndex(session.TaskIndexPath(owner.Transcript))
	foundSibling := false
	for _, entry := range records {
		if entry.SessionID == owner.ID && entry.ID == "sibling" {
			foundSibling = true
		}
	}
	if !foundSibling {
		t.Fatal("sibling record was deleted")
	}
	fresh := lab.open(180, 60)
	fresh.showPage(pageTasks)
	for _, item := range fresh.tasksFiltered().items {
		if item.entry.SessionID == owner.ID && item.entry.ID == "t1" {
			t.Fatal("fresh scan resurrected deleted task")
		}
	}
}

func TestDeleteCurrentConversationInstallsANewOwner(t *testing.T) {
	a, files := homeTabsFixture(t)
	old := &fakeAgent{model: "m"}
	a.agent = old
	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: files[5], Workspace: a.workspace}, nil
	}
	a.home.point(files[0])
	drive(t, a, key("right"), key("x"))
	a.home.point(files[0])
	drive(t, a, key("right"), key("x"), key("y"))
	if a.agent != next || a.file != files[5] || old.closes != 1 {
		t.Fatal("foreground deletion did not replace its owner")
	}
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Fatalf("foreground history remains: %v", err)
	}
	if !a.at(pageHome) {
		t.Fatal("delete left Home")
	}
}

func TestDeletedConversationRejectsAnOlderTaskSnapshot(t *testing.T) {
	a, files := homeTabsFixture(t)
	stale := session.TaskIndexEntry{SessionID: filepath.Base(filepath.Dir(files[1])), ID: "1", Title: "Old cached work"}
	a.comp.tasks = []session.TaskIndexEntry{stale}
	a.home.point(files[1])
	drive(t, a, key("right"), key("x"))
	a.home.point(files[1])
	drive(t, a, key("right"), key("x"), key("y"))
	drive(t, a, tasksLoadedMsg{rows: []session.TaskIndexEntry{stale}, known: true})
	if len(a.comp.tasks) != 0 {
		t.Fatal("late index response restored the deleted conversation's task")
	}
	a.showPage(pageTasks)
	for _, item := range a.tasksFiltered().items {
		if item.entry.SessionID == stale.SessionID {
			t.Fatal("deleted conversation reappeared in Sessions")
		}
	}
}
