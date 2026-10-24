package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestCompletionDisclosureFitsNarrowBodyClosedOpenAndWithDetails(t *testing.T) {
	a := completionFoldApp(t)
	a.turn = 2
	a.taskUpdate(update(9, "Restored manual task with a long title", session.TaskDone, session.TaskNotice{}))
	raw := "Original checker and spending details remain available.\n" + strings.Repeat("wide界 ", 12)
	a.entries = append(a.entries, entry{kind: entryNote, turn: 2, completion: &workCompletion{key: "task/9/0", task: 9, title: "Restored manual task with a long title", state: "done", text: raw}})
	for _, width := range []int{11, 40, 80, 120} {
		for _, open := range []bool{false, true} {
			for _, f := range a.deckFolds(a.conversation()) {
				a.setWorkOpen(a.conversation(), f.key, open)
			}
			for i := range a.entries {
				a.entries[i].open = open
			}
			rows, _ := a.deckRows(a.conversation(), width)
			found := false
			for _, r := range rows {
				if r.hit == hitWorkFold || r.completion != nil {
					found = true
					if got := ansi.StringWidth(r.text); got > width {
						t.Fatalf("completion at width %d open %v draws %d cells: %q", width, open, got, ansi.Strip(r.text))
					}
				}
			}
			if !found {
				t.Fatal("fixture lost its work disclosure")
			}
		}
	}
}
