package tui3

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func homeTabsFixture(t *testing.T) (*app, []string) {
	t.Helper()
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	var files []string
	for i := 0; i < 7; i++ {
		files = append(files, lab.session("project", fmt.Sprintf("%016d", i+1), fmt.Sprintf("Conversation %d", i+1), workspace, time.Now().Add(-time.Duration(i)*time.Hour)))
	}
	a := lab.app(files[0])
	a.title, a.workspace = "Conversation 1", workspace
	a.width, a.height = 180, 45
	_ = a.tabsRow(a.width)
	for i, file := range files[1:4] {
		rememberUnheldTab(a, file, workspace, fmt.Sprintf("Conversation %d", i+2))
	}
	drain(t, a, a.openHome())
	return a, files
}

func homeConversationLines(a *app) (open, closed []homeLine) {
	for _, line := range a.home.lines {
		if line.kind != homeSession || line.cell == nil || line.cell.panel != panelRecent {
			continue
		}
		if line.cell.closed {
			closed = append(closed, line)
		} else {
			open = append(open, line)
		}
	}
	return
}

func assertHomeTabParity(t *testing.T, a *app) {
	t.Helper()
	open, _ := homeConversationLines(a)
	var home, tabs, chats []string
	for _, line := range open {
		home = append(home, line.row.Transcript)
	}
	for _, tab := range a.tabList() {
		tabs = append(tabs, tab.file)
	}
	rows, _, _ := a.hopReading(false)
	for _, row := range rows {
		chats = append(chats, row.file)
	}
	if !reflect.DeepEqual(home, tabs) || !reflect.DeepEqual(home, chats) {
		t.Fatalf("Home %v, tabs %v, chats %v", home, tabs, chats)
	}
}

func TestHomeConversationsMirrorTabsWithoutHeadingOrIndent(t *testing.T) {
	a, files := homeTabsFixture(t)
	assertHomeTabParity(t, a)
	open, closed := homeConversationLines(a)
	if len(open) != 4 || len(closed) != 0 {
		t.Fatalf("open=%d closed=%d", len(open), len(closed))
	}
	frame := homeText(a)
	if strings.Contains(frame, "threads") || strings.Contains(frame, "Conversation 5") {
		t.Fatalf("Home lists a heading or unopened history:\n%s", frame)
	}
	for _, line := range open {
		row := plain(a.homeCellRow(line, -1, 60, a.pal, false)[0])
		if !strings.HasPrefix(row, line.cell.title) {
			t.Fatalf("conversation is indented: %q", row)
		}
	}
	// A remembered tab has no held agent, but must still be on every list.
	rememberUnheldTab(a, files[4], a.workspace, "Conversation 5")
	assertHomeTabParity(t, a)
}

func TestHomeCloseClosesTheSelectedTabAndEnterReopensIt(t *testing.T) {
	a, files := homeTabsFixture(t)
	a.input.setText("keep the conversation draft")
	a.home.point(files[1])
	drive(t, a, key("right"), key("x"))
	if !a.at(pageHome) {
		t.Fatal("closing a Home row left Home")
	}
	if !a.tabShut[a.convKey(files[1])] {
		t.Fatal("the selected tab stayed open")
	}
	assertHomeTabParity(t, a)
	_, closed := homeConversationLines(a)
	if len(closed) != 1 || closed[0].row.Transcript != files[1] {
		t.Fatalf("wrong closed rows: %+v", closed)
	}
	if !strings.Contains(a.homeCellRow(closed[0], -1, 60, a.pal, true)[0], a.pal.dim(closed[0].cell.title)) {
		t.Fatal("closed conversation is not dim")
	}
	if a.input.String() != "keep the conversation draft" {
		t.Fatal("closing another row lost the draft")
	}
	a.home.point(files[1])
	drive(t, a, key("enter"))
	if a.at(pageHome) || a.file != files[1] || a.tabShut[a.convKey(files[1])] {
		t.Fatal("Enter did not reopen the closed conversation and tab")
	}
	drain(t, a, a.openHome())
	assertHomeTabParity(t, a)
}

func TestHomeClosedConversationsAreBoundedAndNewestFirst(t *testing.T) {
	a, files := homeTabsFixture(t)
	for _, file := range files[:4] {
		a.home.point(file)
		drive(t, a, key("right"), key("x"))
	}
	assertHomeTabParity(t, a)
	open, closed := homeConversationLines(a)
	if len(open) != 0 || len(closed) != homeClosedLimit {
		t.Fatalf("open=%d closed=%d", len(open), len(closed))
	}
	for i, line := range closed {
		if line.row.Transcript != files[3-i] {
			t.Fatalf("closed order %d = %q", i, line.row.Transcript)
		}
	}
	// Archived records also supply the bounded list without this close stack.
	fresh := a.newHomeView(a.readWorld(), true)
	fresh.closedTabs = nil
	_, saved := fresh.conversationRows()
	if len(saved) != homeClosedLimit {
		t.Fatalf("saved closed rows = %d", len(saved))
	}
}

func TestTabDismissUpdatesHomeWithoutAWorldRefresh(t *testing.T) {
	a, files := homeTabsFixture(t)
	tab, ok := chatTabAt(a.tabList(), a.convKey(files[2]))
	if !ok {
		t.Fatal("missing tab")
	}
	drain(t, a, a.tabDismiss(tab))
	assertHomeTabParity(t, a)
	_, closed := homeConversationLines(a)
	if len(closed) != 1 || closed[0].row.Transcript != files[2] {
		t.Fatal("tab close did not reach Home")
	}
}

func TestHomeShowsPersistedClosedConversationsAndSearchFindsOlderOnes(t *testing.T) {
	a, files := homeTabsFixture(t)
	for _, file := range files[4:] {
		if err := session.SetArchived(filepath.Dir(file), true); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshHome()
	_, closed := homeConversationLines(a)
	if len(closed) != homeClosedLimit {
		t.Fatalf("closed rows=%d", len(closed))
	}
	a.home.box.setText("Conversation 7")
	a.home.build()
	a.home.point(files[6])
	if row, ok := a.home.focusedLine(); !ok || row.row.Transcript != files[6] {
		t.Fatal("closed history is no longer searchable")
	}
}

func TestHomeCloseKeepsRunningWorkAndCurrentDraft(t *testing.T) {
	a, files := homeTabsFixture(t)
	busy := &busyAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.agent = busy
	a.input.setText("unfinished message")
	a.home.point(files[0])
	drive(t, a, key("right"), key("x"))
	if busy.closes != 0 || busy.stops != 0 || a.input.String() != "unfinished message" {
		t.Fatal("closing the Home row stopped work or lost the draft")
	}
	assertHomeTabParity(t, a)
	a.home.point(files[0])
	drive(t, a, key("enter"))
	if a.at(pageHome) || a.agent != busy || a.input.String() != "unfinished message" {
		t.Fatal("reopening the current tab did not retain its work and draft")
	}
}

func TestHomeCloseRefusalLeavesAllThreeListsIntact(t *testing.T) {
	a, files := homeTabsFixture(t)
	a.archive = func(string, bool) error { return fmt.Errorf("read only") }
	a.home.point(files[1])
	drive(t, a, key("right"), key("x"))
	assertHomeTabParity(t, a)
	if a.tabShut[a.convKey(files[1])] || a.home.msg != "could not close conversation" {
		t.Fatal("a failed close removed the tab or lost its explanation")
	}
}

func TestChatsMenuOpensRememberedTabsWithoutHeldAgents(t *testing.T) {
	a, files := homeTabsFixture(t)
	drive(t, a, key(hopOpenKey))
	for i, row := range a.hop.rows {
		if row.file == files[2] {
			a.hop.at = i
			break
		}
	}
	drive(t, a, key("enter"))
	if a.at(pageHome) || a.file != files[2] {
		t.Fatal("chats menu did not open its remembered tab")
	}
	drain(t, a, a.openHome())
	assertHomeTabParity(t, a)
}

func TestHomeConversationListsStayInSyncAcrossWidths(t *testing.T) {
	for _, width := range []int{50, 80, 120, 180} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, files := homeTabsFixture(t)
			a.width = width
			homeText(a)
			assertHomeTabParity(t, a)
			a.home.point(files[1])
			drive(t, a, key("ctrl+e"))
			assertHomeTabParity(t, a)
			_, closed := homeConversationLines(a)
			if len(closed) != 1 || closed[0].row.Transcript != files[1] {
				t.Fatal("closed row missing at this width")
			}
		})
	}
}
