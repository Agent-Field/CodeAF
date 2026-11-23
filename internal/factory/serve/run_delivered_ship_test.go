package serve_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// The tenth live A+B run shipped a job whose only step ran on AgentField and
// whose pull request the run opened, and its Merge card said "no approved
// commit is recorded for this job" for good. The route started it with
// "Asks first", so the person's yes was written to the record as an approval;
// the worker's done then read that start approval as consent to publish and
// shipped the job down the native publish path, which gave it the factory's
// own Follow PR step. With that step on it, the job no longer read as a
// pipeline that runs only on AgentField, so the follow never recorded the
// head the run delivered, and the merge gate had no commit to hold the merge
// to. A route that started the job from the backlog wrote no approval and
// shipped correctly, which is why the sixth run's fix held there.
//
// A JOB WHOSE WORK RAN ELSEWHERE ENDS AS ITS RUN DOES, whatever approval the
// record holds: no Follow PR step, no CI follow-up this machine has no
// checkout to carry out, and Merge once the pull request is read.
func TestAnAgentFieldJobStartedOnAYesShipsAsItsRunAndCanMerge(t *testing.T) {
	f, rec, _, _, _ := publishingSetup(t)
	it, err := f.st.Create(factory.Item{
		Repo: "owner/repo", Num: 324, Kind: factory.KindIssue, Title: "Add a method", State: factory.StateQueued, Assignee: "sam",
		Stages:          []factory.Stage{{Name: "build", Label: "Build on AgentField", Kind: factory.StageAgentField, Target: "planner.build_issue", On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {Target: "planner.build_issue", Status: "succeeded", Ended: now}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The yes to "Start this job?" is an approval with no commit, exactly as
	// the start door writes one.
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "sam"}); err != nil {
		t.Fatal(err)
	}
	worker := f.attach(t, "sam")
	pulled := f.pull(t, worker)
	if pulled.Job.ID != it.ID {
		t.Fatalf("leased job %d, want %d", pulled.Job.ID, it.ID)
	}
	lease := "/v1/workers/leases/" + pulled.Lease.ID
	pr := &api.PR{Number: 325, URL: "https://github.com/owner/repo/pull/325"}
	f.request(t, "POST", lease+"/report", worker.Credential, api.WorkerReport{PR: pr, Publish: "pr"}, 200, nil)
	f.request(t, "POST", lease+"/done", worker.Credential, api.WorkerDone{State: api.StateDone}, 200, nil)

	got, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != factory.StateShipped {
		t.Fatalf("state %q, want shipped", got.State)
	}
	for _, st := range got.Stages {
		if factory.IsCIRepairStage(st) {
			t.Fatalf("a run-delivered job was given the factory's %q step: %+v", st.Name, got.Stages)
		}
	}
	if got.FollowUp != nil {
		t.Fatalf("a run-delivered job began a CI follow-up this machine has no checkout for: %+v", got.FollowUp)
	}
	if !factory.RunDeliveredPR(got) {
		t.Fatal("the job no longer reads as one whose pull request a run delivered")
	}
	// The follow's first read after the run ended records the head, and the
	// card offers Merge.
	got.PullRequest.State, got.PullRequest.HeadSHA, got.PullRequest.Mergeable, got.PullRequest.ChecksRead, got.PullRequest.ReviewsRead = factory.PROpen, "9a82256", "yes", true, true
	if !factory.PinDeliveredHead(&got, got.PullRequest.HeadSHA) {
		t.Fatal("the delivered head was not recorded")
	}
	if v := factory.MergeOffered(got); !v.OK {
		t.Fatalf("merge refused: %s", v.Why)
	}
}

// A job already shipped the wrong way before this fix carries the Follow PR
// step and a watching follow-up. The factory's own maintenance step is not a
// step of the job's pipeline, so the pull request still reads as the run's,
// and the next read records its head instead of refusing the merge forever.
func TestARunDeliveredJobThatGainedFollowPRStillRecordsItsHead(t *testing.T) {
	it := factory.Item{
		Repo: "owner/repo", Num: 324, Kind: factory.KindIssue, State: factory.StateShipped, Publish: "pr",
		Stages: []factory.Stage{
			{Name: "build", Kind: factory.StageAgentField, Target: "planner.build_issue", On: true},
			{Name: factory.FollowPRStep, Kind: factory.StageChat, On: true, Fixed: true},
		},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {Target: "planner.build_issue", Status: "succeeded", Ended: time.Now()}},
		FollowUp:        &factory.PRFollowUp{Kind: "pr", State: "watching"},
		PullRequest:     &factory.PullRequest{Number: 325, URL: "https://github.com/owner/repo/pull/325", State: factory.PROpen, HeadSHA: "9a82256", Mergeable: "yes", ChecksRead: true, ReviewsRead: true},
	}
	if !factory.PinDeliveredHead(&it, it.PullRequest.HeadSHA) {
		t.Fatal("the delivered head was not recorded for a job that gained the Follow PR step")
	}
	if v := factory.MergeOffered(it); !v.OK {
		t.Fatalf("merge refused: %s", v.Why)
	}
	// It stays a job that ran here for every other purpose: the maintenance
	// step is a step this machine runs.
	if factory.RunsElsewhere(it) {
		t.Fatal("a job with the Follow PR step on it now reads as running elsewhere")
	}
}
