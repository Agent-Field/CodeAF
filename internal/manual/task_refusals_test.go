package manual

import (
	"os"
	"strings"
	"testing"
)

// R7-16 and contract 16 keep the door's actionable refusals in the shipped chat manual.
func TestTaskComputerRefusalsExplainWhatToDo(t *testing.T) {
	raw, err := os.ReadFile("chat/tasks-on-another-computer.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, sentence := range []string{
		"taskdoor: empty frame", "taskdoor: a frame needs a type", "could not check your devices right now", "could not confirm this computer", "could not confirm this is <computer>",
		"this computer is already taking tasks", "that task attempt id is not readable", "that task is still stopping", "that task has already finished",
		"that task request is not readable", "choose landed or abandoned for this task",
		"the task copy's cell is not readable", "the task copy's head is not readable", "task copies need private storage and a relay blob store",
		"the task copy changed while it was being sent; try again", "the task's destination already contains files",
		"could not put the result in place; the earlier copy is kept at <path>: <reason>", "the sent task copy's fingerprint is missing",
		"a home folder or a folder above it cannot be sent as a task copy",
		"the task's remote record cannot be saved here", "the task's remote record is missing", "the other computer no longer has this task's answer",
		"the other computer sent an unreadable task event", "The result could not be brought from <computer>; the copy here was kept",
		"the task was interrupted on this computer before it left an answer", "the task ended without an answer", "the task ended without a result",
		"could not record the task's child: <reason>", "the task's saved event is too large", "The work could not be sent back: <reason>",
		"the recorded process does not name this task's work folder", "the recorded process does not name this task's private record",
		"the child's process start is not readable", "the recorded process is not this task's child", "this task's child is no longer running",
		"could not save the task result: <reason>", "the returned task copy must be landed by this computer",
	} {
		t.Run(sentence, func(t *testing.T) {
			if !strings.Contains(string(raw), "`"+sentence+"`") {
				t.Errorf("missing exact refusal and recovery guidance: %s", sentence)
			}
		})
	}
}
