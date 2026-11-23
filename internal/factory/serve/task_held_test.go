package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// heldTaskJob is a landed job with an open pull request and a task kept for
// its next Follow PR round, whose person has a reply waiting for its manager.
func heldTaskJob() factory.Item {
	return factory.Item{Title: "keeps a task", Repo: "acme/api", Kind: factory.KindIssue, Num: 7, State: factory.StateLanded, Cap: 10, Assignee: "sam",
		Publish: "pr", HeadCommit: "abc",
		PullRequest: &factory.PullRequest{Number: 9, State: factory.PROpen, HeadSHA: "abc", ChecksRead: true, ReviewsRead: true},
		FollowUp:    &factory.PRFollowUp{Kind: "pr", State: "waiting_approval", Limit: factory.DefaultPRRepairAttempts, Next: factory.FollowTaskRequest(factory.TaskByYou, "Add the note.")},
		Talk:        "talk-1", Thread: []factory.Message{{ID: "m1", From: "you", Text: "what changed?", State: "pending"}},
		Proof:  []factory.Claim{{Text: "works", OK: true, Evidence: "ran it"}},
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1}, {Name: factory.FollowPRStep, Kind: factory.StageChat, State: factory.PhaseWaiting}}}}
}

// A LEASE THAT ONLY LENDS THE MANAGER TO A REPLY RUNS NO STEP, so it neither
// blocks a task kept for the next round from starting nor counts as a runner
// holding the job: during a manager reply the held task starts, and Auto does
// not approve and push the repair the last round landed ahead of it. A lease
// that does run a step still holds the job back.
func TestAThreadOnlyLeaseDoesNotHoldBackATaskKeptForTheNextRound(t *testing.T) {
	f := newWatchFloor(t, false)
	it := f.create(heldTaskJob())
	lent, err := f.h.workers.desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil || lent.Job != it.ID || !lent.ThreadOnly {
		t.Fatalf("the job's pending reply did not lend a thread-only lease: %+v (%v)", lent, err)
	}
	leases, err := f.h.leases()
	if err != nil {
		t.Fatal(err)
	}
	f.h.prepareHeldTasks([]factory.Item{f.job(it.ID)}, leases)
	got := f.job(it.ID)
	if got.FollowUp.Next != "" || got.FollowUp.State != "queued" || got.State != factory.StateQueued {
		t.Fatalf("the kept task did not start under a thread-only lease: state %s, follow %+v", got.State, got.FollowUp)
	}
	if _, err := f.h.workers.desk.Renew(lent.ID, "w1"); err != nil {
		t.Fatalf("starting the task took the reply's lease: %v", err)
	}

	// A hold that runs a step is a runner holding the job: the task waits.
	other := f.create(heldTaskJob())
	f.h.prepareHeldTasks([]factory.Item{f.job(other.ID)}, []lease.Lease{{Job: other.ID}})
	if kept := f.job(other.ID); kept.FollowUp.Next == "" || kept.State != factory.StateLanded {
		t.Fatalf("a task started under a runner's hold: state %s, follow %+v", kept.State, kept.FollowUp)
	}
}
