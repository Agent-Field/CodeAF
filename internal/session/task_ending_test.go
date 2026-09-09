package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A STREAM THAT RESETS MID-BODY IS THE WIRE, AND THE WIRE IS RETRIED. Three
// task nodes in one evening died on attempt 1 of
// "decode stream: read tcp …: read: connection reset by peer" — one of them
// twenty-eight minutes in, suite green, writing its landing note — because the
// retryable pattern knew "reset before headers" and not a reset after them.
func TestAMidStreamConnectionResetIsRetryableAndIsTheWire(t *testing.T) {
	for _, message := range []string{
		"decode stream: read tcp 192.168.2.13:51346->104.18.3.115:443: read: connection reset by peer",
		"write tcp 10.0.0.2:4431->1.2.3.4:443: write: broken pipe",
		"decode stream: unexpected EOF",
	} {
		if !isRetryable(message) {
			t.Errorf("%q is the wire and was not retryable", message)
		}
		if !diedOnTheWire(errors.New(message)) {
			t.Errorf("%q did not read as a run that died on the wire", message)
		}
	}
	// AND A VERDICT ABOUT THE REQUEST IS NOT THE WIRE. A refusal the router made
	// on its own account, a context that overflowed and a cancel a person asked
	// for each have an answer of their own already.
	for _, err := range []error{
		refusalOf(400, "no endpoints found that support tool use", "", ""),
		errors.New("prompt is too long: context length exceeded"),
		fmt.Errorf("wrapped: %w", context.Canceled),
		nil,
	} {
		if diedOnTheWire(err) {
			t.Errorf("%v read as the wire", err)
		}
	}
}

// THE LAST WORDS NAME THE ENDING. The loop guard's own sentence is the one claim
// this package writes rather than the worker, and a worker whose every write was
// refused by another task's copy was queued, not circling.
func TestTheLoopGuardsSentenceIsACirclingEndingUnlessSomebodyHeldTheFiles(t *testing.T) {
	if got := endingOfClaim("done: the parser is fixed", ""); got != "" {
		t.Fatalf("a finished claim ended as %q", got)
	}
	if got := endingOfClaim(loopLeftUndoneNote, ""); got != TaskEndingCircling {
		t.Fatalf("the loop guard's sentence ended as %q, want circling", got)
	}
	if got := endingOfClaim(loopLeftUndoneNote, "task 2 (slash parity)"); got != TaskEndingBlocked {
		t.Fatalf("a refused writer ended as %q, want blocked", got)
	}
}

// THE FIRST CAUSE WINS, AND A PERSON'S STOP OUTRANKS THE FIELD: the flag was the
// whole of that fact before the ending existed and it stays the source of it.
func TestAnEndingIsWrittenOnceAndAStopOutranksIt(t *testing.T) {
	graph := &TaskGraph{}
	ending := func(n *TaskNode) TaskEnding {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		return n.endingLocked()
	}
	node := &TaskNode{graph: graph, state: TaskFailed}
	node.end(TaskEndingCircling)
	node.end(TaskEndingRefused)
	if got := ending(node); got != TaskEndingCircling {
		t.Fatalf("ending = %q; the check's refusal overwrote the giving up", got)
	}
	node.markStopped()
	if got := ending(node); got != TaskEndingStopped {
		t.Fatalf("ending = %q on a node a person stopped", got)
	}
	done := &TaskNode{graph: graph, state: TaskDone}
	done.end(TaskEndingError)
	if got := ending(done); got != "" {
		t.Fatalf("a finished node carries an ending %q", got)
	}
}

// THE LANDING NOTE SAYS WHAT HAPPENED, NOT "FAILED": the model reads this line
// and tells the person, and a wire that dropped is not a fault in the work. The
// word is `incomplete` for every one of them and the REASON is what tells them
// apart, which is the one word and the one table every surface now draws from
// (task_status.go's [TaskReasonOf]).
func TestAHaltedLandingNoteNamesTheEndingAndNeverSaysFailed(t *testing.T) {
	note := func(ending TaskEnding) string {
		return taskNote(TaskNotice{ID: 7, Title: "port the parser", State: TaskFailed, Ending: ending}, "", TaskSettleAsk, landingAddress{})
	}
	for ending, want := range map[TaskEnding]string{
		TaskEndingWire:     "task 7 incomplete: port the parser · lost the connection",
		TaskEndingCircling: "task 7 incomplete: port the parser · went in circles",
		TaskEndingBlocked:  "task 7 incomplete: port the parser · was blocked by another task",
		TaskEndingSteps:    "task 7 incomplete: port the parser · ran out of steps",
		TaskEndingRefused:  "task 7 incomplete: port the parser · would not take a step it was asked to",
		TaskEndingError:    "task 7 incomplete: port the parser · a fault",
		"":                 "task 7 incomplete: port the parser · a fault",
	} {
		if got := note(ending); !strings.HasPrefix(got, want) {
			t.Errorf("%q: note opens %q, want %q", ending, firstLines(got, 1), want)
		}
	}
	stopped := taskNote(TaskNotice{ID: 7, Title: "port the parser", State: TaskFailed, Ending: TaskEndingWire, Stopped: true}, "", TaskSettleAsk, landingAddress{})
	if !strings.HasPrefix(stopped, "task 7 stopped: port the parser") {
		t.Errorf("a person's stop lost to the ending: %q", firstLines(stopped, 1))
	}
}

// AN OLD NOTES ENDING KEEPS ITS PERSON-FACING WORDS. Reopening a historical row
// must not turn its recorded reason into a generic failure.
func TestAHistoricalNotesEndingKeepsItsLandingWords(t *testing.T) {
	note := taskNote(TaskNotice{ID: 7, Title: "port the parser", State: TaskFailed, Ending: TaskEndingNotes},
		"", TaskSettleAsk, landingAddress{})
	if want := "task 7 incomplete: port the parser · would not write its notes down"; !strings.HasPrefix(note, want) {
		t.Fatalf("the note opens %q, want %q", firstLines(note, 1), want)
	}
}
