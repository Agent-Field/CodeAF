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

func TestPlanRailFoldsOnlyFinishedFamiliesAndFoldEnterOpensThePage(t *testing.T) {
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
		if !ok || !strings.Contains(line, "· 1 done") || strings.Contains(text, child.Title) {
			t.Fatalf("the finished family did not fold to one counted line:\n%s", text)
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
	for _, want := range []string{"under it", "Handler", "Fixtures", "Tests", "$ go test ./internal/auth/...", "· 2 queued behind it"} {
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
	if text := taskSheetText(a); !strings.Contains(text, "esc/← Root page") {
		t.Fatalf("the child page has no parent breadcrumb:\n%s", text)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != root.ID {
		t.Fatalf("esc did not return to the calling page: on=%v row=%q", a.taskSheet.planOn, a.taskSheet.plan.Row.ID)
	}
}
