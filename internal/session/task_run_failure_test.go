package session

import (
	"strings"
	"testing"
	"time"
)

func TestDailyLimitRefusalReachesTheTaskReportAndEnding(t *testing.T) {
	agent, store, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
	refusal := "daily limit reached · $1.50 spent of $1 · /budget day changes it"
	for _, result := range []string{"", "partial work"} {
		summary := RunSummary{Outcome: "ran and did not finish", Result: result, Failure: refusal}
		landing := RunLanding{Refused: "nothing to land: the run's working copy holds no change"}
		notice := agent.beltRunNotice(run, summary, landing)
		if notice.State != TaskFailed || !strings.HasPrefix(notice.Report, refusal) {
			t.Errorf("task report hides the daily refusal: %q", notice.Report)
		}
		status := ProjectTask(notice.StatusFacts())
		if status.Presence != TaskPresenceIncomplete || !strings.Contains(status.Reason, refusal) {
			t.Errorf("ending card hides the daily refusal: %+v", status)
		}
		if note := beltRunOutcomeNote(store, run.root, summary, landing, time.Second); !strings.Contains(note, refusal) {
			t.Errorf("saved ending hides the daily refusal: %q", note)
		}
		if result != "" && !strings.Contains(notice.Report, result) {
			t.Errorf("failure dropped the partial result: %q", notice.Report)
		}
	}
}
