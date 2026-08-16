package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func arrivalBriefModel() *Model {
	model := New(&fakeBackend{}, "arrival")
	model.messages = []store.Message{
		{Seq: 2, SessionID: "arrival", Role: store.RoleUser, Body: "Earlier conversation."},
		{
			Seq: 9, SessionID: "arrival", Role: store.RoleAgent,
			Body: "While you were away: the release report landed and one choice is waiting.",
			Brief: &store.Brief{
				SinceSeq: 3, ThroughSeq: 8, Done: 1, Questions: 1, CostUSD: 0.82,
				Items: []store.BriefItem{
					{Kind: store.BriefDone, Body: "Release report — landed in /tmp/report.md with the migration table attached."},
					{Kind: store.BriefQuestion, Body: "The rollout is waiting on which region should receive it first."},
					{Kind: store.BriefSpend, Body: "$0.82 spent."},
				},
			},
		},
	}
	return model
}

// Journey #12 on the reading side: the brief is thread-first — a line in the
// conversation, at its tail, where the user is already looking — and it stays
// readable in a docked pane. Its rows used to be clipped to the pane, so at 60
// columns the surface that answers "what did I miss" answered in ellipses.
func TestTheReturnBriefIsThreadFirstAndReadableInTheDock(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		model := arrivalBriefModel()
		model.setSize(width, 30)

		collapsed := ansi.Strip(model.renderMessages())
		if !strings.Contains(collapsed, "▸ While you were away:") {
			t.Fatalf("width %d: the brief did not fold to one line:\n%s", width, collapsed)
		}
		if strings.LastIndex(collapsed, "While you were away:") < strings.LastIndex(collapsed, "Earlier conversation") {
			t.Fatalf("width %d: the brief is not at the thread tail:\n%s", width, collapsed)
		}
		assertFitsWidth(t, model.View(), width, "folded brief")

		model.briefExpanded[9] = true
		model.threadGen++
		expanded := ansi.Strip(model.renderMessages())
		for _, row := range []string{"Release report", "/tmp/report.md", "The rollout is waiting", "$0.82 spent."} {
			if !strings.Contains(expanded, row) {
				t.Fatalf("width %d: the opened brief lost %q:\n%s", width, row, expanded)
			}
		}
		// The fold line is one line everywhere and may clip; the rows under it
		// are the content and must not.
		for _, line := range strings.Split(ansi.Strip(model.renderBrief(model.messages[1], width-2, 0, false)), "\n")[1:] {
			if strings.Contains(line, "…") {
				t.Fatalf("width %d: an opened brief row clipped: %q", width, line)
			}
		}
		assertFitsWidth(t, model.View(), width, "opened brief")
	}
}
