package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Detail formatting remains a unit concern even though ordinary results no
// longer have a clickable receipt in the transcript. Inspect a snapshot rather
// than making the real conversation show a quiet card just for these checks.
func retainedLandingText(t *testing.T, a *app, id uint64) string {
	t.Helper()
	card := a.doneCardFor(id)
	if card == nil {
		t.Fatalf("task %d lost its retained result", id)
	}
	snapshot := *card
	snapshot.quiet, snapshot.dismissed, snapshot.open = false, false, true
	return plainOf(a.doneRows(&snapshot, a.bodyWidth(), false))
}

func TestRuntimeReceiptProjectionKeepsRequestedAnswersAndAlerts(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{
		{Role: "user", Text: "Check the report"},
		{Role: "aside", Text: "task 9 done: report", RoutineCompletion: true},
		{Role: "assistant", Text: "The report is ready.", Answer: true},
		{Role: "aside", Text: "Your requested watch matched."},
		{Role: "user", Text: "task 9 done: report"},
	}, chatReplay(0))
	a.touch()
	text := taskText(a)
	for _, want := range []string{"The report is ready."} {
		if !strings.Contains(text, want) {
			t.Fatalf("presentation hid substantive content %q: %s", want, text)
		}
	}
	retainedAlert := false
	for _, entry := range a.entries {
		retainedAlert = retainedAlert || entry.text == "Your requested watch matched."
	}
	if !retainedAlert {
		t.Fatal("runtime receipt projection removed the requested alert from its existing work disclosure")
	}
	if strings.Count(text, "task 9 done: report") != 1 {
		t.Fatalf("runtime receipt leaked or person's quote disappeared: %s", text)
	}
}
