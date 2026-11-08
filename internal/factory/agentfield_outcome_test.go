package factory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// THE CONTROL PLANE'S OUTCOME READS IN A PERSON'S WORDS (live e2e round 7):
// the raw report in a closing parenthesis is left off, a snake_case code reads
// as words, and no machinery word survives.
func TestAnAgentFieldOutcomeReadsInPlainWords(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"No pull request: the build did not pass verification (Partial: 1/1 issues completed, verification: OVERALL: FAIL. …)", "No pull request: the build did not pass its checks"},
		{"approval_rejected: out of scope", "approval rejected: out of scope"},
		{"Verified on the node, verdict pending.", "Checked on the node, result pending"},
		{"Plan rejected: not now", "Plan rejected: not now"},
		{"  ", ""},
	} {
		if got := AgentFieldOutcomeWords(tc.in); got != tc.want {
			t.Errorf("AgentFieldOutcomeWords(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := AgentFieldDidNotSucceed(Stage{Name: "build"}, ""); got != "Build did not succeed" {
		t.Errorf("unlabelled step = %q", got)
	}
	if got := AgentFieldDidNotSucceed(Stage{Name: "build", Label: "Build on AgentField"}, "No pull request"); got != "Build on AgentField did not succeed: No pull request" {
		t.Errorf("labelled step = %q", got)
	}
	if got := FirstSentence("Partial: 1/1 done. The rest failed."); got != "Partial: 1/1 done." {
		t.Errorf("FirstSentence = %q", got)
	}
}

// A RUN'S OUTCOME IS READ FROM THE SHAPE OF ITS WORDS: a graded report gives
// its reason with the grade and any quoted command left off, a "No pull
// request:" note reads as that consequence of the reason, and what a command
// printed never reaches the line. No pipeline or step name is read.
func TestAnAgentFieldOutcomeIsReadFromItsWordsShape(t *testing.T) {
	build := "OVERALL: FAIL. The project does not build or its tests fail: `go build ./...` exited 1. ./ledger.go:12:2: no required module provides package github.com/x/hashkit; to add it:\n\tgo get github.com/x/hashkit"
	partial := "Partial: 1/1 issues completed, verification: OVERALL: FAIL. The delivered implementation does not satisfy the PRD."
	for _, tc := range []struct{ name, note, summary, want string }{
		{"a project that does not build, with its note", "No pull request: the build did not pass verification (" + build + ")", build,
			"The project does not build or its tests fail, so no pull request was opened"},
		{"a project that does not build, no note", "", build, "The project does not build or its tests fail"},
		{"an ungraded report under a no-pull-request note", "No pull request: the build did not pass verification (" + partial + ")", partial,
			"The build did not pass its checks, so no pull request was opened"},
		{"a note of another shape", "Stopped early: the node went away", partial, "Stopped early: the node went away"},
		{"a plain summary", "", "Plan rejected: not now", "Plan rejected: not now"},
		{"nothing said", "", "", ""},
	} {
		got := AgentFieldOutcome(tc.note, tc.summary)
		if got != tc.want {
			t.Errorf("%s: AgentFieldOutcome = %q, want %q", tc.name, got, tc.want)
		}
		if strings.Contains(got, "`") || strings.Contains(got, "exited") || strings.Contains(got, "OVERALL") || len([]rune(got)) > FailureSummaryMost {
			t.Errorf("%s: machinery or command output reached the line: %q", tc.name, got)
		}
	}
}

// A FAILED JOB'S FILE SAYS FAILED (live e2e rounds 4 to 7, jobs 74 and 85
// read "new"), and reading it back is the job it was: not running, its
// failure on it. A job a person stopped, or one that never failed, keeps
// "new".
func TestAFailedJobsFileSaysFailedAndReadsBackAsItWas(t *testing.T) {
	at := time.Date(2026, 11, 8, 8, 58, 13, 0, time.UTC)
	failed := Item{ID: 85, Title: "Add a Fingerprint method", State: StateNew,
		Failure: &Failure{Step: "build", Code: CodeToolError, Detail: "Build did not succeed", Next: FailureStopped, At: at}}
	raw, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"State":"failed"`) {
		t.Fatalf("a failed job's file reads %s", raw)
	}
	var back Item
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.State != StateNew || back.Failure == nil || back.Failure.Detail != "Build did not succeed" {
		t.Fatalf("read back as %q %+v", back.State, back.Failure)
	}
	stopped := failed
	stopped.Failure = &Failure{Step: "build", Code: CodeStopped, Detail: "Stopped by you", At: at}
	fresh := Item{ID: 86, State: StateNew}
	for name, it := range map[string]Item{"stopped": stopped, "never ran": fresh} {
		raw, _ := json.Marshal(it)
		if !strings.Contains(string(raw), `"State":"new"`) {
			t.Errorf("%s job's file reads %s", name, raw)
		}
	}
}
