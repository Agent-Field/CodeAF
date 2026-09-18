package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func c280Page() session.PlanTaskPage {
	now := taskFixtureNow
	return session.PlanTaskPage{
		Row:       session.PlanTaskRow{ID: "t-run", Title: "rewrite auth", Status: "running", Done: 2, Running: 1, Total: 5, USD: .21, Started: now.Add(-time.Hour)},
		WorkModel: "sonnet", PlanModel: "opus",
		Questions: []session.PlanTaskQuestion{{Head: "ship this?", Options: []session.AnswerOption{{Key: "1", Label: "yes"}, {Key: "2", Label: "no"}}}},
		Children: []session.PlanTaskRow{
			{ID: "t-live", Title: "write handler", Status: "running", Seat: "work"},
			{ID: "t-done", Title: "old test", Status: "done", Seat: "work"},
		},
		Notes: []session.PlanTaskNote{{Person: true, Body: "keep middleware order", At: now}},
	}
}

func TestC280SelectedRunPaneDrawsOrderedOptionalBlocks(t *testing.T) {
	page := c280Page()
	a, _ := planAppWith(t, append([]session.PlanTaskRow{page.Row}, page.Children...), map[string]session.PlanTaskPage{page.Row.ID: page})
	a.taskSheet.panePlan = map[string]session.PlanTaskPage{page.Row.ID: page}
	rows := a.taskPaneRun(page, 60)
	text := ansi.Strip(strings.Join(func() []string {
		out := []string{}
		for _, r := range rows {
			out = append(out, r.text)
		}
		return out
	}(), "\n"))
	last := -1
	for _, want := range []string{"rewrite auth", "2/5", "from ", "work sonnet", "your call", "ship this?", "write handler", "notes", "keep middleware order"} {
		at := strings.Index(text, want)
		if at < 0 || at < last {
			t.Fatalf("%q absent or out of order in:\n%s", want, text)
		}
		last = at
	}
}

func TestC280RunPaneRoomDropsTreeBottomThenNotes(t *testing.T) {
	page := c280Page()
	a, _ := planAppWith(t, append([]session.PlanTaskRow{page.Row}, page.Children...), map[string]session.PlanTaskPage{page.Row.ID: page})
	rows := a.taskPaneRunFit(page, 60, 8)
	text := ansi.Strip(strings.Join(func() []string {
		out := []string{}
		for _, r := range rows {
			out = append(out, r.text)
		}
		return out
	}(), "\n"))
	for _, want := range []string{"rewrite auth", "your call", "ship this?", "write handler"} {
		if !strings.Contains(text, want) {
			t.Fatalf("short pane dropped %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "old test") {
		t.Fatalf("short pane retained bottom tree row:\n%s", text)
	}
}

func TestC280RunPageReadIsACommandAndFrameDoesNotRead(t *testing.T) {
	page := c280Page()
	a, fake := planAppWith(t, []session.PlanTaskRow{page.Row}, map[string]session.PlanTaskPage{page.Row.ID: page})
	fake.pageReads = 0
	for range 5 {
		_ = paneText(a)
	}
	if fake.pageReads != 0 {
		t.Fatalf("drawing made %d PlanTaskPage calls", fake.pageReads)
	}
	cmd := a.taskPaneFollow()
	if cmd == nil {
		t.Fatal("selection move armed no run page read")
	}
	msg, ok := cmd().(taskPanePlanMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	a.taskPanePlanRead(msg)
	if fake.pageReads != 1 {
		t.Fatalf("command made %d page reads, want 1", fake.pageReads)
	}
}
