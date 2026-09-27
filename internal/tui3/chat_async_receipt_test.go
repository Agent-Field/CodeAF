package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// Reproduce the real command road: slash returns before taskStartedMsg adds
// its ordinary acknowledgement. A synthetic told note misses this regression.
func TestCleanChatAsyncTaskReceiptDoesNotUnfoldEarlierCacheNote(t *testing.T) {
	for _, wake := range []bool{false, true} {
		a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
		a.entries = foldFixture()
		a.entries[5].confirmed = &responseConfirmation{done: true}
		a.turn = 1
		a.workMode = config.WorkFold
		a.feed.note("PRIVATE CACHE RECEIPT")
		a.toggleLatestWorkfold()
		a.setCapOpen(a.conversation(), 2, true)
		a.toggleLatestWorkfold()
		if a.workOpen[1] {
			t.Fatal("test did not close the old work")
		}
		_, _ = a.Update(taskMsg(a.slash("/task solo write a hello file")))
		last := len(a.entries) - 1
		if !strings.Contains(a.entries[last].text, "task 7 started") || a.entries[last].told {
			t.Fatalf("expected real async ordinary task acknowledgement: %+v", a.entries[last])
		}
		a.entries = append(a.entries, entry{kind: entryDone, turn: 1, done: &taskDone{id: 7, title: "hello file", ident: identFor(7)}})
		if wake {
			a.turn = 2
			a.state = stateWorking
		}
		a.touch()
		page := strings.Join(plainRows(a), "\n")
		for _, hidden := range []string{"PRIVATE CACHE RECEIPT", "task 7 started"} {
			if strings.Contains(page, hidden) {
				t.Fatalf("async landing exposed %q: %s", hidden, page)
			}
		}
		if !strings.Contains(page, "hello file") || !strings.Contains(page, "Done.") {
			t.Fatalf("lost notification or final answer: %s", page)
		}
		a.setWorkOpen(a.conversation(), 1, true)
		page = strings.Join(plainRows(a), "\n")
		if !strings.Contains(page, "PRIVATE CACHE RECEIPT") || !strings.Contains(page, "task 7 started") {
			t.Fatalf("operational receipts are not recoverable: %s", page)
		}
	}
}
