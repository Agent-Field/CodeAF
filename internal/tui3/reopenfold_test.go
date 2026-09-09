package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A LONG TURN STRADDLES THE REPLAY WINDOW. Its tail is numbered zero until
// scrolling supplies the question; zero must not make finished work look live.
func TestReopenedLongTurnFoldsBeforeAndAfterBackfill(t *testing.T) {
	past := []session.DisplayEntry{{Role: "user", Text: "Please finish the work."}}
	for i := 0; i < replayTail; i++ {
		past = append(past,
			session.DisplayEntry{Role: "assistant", Text: "Checking the next file."},
			replayedCall("bash", "inspect-file", `{"command":"inspect-file"}`, "file checked"),
		)
	}
	past = append(past, session.DisplayEntry{Role: "assistant", Text: "The work is finished."})
	a := resumedApp(t, past...)
	check := func() {
		t.Helper()
		page := drawnText(a)
		if !strings.Contains(page, "worked") || !strings.Contains(page, "The work is finished.") || strings.Contains(page, "inspect-file") || strings.Contains(page, "Checking the next file") {
			t.Fatalf("reopened work did not collapse around its answer:\n%s", page)
		}
	}
	check()
	for a.moreHistory() {
		if !a.backfill() {
			t.Fatal("history stopped before the question")
		}
		check()
	}
	if !strings.Contains(drawnText(a), "Please finish the work.") {
		t.Fatal("backfill lost the person's question")
	}
	if !a.toggleLatestWorkfold() || !strings.Contains(drawnText(a), "Checking the next file") {
		t.Fatal("the folded work cannot be opened onto its outline")
	}
	if !a.toggleLatestCaption() || !strings.Contains(drawnText(a), "inspect-file") {
		t.Fatal("the outline cannot be opened onto its calls")
	}
}
