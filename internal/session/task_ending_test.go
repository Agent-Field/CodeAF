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
	if got := endingOfClaim("done: the parser is fixed", "", ""); got != "" {
		t.Fatalf("a finished claim ended as %q", got)
	}
	if got := endingOfClaim(loopLeftUndoneNote, "", ""); got != TaskEndingCircling {
		t.Fatalf("the loop guard's sentence ended as %q, want circling", got)
	}
	if got := endingOfClaim(loopLeftUndoneNote, "task 2 (slash parity)", ""); got != TaskEndingBlocked {
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
// and tells the person, and a wire that dropped is not a fault in the work.
func TestAHaltedLandingNoteNamesTheEndingAndARefusedOneStillSaysFailed(t *testing.T) {
	note := func(ending TaskEnding) string {
		return taskNote(TaskNotice{ID: 7, Title: "port the parser", State: TaskFailed, Ending: ending}, "", TaskSettleAsk, landingAddress{})
	}
	for ending, want := range map[TaskEnding]string{
		TaskEndingWire:     "task 7 lost the connection: port the parser",
		TaskEndingCircling: "task 7 went in circles: port the parser",
		TaskEndingBlocked:  "task 7 was blocked by another task: port the parser",
		TaskEndingSteps:    "task 7 ran out of steps: port the parser",
		TaskEndingRefused:  "task 7 failed: port the parser",
		TaskEndingError:    "task 7 failed: port the parser",
		"":                 "task 7 failed: port the parser",
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

// THE STOP IS A FACT, NOT A SENTENCE. A worker the write-your-notes rule ended
// is recognised by the answer the loop wrote down at the moment it stopped the
// turn (processrule.go's [ruleStopWitness]) — never by matching the line the
// person reads, which is prose somebody will one day improve.
func TestAWorkerStoppedByTheNotesRuleIsNamedByTheFactAndNotByItsWords(t *testing.T) {
	notes := (writeNotesRule{}).ending()
	if notes == "" {
		t.Fatal("the write-your-notes rule names no ending, so a worker it stops has nothing to wear")
	}
	if got := endingOfClaim("done: the parser is fixed", "", notes); got != notes {
		t.Fatalf("ending = %q, want %q: the fact did not name the ending", got, notes)
	}
	// AND IT OUTRANKS THE LAST WORDS. The stopped turn writes its landing line
	// into the transcript, so a run stopped here and one that circled are two
	// claims; the one the loop knows is the one that wins.
	if got := endingOfClaim(loopLeftUndoneNote, "task 2 (slash parity)", notes); got != notes {
		t.Fatalf("ending = %q, want %q", got, notes)
	}
	// AND THE PROSE IS NEVER THE EVIDENCE FOR IT: the very sentence a stopped
	// turn lands on names nothing on its own.
	if got := endingOfClaim((writeNotesRule{}).stopped(), "", ""); got != "" {
		t.Fatalf("the landing sentence was read as %q; the words are being matched", got)
	}
}

// A TURN THE RULE NEVER STOPPED SAYS SO, and a turn that was held once and then
// complied is a turn that complied: the witness is about ONE turn, so the next
// episode opens with nothing written on it.
func TestTheProcessRuleStopIsForgottenWhenTheNextTurnOpens(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := agent.stoppedOnProcessRule(); got != "" {
		t.Fatalf("a fresh agent reports the ending %q", got)
	}
	agent.ruleStop.stopped(writeNotesRule{})
	if got := agent.stoppedOnProcessRule(); got != TaskEndingNotes {
		t.Fatalf("ending = %q after the rule stopped the turn, want %q", got, TaskEndingNotes)
	}
	agent.newEpisode()
	if got := agent.stoppedOnProcessRule(); got != "" {
		t.Fatalf("the next turn opened carrying %q from the turn before it", got)
	}
}

// THE LANDING NOTE SAYS WHAT THE ROW SAYS. A worker stopped for its notes is
// halted news — nothing was found wrong with the work — so the note names it
// rather than saying "failed".
func TestAWorkerStoppedForItsNotesLandsAsHaltedNews(t *testing.T) {
	note := taskNote(TaskNotice{ID: 7, Title: "port the parser", State: TaskFailed, Ending: TaskEndingNotes},
		"", TaskSettleAsk, landingAddress{})
	if want := "task 7 would not write its notes down: port the parser"; !strings.HasPrefix(note, want) {
		t.Fatalf("the note opens %q, want %q", firstLines(note, 1), want)
	}
}
