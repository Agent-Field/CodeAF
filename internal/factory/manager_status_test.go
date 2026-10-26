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

func TestStatusOnlyRequestRecognizesNaturalCheckInsWithoutSwallowingWork(t *testing.T) {
	for _, text := range []string{
		"status", "status please", "what's the status", "what is the status", "where do we stand",
		"where do we stand here", "what's next", "what is next", "where do we stand here what's next",
		"where do we stand what's next", "are we stuck", "are we stuck here", "What’s the status?",
		"Everything ok here?", "Is everything okay?", "Everything all right so far?",
		"Are things on track?", "Are you making progress?", "Is the pipeline still running?",
		"How's it going?", "How is the plan coming along?", "How are things progressing?",
		"Any updates?", "Any progress on this?", "What's going on?", "What is taking so long?",
		"Where are we at?", "Where do we stand here? What's next?", "Status and what's next?",
		"Can you give me a status update, please?", "Could you please give me a status update?",
		"Hi, everything ok here, thank you?", "What is the status of the job?", "Any updates on the plan?", "Could you tell me the current status?",
		"Why are we paused?", "Why is the plan stuck?", "Have you finished yet?",
		"Are we stuck here?", "STATUS PLEASE!", "Please, any updates?", "Is there any news?",
	} {
		t.Run(text, func(t *testing.T) {
			if !StatusOnlyRequest(text) {
				t.Fatal("read-only check-in was routed as work")
			}
		})
	}
	for _, text := range []string{
		"", "?", "please", "Use the existing helper", "Can you check the tests?",
		"Are we stuck? Please fix it", "Everything ok here? Use the second ledger",
		"Status and fix the tests", "Any updates? Restart the pipeline", "What's next? Go ahead",
		"Update the status page", "Mark the PR ready", "How should we implement the status endpoint?",
		"Can you give me a status update and raise the budget?", "Any progress on deleting the old branch?",
		"Why is the plan stuck? Retry it", "Status of the endpoint should be 200",
		"Everything okay; push the branch", "Any updates\nDo not use the old API",
	} {
		t.Run(text, func(t *testing.T) {
			if StatusOnlyRequest(text) {
				t.Fatal("instruction or mixed request was swallowed by the status reader")
			}
		})
	}
}

func TestManagerStatusQueuedExplainsExecutionCapacityAndSettings(t *testing.T) {
	status := StatusForManager(Item{State: StateQueued}, false, time.Now())
	for _, want := range []string{"queued", "execution slot", "Settings → Factory → Your computers", "Idle PR monitoring does not use"} {
		if !strings.Contains(status.Text, want) {
			t.Fatalf("queued status missing %q: %s", want, status.Text)
		}
	}
	if strings.Contains(status.Text, "bench") {
		t.Fatal(status.Text)
	}
}
