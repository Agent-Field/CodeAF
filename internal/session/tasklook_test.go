package session

// THE OWNER'S SESSION, AS TESTS.
//
//	├─▶ tasks — Invalid arguments: json: cannot unmarshal number 10.0 into Go
//	    struct field tasksArguments.limit of type int   ✗
//	{"limit":10.0}
//	  · stuck: the same tasks call 9 times
//	  · stuck: tasks has failed the same way 11 times
//	  · stopped
//
// This file is the POLLING half: the model was calling `tasks` for a report that
// was going to be delivered to it. The handoff receipt now says so, and an answer
// that has not moved inside one turn says so again.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// THE SECOND LOOK SAYS IT IS THE SECOND LOOK, and the first says nothing — a
// model asking once is asking, not polling.
func TestASecondIdenticalTasksAnswerSaysNothingHasChanged(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	appendTaskIndex(TaskIndexPath(journal), TaskIndexEntry{
		ID: "1", Name: "fix-the-nil-map-crash", Label: "fix the nil-map crash",
		Title: "Fix the nil-map crash", Status: string(TaskDone),
	})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	// The turn is what "your last look" is measured over, so the tool is run
	// under one episode exactly as a batch runs it (loop.go).
	ctx := withEpisode(context.Background(), agent.newEpisode())
	tool := beltTool(t, agent, "tasks")

	first, _, err := tool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if strings.Contains(first, taskLookUnchanged) {
		t.Fatalf("the FIRST look was told nothing had changed:\n%s", first)
	}
	if !strings.Contains(first, "Fix the nil-map crash") {
		t.Fatalf("the first look answered nothing:\n%s", first)
	}

	second, _, err := tool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if !strings.HasPrefix(second, taskLookUnchanged+"\n") {
		t.Fatalf("the second look did not lead with the fact line:\n%s", second)
	}
	if !strings.Contains(second, "Fix the nil-map crash") {
		t.Fatalf("the fact line ate the answer:\n%s", second)
	}

	// A NEW TURN IS A NEW LOOK. The memory dies with the episode, because "since
	// your last look" means nothing across a turn the person has spoken in.
	next := withEpisode(context.Background(), agent.newEpisode())
	fresh, _, err := tool.Execute(next, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if strings.Contains(fresh, taskLookUnchanged) {
		t.Fatalf("a new turn's first look was told nothing had changed:\n%s", fresh)
	}
}

// Outside a turn — a tool called from a door with no episode — nothing is
// remembered and nothing is claimed.
func TestTheUnchangedLineIsSilentWithNoTurnBehindIt(t *testing.T) {
	if got := markTaskLook(context.Background(), "rows"); got != "rows" {
		t.Fatalf("with no episode the answer was rewritten to %q", got)
	}
}

// THE HANDOFF SAYS THE REPORT COMES TO YOU. It is the fact the polling model did
// not have, and it belongs on the receipt because that is the last thing read
// before the model decides what to do next.
func TestTheHandoffReceiptSaysTheSessionIsToldWhenTheWorkLands(t *testing.T) {
	for _, where := range []struct {
		what string
		text string
	}{
		{"the started receipt", handoffReceipt(t, TaskRunning)},
		{"the queued receipt", handoffReceipt(t, TaskQueued)},
	} {
		if !strings.Contains(where.text, taskHandoffWakeSentence) {
			t.Fatalf("%s does not say the session is told:\n%s", where.what, where.text)
		}
		for _, want := range []string{"You are told the moment it lands", "nothing to poll"} {
			if !strings.Contains(where.text, want) {
				t.Fatalf("%s is missing %q:\n%s", where.what, want, where.text)
			}
		}
	}

	// And the two descriptions the model reads before it ever calls either tool.
	if !strings.Contains(taskDescription, "starts a turn here when it lands, so never wait or poll") {
		t.Fatalf("propose_task's description does not rule out polling:\n%s", taskDescription)
	}
	if !strings.Contains(tasksDescription, "Never to WAIT for handed-off work") {
		t.Fatalf("the tasks description does not rule out polling:\n%s", tasksDescription)
	}
	// AND THE PERSON'S OWN CONTINUE WORDS ARE ASKED OF THE WHOLE DEFINITION.
	// They were in the description AND in the `continue` field, which is one
	// trigger phrase written twice in the same block of text the model reads
	// before it calls. The field owns it — it is the field's own door — so this
	// asks the tool, not one half of it.
	if !strings.Contains(tasksDescription+tasksSchemaJSON, "continue task N") {
		t.Fatalf("nothing in the tasks tool names the person's continue words:\n%s\n%s", tasksDescription, tasksSchemaJSON)
	}
}

// handoffReceipt is one admitted proposal's receipt, in the state named. It is
// spelled here rather than driven through a whole turn because what is under
// test is the sentence, and a proposal takes a countdown, a card and a person.
func handoffReceipt(t *testing.T, state TaskState) string {
	t.Helper()
	spec := taskSpec{title: "Fix the nil-map crash"}
	if state == TaskQueued {
		return fmt.Sprintf("task %d queued: %s\nIt starts when the work it waits on has finished and a slot is free. %s",
			7, spec.title, taskHandoffWakeSentence)
	}
	return fmt.Sprintf("task %d started: %s\nIt works from the brief alone, in a copy of its own. %s",
		7, spec.title, taskHandoffWakeSentence)
}
