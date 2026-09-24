package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func pointSessionsConversation(t *testing.T, a *app, file string) {
	t.Helper()
	for _, at := range a.taskSheet.stops(a) {
		a.taskSheet.cursor = at
		if chat, ok := a.taskSheetChat(); ok && chat.row.Transcript == file {
			return
		}
	}
	t.Fatal("conversation missing from Sessions")
}

func assertConversationClosedEverywhere(t *testing.T, a *app, file string, closed bool) {
	t.Helper()
	meta, err := session.LoadMeta(filepath.Dir(file))
	if err != nil || meta.Archived != closed {
		t.Fatalf("saved closed=%v, want %v: %v", meta.Archived, closed, err)
	}
	foundTab, foundMenu := false, false
	for _, tab := range a.tabList() {
		if tab.file == file {
			foundTab = true
		}
	}
	rows, _, _ := a.hopReading(false)
	for _, row := range rows {
		if row.file == file {
			foundMenu = true
		}
	}
	if foundTab == closed || foundMenu == closed {
		t.Fatalf("closed=%v, tab=%v, default menu=%v", closed, foundTab, foundMenu)
	}
	rows, _, _ = a.hopReading(true)
	found := false
	for _, row := range rows {
		if row.file == file {
			found = true
		}
	}
	if !found {
		t.Fatal("saved conversation absent from the expanded chats menu")
	}
	drain(t, a, a.openHome())
	a.home.point(file)
	line, ok := a.home.focusedLine()
	if !ok || line.row.Transcript != file || line.cell.closed != closed {
		t.Fatal("Home disagrees about closure")
	}
	drain(t, a, a.showPage(pageTasks))
	r := a.tasksFiltered()
	lines := r.lay(a.taskSheetListWidth())
	for at, line := range lines {
		if line.kind != tasksLineChat || line.chat.row.Transcript != file {
			continue
		}
		if line.chat.row.Archived != closed {
			t.Fatal("Sessions disagrees about closure")
		}
		// Selection must not brighten a closed name; progress retains its own mark.
		for _, lit := range []bool{false, true} {
			painted := r.paint(lines, at, a.taskSheetListWidth(), a.pal, lit)
			if closed && !strings.Contains(painted, a.pal.dim(line.chat.title)) {
				t.Fatalf("closed name is not dark grey: %q", painted)
			}
			if !closed && !strings.Contains(painted, placeSubject(line.chat.title, lit, a.pal)) {
				t.Fatal("reopened name is still dim")
			}
		}
		return
	}
	t.Fatal("conversation missing from Sessions")
}

func TestConversationClosureStaysInSyncAcrossEveryDoor(t *testing.T) {
	for _, width := range []int{50, 80, 120, 180} {
		for _, door := range []string{"home", "sessions", "tab", "chats"} {
			t.Run(fmt.Sprintf("%d/%s", width, door), func(t *testing.T) {
				a, files := homeTabsFixture(t)
				a.width = width
				open := a.open
				a.open = func(workspace, transcript string) (Conversation, error) {
					conv, err := open(workspace, transcript)
					meta, _ := session.LoadMeta(filepath.Dir(transcript))
					conv.Agent = &twoTitleAgent{fakeAgent: &fakeAgent{model: "m"}, full: meta.Title}
					return conv, err
				}
				file := files[1]
				switch door {
				case "home":
					a.home.point(file)
					drive(t, a, key("right"), key("x"))
				case "sessions":
					drain(t, a, a.showPage(pageTasks))
					pointSessionsConversation(t, a, file)
					drive(t, a, key("right"), key("right"), key("x"))
				case "tab":
					tab, _ := chatTabAt(a.tabList(), a.convKey(file))
					drain(t, a, a.tabDismiss(tab))
				case "chats":
					a.hopOpen()
					for i, row := range a.hop.rows {
						if row.file == file {
							a.hop.at = i
						}
					}
					drain(t, a, a.hopAway())
					a.hopClose()
				}
				assertConversationClosedEverywhere(t, a, file, true)
				// Reopen from the chats menu, which must clear both saved and window state.
				a.hopOpen()
				a.hopSpread(true)
				for i, row := range a.hop.rows {
					if row.file == file {
						a.hop.at = i
					}
				}
				drain(t, a, a.hopTake())
				assertConversationClosedEverywhere(t, a, file, false)
			})
		}
	}
}

func TestDeleteRemovesConversationFromEveryNavigationSurface(t *testing.T) {
	for _, door := range []string{"home", "sessions"} {
		t.Run(door, func(t *testing.T) {
			a, files := homeTabsFixture(t)
			file := files[1]
			if door == "home" {
				a.home.point(file)
				drive(t, a, key("right"), key("x"))
				a.home.point(file)
				drive(t, a, key("right"), key("x"), key("y"))
			} else {
				drain(t, a, a.showPage(pageTasks))
				pointSessionsConversation(t, a, file)
				drive(t, a, key("right"), key("right"), key("x"))
				pointSessionsConversation(t, a, file)
				drive(t, a, key("right"), key("right"), key("x"), key("y"))
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatal("deleted conversation still exists")
			}
			for _, tab := range append(a.tabList(), a.closedTabs...) {
				if tab.file == file {
					t.Fatal("deleted conversation remains in tabs or reopen history")
				}
			}
			rows, _, _ := a.hopReading(true)
			for _, row := range rows {
				if row.file == file {
					t.Fatal("deleted conversation remains in chats menu")
				}
			}
			drain(t, a, a.openHome())
			for _, line := range a.home.lines {
				if line.row.Transcript == file {
					t.Fatal("deleted conversation remains in Home")
				}
			}
			drain(t, a, a.showPage(pageTasks))
			for _, row := range a.tasksFiltered().chats {
				if row.Transcript == file {
					t.Fatal("deleted conversation remains in Sessions")
				}
			}
		})
	}
}

func TestSwitchingAwayDoesNotReopenAClosedForegroundConversation(t *testing.T) {
	a, files := homeTabsFixture(t)
	a.home.point(files[0])
	drive(t, a, key("right"), key("x"))
	a.home.point(files[1])
	drive(t, a, key("enter"))
	assertConversationClosedEverywhere(t, a, files[0], true)
	// The browser-style reopen shortcut restores the same saved state, too.
	drain(t, a, a.reopenClosedTab())
	assertConversationClosedEverywhere(t, a, files[0], false)
}

func TestSessionsDeleteCurrentConversationKeepsItsSavedIdentity(t *testing.T) {
	a, files := homeTabsFixture(t)
	old := &fakeAgent{model: "m"}
	next := &fakeAgent{model: "m"}
	a.agent = old
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: files[5], Workspace: a.workspace}, nil
	}
	drain(t, a, a.showPage(pageTasks))
	pointSessionsConversation(t, a, files[0])
	chat, ok := a.taskSheetChat()
	if !ok || chat.row.Dir != filepath.Dir(files[0]) {
		t.Fatalf("Sessions lost the current conversation's saved folder: %+v", chat.row)
	}
	drive(t, a, key("right"), key("right"), key("x"))
	meta, _ := session.LoadMeta(filepath.Dir(files[0]))
	if !meta.Archived {
		t.Fatal("Sessions Close did not save the current conversation's closed state")
	}
	pointSessionsConversation(t, a, files[0])
	drive(t, a, key("right"), key("right"), key("x"), key("y"))
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Fatalf("Sessions failed to delete the current conversation: %v; notice %q", err, a.taskSheet.actionNote)
	}
	if a.agent != next || old.closes != 1 || old.stops != 1 {
		t.Fatal("Sessions deletion did not stop and replace the old agent exactly once")
	}
	if !a.at(pageTasks) {
		t.Fatal("deletion left Sessions")
	}
}

func TestSessionsLiveRowRetainsSavedOwnershipAndClosureMetadata(t *testing.T) {
	a, files := homeTabsFixture(t)
	meta, err := session.LoadMeta(filepath.Dir(files[0]))
	if err != nil {
		t.Fatal(err)
	}
	meta.Owned = true
	meta.Archived = true
	meta.ArchivedTasks = map[string]bool{"closed-task": true}
	meta.DeletedTasks = map[string]bool{"deleted-task": true}
	if err := session.SaveMeta(filepath.Dir(files[0]), meta); err != nil {
		t.Fatal(err)
	}
	drain(t, a, a.showPage(pageTasks))
	pointSessionsConversation(t, a, files[0])
	chat, ok := a.taskSheetChat()
	if !ok || chat.row.Dir != filepath.Dir(files[0]) || !chat.row.Owned || !chat.row.ArchivedTasks["closed-task"] || !chat.row.DeletedTasks["deleted-task"] {
		t.Fatalf("live status erased saved ownership or task visibility: %+v", chat.row)
	}
	// A conversation younger than the latest world scan still knows its folder.
	a.owned = true
	row := a.taskSheetSelfRow()
	if row.Dir != filepath.Dir(files[0]) || row.ID != filepath.Base(row.Dir) || !row.Owned {
		t.Fatalf("unscanned live identity is incomplete: %+v", row)
	}
}
