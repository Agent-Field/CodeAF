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
// This file is the LOOP GUARD's half: a call refused for its arguments, sent
// again unchanged, is stopped at two with the correction in the note — where the
// counts that reached the person on that screen were nine and eleven.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// THE DEFECT ITSELF: `tasks {"limit":10.0}` answers with rows.
func TestTasksTakesAWholeNumberSentAsAFloat(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	index := TaskIndexPath(journal)
	for _, title := range []string{"Fix the nil-map crash", "Sweep the imports"} {
		appendTaskIndex(index, TaskIndexEntry{
			ID: title[:1], Name: title, Label: title, Title: title, Status: string(TaskDone),
		})
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})

	loose, isError := runTool(t, agent, "tasks", `{"limit":10.0}`)
	if isError {
		t.Fatalf(`tasks {"limit":10.0} was refused:\n%s`, loose)
	}
	for _, want := range []string{"Fix the nil-map crash", "Sweep the imports"} {
		if !strings.Contains(loose, want) {
			t.Fatalf("the answer is missing %q:\n%s", want, loose)
		}
	}

	// And the strict form answers exactly the same thing, because 10.0 IS ten.
	strict, isError := runTool(t, agent, "tasks", `{"limit":10}`)
	if isError || strict != loose {
		t.Fatalf("10 and 10.0 gave different answers:\n%s\n---\n%s", strict, loose)
	}

	// A number that is not whole is still refused, and the refusal is the call
	// that would have worked.
	half, isError := runTool(t, agent, "tasks", `{"limit":10.5}`)
	if !isError {
		t.Fatalf(`tasks {"limit":10.5} was accepted:\n%s`, half)
	}
	if want := `Invalid arguments: limit takes a whole number: send {"limit":10}, not 10.5`; half != want {
		t.Fatalf("refusal:\n want %q\n  got %q", want, half)
	}
}

// TWO IDENTICAL CALLS REFUSED FOR THEIR ARGUMENTS ARE A LOOP, and the note the
// model is handed is the tool's own corrected call rather than advice to think
// again — there is nothing to rethink.
func TestTwoIdenticalArgumentRefusalsAreNamedWithTheCorrectedCall(t *testing.T) {
	// The hand is named `lookup` rather than `tasks` because the real `tasks`
	// takes {"limit":10.0} now — that is the other half of this change — and a
	// test of the guard needs a call that is still refused.
	repair := `limit takes a whole number: send {"limit":10}, not 10.0`
	completer := &scriptedCompleter{steps: repeatedCalls("lookup", `{"limit":10.0}`, 2)}
	agent := loopAgent(t, completer, failingTool("lookup", invalidArgumentsPrefix+repair))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	fired := nudgeEvents(collect(t, events))
	if len(fired) != 1 {
		t.Fatalf("nudges: got %d, want exactly one at the second call (%v)", len(fired), fired)
	}
	if fired[0].Count != loopInvalidRepeats {
		t.Fatalf("the nudge fired at ×%d, want ×%d", fired[0].Count, loopInvalidRepeats)
	}
	if want := "stuck: lookup was sent the same wrong argument 2 times"; fired[0].Hint != want {
		t.Fatalf("the rule reads %q, want %q", fired[0].Hint, want)
	}

	notes := transcriptNotes(agent)
	if len(notes) != 1 {
		t.Fatalf("notes in the transcript: got %d, want 1 (%v)", len(notes), notes)
	}
	if !strings.Contains(notes[0], repair) {
		t.Fatalf("the note does not carry the corrected call:\n%s", notes[0])
	}
	if !strings.Contains(notes[0], "refused the same way each time") {
		t.Fatalf("the note does not say what happened:\n%s", notes[0])
	}
	// The advice for a loop out in the world is the wrong advice here.
	if strings.Contains(notes[0], "which assumption is wrong") {
		t.Fatalf("the note asks a model with the answer in hand to rethink:\n%s", notes[0])
	}
}

// A failure that is NOT an argument refusal keeps the old counts: nothing about
// a build that keeps breaking is known in advance, so two is a retry.
func TestAnOrdinaryFailureIsStillGivenThreeTries(t *testing.T) {
	completer := &scriptedCompleter{steps: repeatedCalls("build", `{}`, 2)}
	agent := loopAgent(t, completer, failingTool("build", "undefined: Frobnicate"))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if fired := nudgeEvents(collect(t, events)); len(fired) != 0 {
		t.Fatalf("two ordinary failures were nudged %d times, want 0 (%v)", len(fired), fired)
	}
}

// ONE LOOP IS NAMED ONCE. The argument rule fires at two; the general error rule
// must not then say the same thing again on the very next call, in different
// words, about the same repetition.
func TestAnArgumentLoopIsNotNamedTwiceInTwoVocabularies(t *testing.T) {
	completer := &scriptedCompleter{steps: repeatedCalls("lookup", `{"limit":10.0}`, 3)}
	agent := loopAgent(t, completer,
		failingTool("lookup", invalidArgumentsPrefix+`limit takes a whole number: send {"limit":10}, not 10.0`))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if fired := nudgeEvents(collect(t, events)); len(fired) != 1 {
		t.Fatalf("nudges over three identical refusals: got %d, want 1 (%v)", len(fired), fired)
	}
}
