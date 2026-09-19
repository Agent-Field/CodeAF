package session

import (
	"strings"
	"testing"
)

// The law these tests state: an ending caused by something its person set is
// never drawn as a fault, and the ending names the limit that caused it. A
// time limit and a dollar limit read apart on the reason line a person
// already reads; a person's stop keeps its own word and no reason; a worker's
// own error keeps the fault and its first line.

// TestRunLimitReasonsReadApartNamesTheLimit states the whole of the law for the
// two bounds a person sets on a run: the reason line names which limit ended
// it, carries no fault, and the two limits do not draw the same sentence.
func TestRunLimitReasonsReadApartNamesTheLimit(t *testing.T) {
	time := TaskReasonOf(TaskEndingTimeLimit, "a limit you set stopped it")
	cost := TaskReasonOf(TaskEndingCostLimit, "a limit you set stopped it")
	if time != "a time limit you set stopped it" {
		t.Fatalf("time-limit reason = %q, want the sentence that names the time limit", time)
	}
	if cost != "a dollar limit you set stopped it" {
		t.Fatalf("cost-limit reason = %q, want the sentence that names the dollar limit", cost)
	}
	if time == cost {
		t.Fatal("the two limits draw the same sentence: a person who set both cannot tell which fired")
	}
	for _, reason := range []string{time, cost} {
		if strings.HasPrefix(reason, taskReasonFault) {
			t.Fatalf("a limit its person set draws as a fault: %q", reason)
		}
	}
}

// TestRunLimitRowIsNotAFaultAndCarriesItsReason reads a limit-ended run's row
// the way a surface does: the ending says which limit, the row is incomplete
// without a fault, and the reason line is the limit's own sentence rather than
// the outcome word the report still carries.
func TestRunLimitRowIsNotAFaultAndCarriesItsReason(t *testing.T) {
	for _, tt := range []struct {
		name   string
		ending TaskEnding
		reason string
	}{
		{name: "time", ending: TaskEndingTimeLimit, reason: "a time limit you set stopped it"},
		{name: "cost", ending: TaskEndingCostLimit, reason: "a dollar limit you set stopped it"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			facts := TaskFacts{State: TaskFailed, Ending: tt.ending, Report: "a limit you set stopped it"}
			status := ProjectTask(facts)
			if status.Presence != TaskPresenceIncomplete {
				t.Fatalf("presence = %q, want incomplete", status.Presence)
			}
			if status.Fault {
				t.Fatal("a limit its person set is drawn as a fault")
			}
			if status.Reason != tt.reason {
				t.Fatalf("reason = %q, want %q", status.Reason, tt.reason)
			}
		})
	}
}

// TestRunPersonsStopStillReadsStoppedWithNoReason keeps the one road the law
// says nothing new about: a person's stop is its own word and carries no
// reason sentence at all.
func TestRunPersonsStopStillReadsStoppedWithNoReason(t *testing.T) {
	if got := TaskReasonOf(TaskEndingStopped, "stopped mid-flight"); got != "" {
		t.Fatalf("stopped reason = %q, want none", got)
	}
	status := ProjectTask(TaskFacts{State: TaskFailed, Ending: TaskEndingStopped, Stopped: true})
	if status.Presence != TaskPresenceStopped {
		t.Fatalf("presence = %q, want stopped", status.Presence)
	}
	if status.Reason != "" {
		t.Fatalf("stopped row carries a reason: %q", status.Reason)
	}
}

// TestRunWorkersOwnErrorKeepsItsFaultAndFirstLine keeps the other road the law
// says nothing new about: a worker's own error is a fault and stays one, with
// the report's first line as the account.
func TestRunWorkersOwnErrorKeepsItsFaultAndFirstLine(t *testing.T) {
	reason := TaskReasonOf(TaskEndingError, "the working copy could not be made\nand more followed")
	if reason != "a fault: the working copy could not be made" {
		t.Fatalf("error reason = %q, want the fault and the first line", reason)
	}
	status := ProjectTask(TaskFacts{State: TaskFailed, Ending: TaskEndingError, Report: "the working copy could not be made"})
	if !status.Fault {
		t.Fatal("a worker's own error is not drawn as a fault")
	}
}

// TestBeltRunNoticeCarriesTheLimitAsAnEnding proves the fact's crossing: a
// run ended by its time limit and a run ended by its dollar limit publish rows
// whose ending names the limit, drawn from the summary's own typed fact and
// never out of the outcome sentence.
func TestBeltRunNoticeCarriesTheLimitAsAnEnding(t *testing.T) {
	agent, _, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
	for _, tt := range []struct {
		name   string
		limit  RunLimit
		ending TaskEnding
	}{
		{name: "time", limit: RunLimitTime, ending: TaskEndingTimeLimit},
		{name: "cost", limit: RunLimitCost, ending: TaskEndingCostLimit},
	} {
		t.Run(tt.name, func(t *testing.T) {
			summary := RunSummary{Outcome: "a limit you set stopped it", Limit: tt.limit}
			notice := agent.beltRunNotice(run, summary, RunLanding{})
			if notice.Ending != tt.ending {
				t.Fatalf("run row ending = %q, want %q", notice.Ending, tt.ending)
			}
			status := ProjectTask(notice.StatusFacts())
			if status.Reason != TaskReasonOf(tt.ending, notice.Report) {
				t.Fatalf("drawn reason = %q, want the ending's own sentence", status.Reason)
			}
		})
	}
}
