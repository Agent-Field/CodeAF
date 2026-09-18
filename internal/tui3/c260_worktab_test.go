package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The run page names both seats before its tree, and the seat is the last dim
// fact on an ordinary subtree row. A state that needs attention wins that
// scarce suffix: paused, failed, and held are actions, not model telemetry.
func TestWorkRunPageDrawsSeatsAndRowSeatUnlessStateOverridesIt(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "rewrite auth", Status: "running", Seat: "work"}
	kids := []session.PlanTaskRow{
		{ID: "t-live", Parent: root.ID, Title: "write handler", Status: "running", Seat: "work"},
		{ID: "t-paused", Parent: root.ID, Title: "write middleware", Status: "paused", Seat: "plan"},
		{ID: "t-failed", Parent: root.ID, Title: "write tests", Status: "failed", Seat: "check"},
		{ID: "t-held", Parent: root.ID, Title: "keep old table", Status: "held", Seat: "work"},
	}
	page := session.PlanTaskPage{Row: root, WorkModel: "glm-5.3-flash", PlanModel: "gpt-5.2", Children: kids}
	a, _ := planAppWith(t, append([]session.PlanTaskRow{root}, kids...), map[string]session.PlanTaskPage{root.ID: page})
	a.taskSheet.plan, a.taskSheet.planOn = page, true
	text := ansi.Strip(strings.Join(a.taskPlanBody(120), "\n"))
	if !strings.Contains(text, "work glm-5.3-flash · plan gpt-5.2") {
		t.Fatalf("the page has no seats line above its tree:\n%s", text)
	}
	for title, suffix := range map[string]string{
		"write handler": "work", "write middleware": "paused", "write tests": "failed", "keep old table": "held",
	} {
		line := lineContaining(text, title)
		if line == "" || !strings.HasSuffix(strings.TrimSpace(line), suffix) {
			t.Errorf("%q row = %q, want suffix %q", title, line, suffix)
		}
	}
}

// Every held question belongs above the tree. Its own option keys are the
// contract: the pane must not invent a generic answer legend, omit a second
// held task, or leave an empty `your call` heading behind.
func TestWorkRunPageDrawsEveryYourCallAboveTreeWithExistingAnswerKeys(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "migrate ledger", Status: "running"}
	child := session.PlanTaskRow{ID: "t-child", Parent: root.ID, Title: "write migration", Status: "running"}
	page := session.PlanTaskPage{
		Row: root, Children: []session.PlanTaskRow{child},
		Questions: []session.PlanTaskQuestion{
			{TaskID: "t-a", Head: "keep the old table for a week?", Options: []session.AnswerOption{{Key: "1", Label: "yes"}, {Key: "2", Label: "no"}}},
			{TaskID: "t-b", Head: "backfill before deploy?", Options: []session.AnswerOption{{Key: "a", Label: "before"}, {Key: "b", Label: "after"}}},
		},
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, child}, nil)
	a.taskSheet.plan, a.taskSheet.planOn = page, true
	text := ansi.Strip(strings.Join(a.taskPlanBody(120), "\n"))
	callAt, treeAt := strings.Index(text, "your call"), strings.Index(text, "under it")
	if callAt < 0 || treeAt < 0 || callAt > treeAt {
		t.Fatalf("your call is not above the tree:\n%s", text)
	}
	for _, want := range []string{"keep the old table for a week?", "1 yes", "2 no", "backfill before deploy?", "a before", "b after"} {
		if !strings.Contains(text, want) {
			t.Errorf("your call section is missing %q:\n%s", want, text)
		}
	}

	page.Questions = nil
	a.taskSheet.plan = page
	if empty := ansi.Strip(strings.Join(a.taskPlanBody(120), "\n")); strings.Contains(empty, "your call") {
		t.Fatalf("a run with no held question drew your call:\n%s", empty)
	}
}

// Tab changes only the reading of the one tree: state headings and counts in
// one column, with the same task strings. A second tab returns to the hierarchy.
func TestTabTogglesRunTreeByStateAndBackWithHeader(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "rewrite auth", Status: "running"}
	kids := []session.PlanTaskRow{
		{ID: "t-run", Parent: root.ID, Title: "write handler", Status: "running", Seat: "work"},
		{ID: "t-queue", Parent: root.ID, Title: "write tests", Status: "pending", Seat: "work"},
		{ID: "t-done", Parent: root.ID, Title: "read flow", Status: "done", Seat: "plan"},
	}
	page := session.PlanTaskPage{Row: root, Children: kids}
	a, _ := planAppWith(t, append([]session.PlanTaskRow{root}, kids...), nil)
	a.taskSheet.plan, a.taskSheet.planOn = page, true

	tree := ansi.Strip(strings.Join(a.taskPlanBody(120), "\n"))
	if !strings.Contains(tree, "by tree") {
		t.Fatalf("tree reading has no header:\n%s", tree)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	state := ansi.Strip(strings.Join(a.taskPlanBody(120), "\n"))
	for _, want := range []string{"by state", "running · 1", "queued · 1", "done · 1", "write handler", "write tests", "read flow"} {
		if !strings.Contains(state, want) {
			t.Errorf("by-state reading is missing %q:\n%s", want, state)
		}
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	back := ansi.Strip(strings.Join(a.taskPlanBody(120), "\n"))
	if !strings.Contains(back, "by tree") || strings.Contains(back, "by state") {
		t.Fatalf("second tab did not return to the tree:\n%s", back)
	}
}

func lineContaining(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// A run filter asks the same fuzzy matcher as home about every durable run
// fact. The winning field is retained so the pane can quote the line that made
// the run the top answer rather than merely moving the cursor without saying
// why.
func TestWorkRunFilterRanksEveryRunFactAndKeepsTheMatchingLine(t *testing.T) {
	pages := []session.PlanTaskPage{
		{Row: session.PlanTaskRow{ID: "t-first", Title: "rotate certificates"}, Description: "replace the expiring edge key", Notes: []session.PlanTaskNote{{Body: "canary stayed green"}}},
		{Row: session.PlanTaskRow{ID: "t-second", Title: "audit storage"}, Description: "inspect retention", Notes: []session.PlanTaskNote{{Body: "edge key appears in the archive"}}},
	}
	for _, tc := range []struct {
		name, query, wantID, wantLine string
	}{
		{"run title", "rot cert", "t-first", "rotate certificates"},
		{"task brief", "exp edge", "t-first", "replace the expiring edge key"},
		{"notes", "can grn", "t-first", "canary stayed green"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := filterPlanRuns(pages, nil, tc.query)
			if len(hits) == 0 || hits[0].page.Row.ID != tc.wantID || hits[0].line != tc.wantLine {
				t.Fatalf("filterPlanRuns(%q) = %#v, want %s quoted as %q", tc.query, hits, tc.wantID, tc.wantLine)
			}
		})
	}
}

// Conversation title and transcript snippets come from the search place's
// existing store answer. They are merged as facts, never re-indexed here.
func TestWorkRunFilterUsesTheHomeConversationIndexAndEscClearIsEmptyQuery(t *testing.T) {
	pages := []session.PlanTaskPage{{Row: session.PlanTaskRow{ID: "t-run", Title: "ship parser"}}}
	indexed := []searchHit{{sessionID: "t-run", title: "payments cleanup", snippet: "the cobalt handshake failed"}}
	for query, wantLine := range map[string]string{
		"pay cln": "payments cleanup",
		"cob hnd": "the cobalt handshake failed",
	} {
		hits := filterPlanRuns(pages, indexed, query)
		if len(hits) != 1 || hits[0].line != wantLine {
			t.Fatalf("indexed filter %q = %#v, want matching line %q", query, hits, wantLine)
		}
	}
	if hits := filterPlanRuns(pages, indexed, ""); len(hits) != len(pages) || hits[0].line != "" {
		t.Fatalf("cleared filter = %#v, want every run and no matching line", hits)
	}
}

func TestWorkRunPaneQuotesTheLineThatMatched(t *testing.T) {
	page := session.PlanTaskPage{
		Row:         session.PlanTaskRow{ID: "t-run", Title: "ship parser"},
		Description: "replace the expiring edge key",
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{page.Row}, map[string]session.PlanTaskPage{page.Row.ID: page})
	a.taskSheet.plan, a.taskSheet.planOn = page, true
	for _, r := range "exp edge" {
		a.taskSheet.query.insert(string(r))
	}
	text := ansi.Strip(strings.Join(a.taskPlanBody(100), "\n"))
	if !strings.Contains(text, "replace the expiring edge key") {
		t.Fatalf("the pane did not quote its matching line:\n%s", text)
	}
}
