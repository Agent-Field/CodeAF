package factory

import (
	"strings"
	"testing"
)

// The manager's status reads the job's own acts record: an act that waits is
// said with the text that will be posted, and a failed one with why.
func TestTheManagersStatusReadsTheJobsPublicActs(t *testing.T) {
	it := Item{Acts: []PublicAct{
		{ID: "a1", Kind: ActKindReply, Thread: "inline:501", Text: "Fixed in the next commit.", State: ActProposed},
		{ID: "a2", Kind: ActKindComment, Text: "Rebased on dev.", State: ActFailed, Error: "GitHub did not answer."},
	}}
	lines := strings.Join(ManagerActsWords(it), "\n")
	for _, want := range []string{"approve 1 public act?", "reply in inline:501: Fixed in the next commit.", "comment on the pull request: Rebased on dev.: failed (GitHub did not answer.)"} {
		if !strings.Contains(lines, want) {
			t.Errorf("the status does not say %q:\n%s", want, lines)
		}
	}
	if !managerActsWaiting(it) {
		t.Fatal("an act that waits for the person is not read as waiting")
	}
}

// The thread list names who wrote each open thread and marks a commenter with
// no write access, from the feedback record itself.
func TestTheManagersThreadListSaysWhoWroteEachThread(t *testing.T) {
	it := Item{PullRequest: &PullRequest{Number: 9, State: PROpen,
		Feedback: "Review comment 501 (thread inline:501) by @bob https://example.test/c/501 sum.go:42 (2026-11-17): Skip voided rows.\n",
		FeedbackItems: []PRFeedback{
			{ID: "inline:501", By: "bob", Untrusted: true},
			{ID: "inline:502", By: "kim", Resolved: true},
		}}}
	lines, more := ManagerThreads(it)
	if more != 0 || len(lines) != 1 {
		t.Fatalf("threads = %q, %d more; want the one open thread", lines, more)
	}
	for _, want := range []string{"inline:501", "by @bob (untrusted: no write access)", "Skip voided rows."} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the thread line does not say %q: %s", want, lines[0])
		}
	}
}
