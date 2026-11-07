package factory

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// THE MANAGER IS GIVEN WHAT THE JOB PAGE SHOWS. On the live audit the job page
// asked "Merge pull request #161?" while the manager, reading factory_status,
// said nothing was waiting; it said a gate held an Auto run; it counted "Stage
// 6 of 7" where the rail said 4 of 8; and it said it could not read the pull
// request the factory had just opened. Each came from a fact the status did
// not carry, so the status carries them now.
func TestManagerStatusCarriesAutonomyStepCountPullRequestAndThePostPRDecision(t *testing.T) {
	now := time.Now()
	phases := []Phase{{Name: "plan", State: PhaseDone}, {Name: "approve", State: PhaseDone}, {Name: "write", State: PhaseDone},
		{Name: "test", State: PhaseDone}, {Name: "review", State: PhaseSkipped}, {Name: "security", State: PhaseDone}, {Name: "proof", State: PhaseDone}}
	it := Item{
		ID: 7, Kind: KindIssue, State: StateShipped, Publish: "pr", Autonomy: AutonomyAuto, HeadCommit: "abc123",
		Stream: &Stream{Cur: 6, Phases: phases},
		Change: &Change{Stat: &ChangeStat{Files: 3, Added: 83, Removed: 1}},
		PullRequest: &PullRequest{Number: 161, URL: "https://github.com/o/r/pull/161", State: PROpen, Draft: true, HeadSHA: "abc123",
			Mergeable: MergeableYes, ChecksRead: true, NoChecks: true, ReviewsRead: true},
		FollowUp: &PRFollowUp{State: "healthy", Reason: "PR #161 is healthy."},
	}
	got := StatusForManager(it, false, now).Text
	done, total := StepCount(it)
	for _, want := range []string{
		"Autonomy: Auto",
		"Manager actions: This job is in Auto: never say a gate holds",
		"Steps: " + strconv.Itoa(done) + " of " + strconv.Itoa(total) + " done",
		"Pull request #161 (draft): https://github.com/o/r/pull/161",
		"3 files, +83 −1",
		"no GitHub checks are configured",
		"Waiting on the person: Mark pull request #161 ready for review?",
		"action=mark_ready",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}

	// Ready for review, with nothing standing in the gate's way: Merge is offered.
	it.PullRequest.Draft = false
	got = StatusForManager(it, false, now).Text
	if !strings.Contains(got, "Waiting on the person: Merge pull request #161?") || !strings.Contains(got, "action=merge") {
		t.Fatalf("an open, mergeable pull request offers no merge:\n%s", got)
	}

	// A step in Manual: the gate waits for the person, and a stopped step is named stopped.
	it = Item{ID: 8, Kind: KindIssue, State: StateNew, Autonomy: AutonomyManual, Failure: &Failure{Code: CodeStopped, Detail: "Stopped by you"},
		Stream: &Stream{Cur: 1, Phases: []Phase{{Name: "plan", State: PhaseDone}, {Name: "write", State: PhaseFailed}}}}
	got = StatusForManager(it, false, now).Text
	if !strings.Contains(got, "Autonomy: Manual") || !strings.Contains(got, "Current step: write, stopped") || strings.Contains(got, "Waiting on the person: Merge") {
		t.Fatalf("manual job status:\n%s", got)
	}
}
