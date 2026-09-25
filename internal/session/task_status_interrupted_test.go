package session

// WORK NOTHING IS DRIVING READS AS ITSELF.
//
// A row that was live when the process holding it went away used to come back
// stamped failed and stopped. That told a person two untrue things in one line:
// that something had gone wrong with the work, and that somebody had ended it.
// Nothing went wrong and nobody did anything. The window closed.
//
// These pin the reading rather than the pixels: the word, the tier it sits in,
// the two answers it offers, and the three states it must never be confused
// with.

import "testing"

func interruptedStatus() TaskStatus {
	return ProjectTask(TaskFacts{State: TaskInterrupted})
}

func TestWorkNothingIsDrivingSaysSoInTheOneWord(t *testing.T) {
	status := interruptedStatus()
	if status.Presence != TaskPresenceInterrupted {
		t.Fatalf("it reads as %q", status.Presence)
	}
	if status.Word != "interrupted" {
		t.Fatalf("the word is %q, and it is the one the owner asked for by name", status.Word)
	}
	// AND IT IS NOT A FAULT. Nothing was found out about the work at all, so
	// colouring it as something having broken would report a finding nobody made.
	if status.Fault {
		t.Fatal("a closed window was drawn as something having broken")
	}
}

// IT SAYS THE TWO FACTS A PERSON NEEDS AND ASKS NOTHING NOTHING CAN ANSWER.
// It used to sit in the person's tier offering `continue it` and `leave it`,
// with the needs-you mark raised; the door that carries a run on has no caller,
// so the question had no answer and the mark could never be cleared
// (run_lifecycle_test.go holds that half). What stays is the line.
func TestInterruptedWorkSaysWhatIsTrueOfIt(t *testing.T) {
	status := interruptedStatus()
	// AND THE REASON SAYS BOTH FACTS A PERSON NEEDS. Without the second one the
	// only safe-looking move is to start over.
	if status.Reason != "nothing is driving it; everything it did is kept" {
		t.Fatalf("the reason is %q", status.Reason)
	}
	if status.Ask != (TaskAsk{}) {
		t.Fatalf("it asks %+v, and no door takes an answer", status.Ask)
	}
}

// AND IT IS NOT ANY OF THE THREE IT WOULD OTHERWISE BE MISTAKEN FOR.
func TestInterruptedIsNotStoppedIncompleteOrDone(t *testing.T) {
	interrupted := interruptedStatus()
	for _, other := range []TaskStatus{
		ProjectTask(TaskFacts{State: TaskFailed, Stopped: true}),
		ProjectTask(TaskFacts{State: TaskFailed, Ending: TaskEndingSteps}),
		ProjectTask(TaskFacts{State: TaskDone}),
	} {
		if interrupted.Word == other.Word {
			t.Fatalf("interrupted work and %q work say the same word %q", other.Presence, other.Word)
		}
	}
}

// AND NO WORKER HOLDS IT. Settled here is the scheduler's word and not the
// person's: the slot is back, which is what keeps a restored row off the live
// tree, while the WORK is still waiting to be picked up.
func TestInterruptedWorkHoldsNoSlotAndIsStillNotOver(t *testing.T) {
	if !TaskInterrupted.settled() {
		t.Fatal("an interrupted row still reads as work in flight, so it would be counted as running")
	}
	if status := interruptedStatus(); status.Presence == TaskPresenceDone {
		t.Fatal("interrupted work reads as finished")
	}
}

// A ROW THAT WAS LIVE WHEN THE PROCESS WENT AWAY COMES BACK INTERRUPTED, and it
// invents no sentence about how it ended, because it did not end.
func TestARowThatWasLiveWhenTheProcessWentAwayComesBackInterrupted(t *testing.T) {
	for _, state := range []TaskState{TaskRunning, TaskQueued} {
		restored := runRowNotice(runRecord{ID: 9, Title: "port the parser", State: state})
		if restored.State != TaskInterrupted {
			t.Fatalf("a %s row came back %s", state, restored.State)
		}
		if restored.Stopped {
			t.Fatalf("a %s row came back as somebody's stop", state)
		}
		if restored.Report != "" {
			t.Fatalf("a %s row invents a sentence about how it ended: %q", state, restored.Report)
		}
	}
}

// AND A ROW THAT HAD ALREADY SETTLED IS LEFT EXACTLY AS IT WAS. Reading a
// conversation back is not an event in the life of work that was already over.
func TestARowThatHadSettledIsUnchangedByBeingReadBack(t *testing.T) {
	for _, state := range []TaskState{TaskDone, TaskFailed, TaskUnverified, TaskInterrupted} {
		restored := runRowNotice(runRecord{ID: 9, State: state, Report: "what it said"})
		if restored.State != state {
			t.Fatalf("a settled %s row was rewritten to %s by being read back", state, restored.State)
		}
		if restored.Report != "what it said" {
			t.Fatalf("a settled %s row lost what it said: %q", state, restored.Report)
		}
	}
}
