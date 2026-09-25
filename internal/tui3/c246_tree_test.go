package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A DEPENDENCY NEVER RE-PARENTS A ROW. Alpha was requested by A and waits on
// its cousin B, so the connector stays in A's family while the sentence names B.
func TestPlanTreeNestsByParentOnlyAcrossACousinWait(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "t-a", Title: "A", Status: "running"},
		{ID: "t-b", Title: "B", Status: "running"},
		{ID: "t-alpha", Title: "Alpha", Parent: "t-a", Status: "pending", Waits: []string{"t-b"}},
	}
	a, _ := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	kin := planDrawnKin(a)
	if ansi.StringWidth(kin["t-alpha"]) <= ansi.StringWidth(kin["t-a"]) {
		t.Fatalf("Alpha is not nested below A: A=%q Alpha=%q", kin["t-a"], kin["t-alpha"])
	}
	text := taskSheetText(a)
	aAt, alphaAt, bAt := strings.Index(text, "A"), strings.Index(text, "Alpha"), strings.Index(text, "B")
	if aAt < 0 || alphaAt < 0 || bAt < 0 || !(aAt < alphaAt && alphaAt < bAt) {
		t.Fatalf("the cousin wait moved Alpha out of A's family:\n%s", text)
	}
	line, _ := planLine(text, "Alpha")
	if !strings.Contains(line, "queued · waits: B") {
		t.Fatalf("Alpha reads %q, want `queued · waits: B`", line)
	}
}

func TestPlanFamiliesStartExpandedAndEnterOpensTheRoom(t *testing.T) {
	t.Run("finished family", func(t *testing.T) {
		root := session.PlanTaskRow{ID: "t-root", Title: "Finished family", Status: "done"}
		child := session.PlanTaskRow{ID: "t-child", Title: "landed child", Parent: root.ID, Status: "done"}
		pages := map[string]session.PlanTaskPage{root.ID: {Row: root, Children: []session.PlanTaskRow{child}}}
		a, _ := planAppWith(t, []session.PlanTaskRow{root, child}, pages)
		if !openTaskPlaceWithRows(a) {
			t.Fatal("the place refused to open over a plan")
		}
		text := taskSheetText(a)
		line, ok := planLine(text, root.Title)
		if !ok || !strings.Contains(line, "done") || !strings.Contains(text, child.Title) {
			t.Fatalf("the finished family did not show its child by default:\n%s", text)
		}
		drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
		if plan := a.roomPlan(); plan == nil || plan.id != root.ID || a.at(pageTasks) {
			t.Fatalf("enter on the family did not open its task room: room=%v tasks=%v", a.roomOpen(), a.at(pageTasks))
		}
	})
	t.Run("live family", func(t *testing.T) {
		root := session.PlanTaskRow{ID: "t-live", Title: "Live family", Status: "running"}
		child := session.PlanTaskRow{ID: "t-queued", Title: "queued child", Parent: root.ID, Status: "pending"}
		a, _ := planAppWith(t, []session.PlanTaskRow{root, child}, nil)
		if !openTaskPlaceWithRows(a) {
			t.Fatal("the place refused to open over a plan")
		}
		if !strings.Contains(taskSheetText(a), child.Title) {
			t.Fatalf("a family with queued work was folded:\n%s", taskSheetText(a))
		}
	})
}

func TestPlanRoomUnderItDrawsWholeSubtreeWithLiveLines(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running"}
	handler := session.PlanTaskRow{ID: "t-handler", Title: "Handler", Parent: root.ID, Status: "running"}
	handler.Live.Step, handler.Live.Command = 2, "go test ./internal/auth/..."
	fixtures := session.PlanTaskRow{ID: "t-fixtures", Title: "Fixtures", Parent: handler.ID, Status: "pending", Waits: []string{handler.ID}}
	tests := session.PlanTaskRow{ID: "t-tests", Title: "Tests", Parent: root.ID, Status: "pending", Waits: []string{handler.ID}}
	kids := []session.PlanTaskRow{handler, fixtures, tests}
	pages := map[string]session.PlanTaskPage{root.ID: {Row: root, Children: kids}}
	a, _ := planAppWith(t, append([]session.PlanTaskRow{root}, kids...), pages)
	a.height = 40
	openPlanRoomNow(t, a, root.ID)
	text := planRoomText(t, a)
	// THE PARTS ARE THE RAIL'S ROWS: the part in flight names its call the way a
	// node row names one, and a part's row carries no count of the work queued
	// behind it, because a node row never did.
	for _, want := range []string{"under it", "Handler", "Fixtures", "Tests", "bash go test ./internal/auth/..."} {
		if !strings.Contains(text, want) {
			t.Fatalf("the run's room is missing %q:\n%s", want, text)
		}
	}
	var handlerLead, fixtureLead int
	for _, line := range strings.Split(text, "\n") {
		if at := strings.Index(line, "Handler"); at >= 0 {
			handlerLead = at
		}
		if at := strings.Index(line, "Fixtures"); at >= 0 {
			fixtureLead = at
		}
	}
	if fixtureLead <= handlerLead {
		t.Fatalf("the grandchild is not deeper than its parent: handler=%d fixture=%d\n%s", handlerLead, fixtureLead, text)
	}
}

// A PART'S ROOM HANGS UNDER THE RUN'S ON ITS TRAIL, and the crumb is a door
// onto the run's own room. `esc` leaves for the conversation, as it does from
// every room.
func TestAPartsRoomNamesItsParentOnTheTrailAndTheCrumbOpensIt(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root page", Status: "running"}
	child := session.PlanTaskRow{ID: "t-child", Title: "Child page", Parent: root.ID, Status: "pending"}
	pages := map[string]session.PlanTaskPage{
		root.ID:  {Row: root, Children: []session.PlanTaskRow{child}},
		child.ID: {Row: child},
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, child}, pages)
	openPlanRoomNow(t, a, child.ID)
	if plan := a.roomPlan(); plan == nil || plan.id != child.ID {
		t.Fatal("the part's room did not open")
	}
	if trail := roomTrailText(t, a); !strings.Contains(trail, "Root page"+roomCrumbSep+"Child page") {
		t.Fatalf("the part's room has no parent crumb:\n%s", trail)
	}
	var parent roomCrumb
	for _, crumb := range a.roomCrumbs() {
		if crumb.word == "Root page" {
			parent = crumb
		}
	}
	if !parent.door() {
		t.Fatalf("the parent crumb is not a door: %+v", parent)
	}
	spend(t, a, a.openRailRoom(parent.node))
	if plan := a.roomPlan(); plan == nil || plan.id != root.ID {
		t.Fatal("the parent crumb did not open the run's own room")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.roomOpen() {
		t.Fatal("esc did not leave the room for the conversation")
	}
}

func TestPlanRoomWaitsOwnFirstThenTasksWaitingOnItAndOmitsEmptySection(t *testing.T) {
	handler := session.PlanTaskRow{ID: "t-handler", Title: "write the handler", Status: "running", Steps: 12}
	tests := session.PlanTaskRow{ID: "t-tests", Title: "write the tests", Status: "pending", Waits: []string{handler.ID}}
	fixtures := session.PlanTaskRow{ID: "t-fixtures", Title: "write the fixtures", Status: "pending", Waits: []string{tests.ID}}
	page := session.PlanTaskPage{Row: tests, WaitRows: []session.PlanTaskRow{handler, fixtures}}
	a, _ := planAppWith(t, []session.PlanTaskRow{handler, tests, fixtures}, map[string]session.PlanTaskPage{tests.ID: page})
	a.height = 40
	openPlanRoomNow(t, a, tests.ID)
	text := planRoomText(t, a)
	own := strings.Index(text, "write the tests · waits: write the handler")
	behind := strings.Index(text, "write the fixtures · waits: write the tests")
	if !strings.Contains(text, "waits") || own < 0 || behind < 0 || own > behind || !strings.Contains(text, "12 steps") {
		t.Fatalf("waits is not own-first in each row's sentence shape:\n%s", text)
	}

	leaf := session.PlanTaskRow{ID: "t-leaf", Title: "leaf", Status: "done"}
	a, _ = planAppWith(t, []session.PlanTaskRow{leaf}, map[string]session.PlanTaskPage{leaf.ID: {Row: leaf, Description: "the leaf's brief"}})
	openPlanRoomNow(t, a, leaf.ID)
	if text := planRoomText(t, a); strings.Contains(text, "waits") || strings.Contains(text, "under it") {
		t.Fatalf("an empty section was drawn:\n%s", text)
	}
}
