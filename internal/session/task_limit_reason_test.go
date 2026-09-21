package session

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
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

// TestRunEndingsThatKeepTheFault is the other half of the inventory: the
// endings that are the work's own and keep the fault reason. A run that ran
// and did not finish and a run that could not be run at all carry no ending a
// reading knows, and their account is the report's first line behind the fault
// word. A row the run's own ending cut mid-flight is NOT here any more: the
// run carries its typed record of what it cut ([RunSummary.Cut]) and the settle
// road draws those rows with the run's own ending
// ([TestRunJoinedRowsCutByAPersonsEndingNameIt]). What stays under this test is
// the reading itself: an ending nobody names, whatever the report, still draws
// the fault line.
func TestRunEndingsThatKeepTheFault(t *testing.T) {
	for _, tt := range []struct {
		name   string
		report string
	}{
		{name: "ran and did not finish", report: "ran and did not finish"},
		{name: "could not be run at all", report: "could not be run at all"},
		{name: "a break with words and no ending", report: "context canceled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reason := TaskReasonOf(TaskEnding(""), tt.report)
			want := "a fault: " + tt.report
			if reason != want {
				t.Fatalf("reason = %q, want %q", reason, want)
			}
			if !ProjectTask(TaskFacts{State: TaskFailed, Ending: TaskEnding(""), Report: tt.report}).Fault {
				t.Fatal("a row with no ending a reading knows is not drawn as a fault")
			}
		})
	}
}

// TestBeltRunNoticeWithoutALimitKeepsNoEnding keeps the fact honest in the
// other direction: a run that did not end on a bound publishes a row with no
// limit ending, so it draws the reading its outcome word always drew.
func TestBeltRunNoticeWithoutALimitKeepsNoEnding(t *testing.T) {
	agent, _, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
	notice := agent.beltRunNotice(run, RunSummary{Outcome: "ran and did not finish"}, RunLanding{})
	if notice.Ending != "" {
		t.Fatalf("run row ending = %q, want none for a run no limit ended", notice.Ending)
	}
	status := ProjectTask(notice.StatusFacts())
	if status.Reason != "a fault: ran and did not finish" {
		t.Fatalf("drawn reason = %q, want the fault and the outcome word", status.Reason)
	}
}

// TestRunJoinedRowsCutByAPersonsEndingNameIt holds the law on the road the
// checker named: a hand-off that joined a run and was taken down by the run's
// own ending is a row a person ended, not a fault. The run's ending and its
// typed record of what it cut ([RunSummary.Cut]) arrive through the real
// settle road, and the row reads the way the run row reads: the limit's own
// sentence, no fault, and the two limits reading apart; a person's stop reads
// stopped; a row that failed on its own keeps the fault.
func TestRunJoinedRowsCutByAPersonsEndingNameIt(t *testing.T) {
	for _, tt := range []struct {
		name    string
		summary RunSummary
		ending  TaskEnding
		reason  string
	}{
		{name: "time", summary: RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitTime, Cut: []string{"7"}}, ending: TaskEndingTimeLimit, reason: "a time limit you set stopped it"},
		{name: "cost", summary: RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitCost, Cut: []string{"7"}}, ending: TaskEndingCostLimit, reason: "a dollar limit you set stopped it"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			agent, store, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "7", Title: "the joined work", Description: "its brief", ParentID: run.root}}); err != nil {
				t.Fatalf("add joined task: %v", err)
			}
			if _, err := store.Claim("7", "7"); err != nil {
				t.Fatalf("claim joined task: %v", err)
			}
			if _, err := store.Fail("7", "7", "context canceled"); err != nil {
				t.Fatalf("fail joined task: %v", err)
			}
			run.joined = []uint64{7}
			agent.settleBeltRun(run, tt.summary, RunLanding{})
			rows := agent.graph().runRows(7)
			if len(rows) != 1 {
				t.Fatalf("the joined row published %d times, want once", len(rows))
			}
			row := rows[0]
			if row.Ending != tt.ending {
				t.Fatalf("joined row ending = %q, want the run's own %q", row.Ending, tt.ending)
			}
			status := ProjectTask(row.StatusFacts())
			if status.Fault {
				t.Fatalf("a row the person's %s limit took down is drawn as a fault", tt.name)
			}
			if status.Reason != tt.reason {
				t.Fatalf("joined row reason = %q, want %q", status.Reason, tt.reason)
			}
		})
	}
	t.Run("stop", func(t *testing.T) {
		agent, store, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
		if _, err := store.AddMany([]plandb.TaskSpec{{ID: "7", Title: "the joined work", Description: "its brief", ParentID: run.root}}); err != nil {
			t.Fatalf("add joined task: %v", err)
		}
		if _, err := store.Claim("7", "7"); err != nil {
			t.Fatalf("claim joined task: %v", err)
		}
		if _, err := store.Fail("7", "7", "context canceled"); err != nil {
			t.Fatalf("fail joined task: %v", err)
		}
		run.joined = []uint64{7}
		agent.settleStoppedBeltRun(run, "the person stopped it", []string{"7"})
		rows := agent.graph().runRows(7)
		if len(rows) != 1 {
			t.Fatalf("the joined row published %d times, want once", len(rows))
		}
		status := ProjectTask(rows[0].StatusFacts())
		if status.Fault {
			t.Fatal("a row the person's stop took down is drawn as a fault")
		}
		if status.Reason != "" {
			t.Fatalf("a stopped row draws the reason %q, want none", status.Reason)
		}
	})
	t.Run("own failure", func(t *testing.T) {
		agent, store, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
		if _, err := store.AddMany([]plandb.TaskSpec{{ID: "7", Title: "the joined work", Description: "its brief", ParentID: run.root}}); err != nil {
			t.Fatalf("add joined task: %v", err)
		}
		if _, err := store.Claim("7", "7"); err != nil {
			t.Fatalf("claim joined task: %v", err)
		}
		if _, err := store.Fail("7", "7", "the leaf broke on its own"); err != nil {
			t.Fatalf("fail joined task: %v", err)
		}
		run.joined = []uint64{7}
		// THE ROW IS OUTSIDE THE CUT SET: the ending did not take it down, so
		// its fault is the work's and keeps the reading it always drew.
		agent.settleBeltRun(run, RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitTime, Cut: []string{"other"}}, RunLanding{})
		rows := agent.graph().runRows(7)
		if len(rows) != 1 {
			t.Fatalf("the joined row published %d times, want once", len(rows))
		}
		status := ProjectTask(rows[0].StatusFacts())
		if !status.Fault {
			t.Fatal("a joined row that failed on its own is not drawn as a fault")
		}
		if status.Reason != "a fault: the leaf broke on its own" {
			t.Fatalf("reason = %q, want the fault and the row's first line", status.Reason)
		}
	})
}
