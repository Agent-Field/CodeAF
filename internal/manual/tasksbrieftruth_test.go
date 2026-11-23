package manual

import (
	"strings"
	"testing"
)

// A task the conversation proposed on the run road reads only its own
// assignment — title, work, output and done-condition — and none of the
// conversation (session's proposalBrief). The section the model reads when
// asked what a task is must say so, and must not promise that a proposal
// carries the person's message and a journal pointer after its work.
func TestTasksManualSaysAProposedRunReadsOnlyItsAssignment(t *testing.T) {
	answer := chatSection(t, "tasks", "What a task is")
	for _, fact := range []string{
		"proposed on the run road reads only its own assignment",
		"with no message, quoted lines or journal pointer",
		"A task you typed yourself, or one on the older session-tree road, reads your own message word for word first",
	} {
		if !strings.Contains(answer, fact) {
			t.Errorf("what a task is does not say %q:\n%s", fact, answer)
		}
	}
	for _, stale := range []string{"A proposal on the run road puts its own work first"} {
		if strings.Contains(answer, stale) {
			t.Errorf("what a task is still promises a proposal carries the message: %q", stale)
		}
	}
}
