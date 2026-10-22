package factory

import (
	"strings"
	"testing"
	"time"
)

func TestManagerStatusReadsSavedFactsWithoutImaginingProgress(t *testing.T) {
	now := time.Now()
	it := Item{State: StateNeedsYou, Question: "Merge both or restrict the form?", QKind: "step", Asking: &Asked{Stage: "plan", Question: "Merge both or restrict the form?", With: AskedYou, At: now}, Stream: &Stream{Phases: []Phase{{Name: "plan", State: PhaseRunning, Round: 1, At: now}}}}
	status := StatusForManager(it, false, now)
	if !strings.Contains(status.Text, "plan is waiting") || strings.Contains(status.Text, "Completed: plan") || !strings.Contains(status.Text, "No step work is running") || status.QuestionID == "" {
		t.Fatal(status.Text)
	}
	id := status.QuestionID
	it.Stream.Spent = 0.28
	if PendingQuestionID(it) != id {
		t.Fatal("spending invalidated a stable decision")
	}
	it.Stream.Phases[0].Round++
	if PendingQuestionID(it) == id {
		t.Fatal("a different round retained the old question identity")
	}
	it.Question, it.QKind, it.Asking = "", "", nil
	it.State = StateRunning
	if got := StatusForManager(it, false, now); !strings.Contains(got.Text, "has no live worker") {
		t.Fatal(got.Text)
	}
	if StatusOnlyRequest("Are we stuck? Please fix it") {
		t.Fatal("action request routed as status-only")
	}
}
