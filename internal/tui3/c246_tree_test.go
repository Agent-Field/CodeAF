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

func TestPlanFamiliesStartExpandedAndEnterOpensThePage(t *testing.T) {
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
		if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != root.ID {
			t.Fatalf("enter on the folded family did not open its page: on=%v row=%q", a.taskSheet.planOn, a.taskSheet.plan.Row.ID)
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

func TestPlanPageUnderItDrawsWholeSubtreeLiveLinesAndReverseWaitCounts(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running"}
	handler := session.PlanTaskRow{ID: "t-handler", Title: "Handler", Parent: root.ID, Status: "running"}
	handler.Live.Step, handler.Live.Command = 2, "go test ./internal/auth/..."
	fixtures := session.PlanTaskRow{ID: "t-fixtures", Title: "Fixtures", Parent: handler.ID, Status: "pending", Waits: []string{handler.ID}}
	tests := session.PlanTaskRow{ID: "t-tests", Title: "Tests", Parent: root.ID, Status: "pending", Waits: []string{handler.ID}}
	kids := []session.PlanTaskRow{handler, fixtures, tests}
	pages := map[string]session.PlanTaskPage{root.ID: {Row: root, Children: kids}}
	a, _ := planAppWith(t, append([]session.PlanTaskRow{root}, kids...), pages)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	text := taskSheetText(a)
	// THE PARTS ARE THE RAIL'S ROWS: the part in flight names its call the way a
	// node row names one, and a part's row carries no count of the work queued
	// behind it, because a node row never did.
	for _, want := range []string{"under it", "Handler", "Fixtures", "Tests", "bash go test ./internal/auth/..."} {
		if !strings.Contains(text, want) {
			t.Fatalf("the subtree page is missing %q:\n%s", want, text)
		}
	}
	lines := strings.Split(text, "\n")
	var handlerLead, fixtureLead int
	for _, line := range lines {
		plain := ansi.Strip(line)
		if at := strings.Index(plain, "Handler"); at >= 0 {
			handlerLead = at
		}
		if at := strings.Index(plain, "Fixtures"); at >= 0 {
			fixtureLead = at
		}
	}
	if fixtureLead <= handlerLead {
		t.Fatalf("the grandchild is not deeper than its parent: handler=%d fixture=%d\n%s", handlerLead, fixtureLead, text)
	}
}

func TestEnterOnSubtreeRowOpensItAndEscapeReturnsToCallingPage(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root page", Status: "running"}
	child := session.PlanTaskRow{ID: "t-child", Title: "Child page", Parent: root.ID, Status: "pending"}
	pages := map[string]session.PlanTaskPage{
		root.ID:  {Row: root, Children: []session.PlanTaskRow{child}},
		child.ID: {Row: child},
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, child}, pages)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyDown})
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != child.ID {
		t.Fatalf("enter on the subtree row stayed on %q", a.taskSheet.plan.Row.ID)
	}
	if text := taskSheetText(a); !strings.Contains(text, "Root page"+roomCrumbSep+"Child page") {
		t.Fatalf("the child page has no parent breadcrumb:\n%s", text)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != root.ID {
		t.Fatalf("esc did not return to the calling page: on=%v row=%q", a.taskSheet.planOn, a.taskSheet.plan.Row.ID)
	}
}

func TestPlanPageWaitsOwnFirstThenTasksWaitingOnItAndOmitsEmptySection(t *testing.T) {
	handler := session.PlanTaskRow{ID: "t-handler", Title: "write the handler", Status: "running", Steps: 12}
	tests := session.PlanTaskRow{ID: "t-tests", Title: "write the tests", Status: "pending", Waits: []string{handler.ID}}
	fixtures := session.PlanTaskRow{ID: "t-fixtures", Title: "write the fixtures", Status: "pending", Waits: []string{tests.ID}}
	page := session.PlanTaskPage{Row: tests, WaitRows: []session.PlanTaskRow{handler, fixtures}}
	a, _ := planAppWith(t, []session.PlanTaskRow{handler, tests, fixtures}, map[string]session.PlanTaskPage{tests.ID: page})
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, a.taskSheetPlan(tests.ID)())
	text := taskSheetText(a)
	own := padTo("write the tests · waits: write the handler", 51) + tierGlyph(a.pal, planStatus(handler)) + " 12 steps"
	behind := padTo("write the fixtures · waits: write the tests", 51) + tierGlyph(a.pal, planStatus(fixtures)) + " queued"
	if !strings.Contains(text, "waits") || strings.Index(text, own) < 0 || strings.Index(text, behind) < 0 || strings.Index(text, own) > strings.Index(text, behind) {
		t.Fatalf("waits is not own-first in each row's sentence shape:\n%s", text)
	}

	leaf := session.PlanTaskRow{ID: "t-leaf", Title: "leaf", Status: "done"}
	a, _ = planAppWith(t, []session.PlanTaskRow{leaf}, map[string]session.PlanTaskPage{leaf.ID: {Row: leaf}})
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if text := taskSheetText(a); strings.Contains(text, "\n waits\n") {
		t.Fatalf("an empty waits section was drawn:\n%s", text)
	}
}

func TestPlanPageHeaderCountsRunningAndQueuedOverSubtreeDroppingZeros(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "root", Status: "done"}
	kids := []session.PlanTaskRow{
		{ID: "t-running", Title: "running child", Parent: root.ID, Status: "running"},
		{ID: "t-queued", Title: "queued child", Parent: root.ID, Status: "pending"},
		{ID: "t-done", Title: "done child", Parent: root.ID, Status: "done"},
	}
	a, _ := planAppWith(t, append([]session.PlanTaskRow{root}, kids...), map[string]session.PlanTaskPage{root.ID: {Row: root, Children: kids}})
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if text := taskSheetText(a); !strings.Contains(text, "1 running · 1 queued") {
		t.Fatalf("header does not count the subtree:\n%s", text)
	}

	a, _ = planAppWith(t, []session.PlanTaskRow{root}, map[string]session.PlanTaskPage{root.ID: {Row: root}})
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if text := taskSheetText(a); strings.Contains(text, "0 running") || strings.Contains(text, "0 queued") {
		t.Fatalf("zero header figures were drawn:\n%s", text)
	}
}
