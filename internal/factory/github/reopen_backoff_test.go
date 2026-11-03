package github_test

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestFailingReopensDoNotStarveAHealthyRow(t *testing.T) {
	f := newIssueScript(t, 41)
	for n := 1; n <= 40; n++ {
		if err := f.st.Update(n, func(it *factory.Item) error {
			it.State = factory.StateShipped
			it.SourceClosed = true
			it.DoneHow = "closed"
			it.GitHubIssueID = f.identity[n]
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		f.status[n] = 500
		f.lists = append(f.lists, map[string]any{"id": f.identity[n], "number": n, "state": "open", "title": "issue", "updated_at": f.now})
	}
	f.state[41] = "closed"
	for poll := 0; poll < 12; poll++ {
		for _, it := range f.lists {
			it["updated_at"] = f.now
			it["title"] = fmt.Sprint(poll)
		}
		f.poll()
	}
	if !f.row(41).SourceClosed {
		t.Fatalf("healthy #41 starved for 12 polls: priority failures consumed %d reads", len(f.reads))
	}
}
