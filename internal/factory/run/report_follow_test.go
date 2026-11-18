package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// followRound is a job whose runner has just begun a Follow PR round: queued
// on the service, with the pull request and the follow-up on head.
func followRound(head string) factory.Item {
	return factory.Item{
		ID: 1, Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateQueued, Cap: 10, Publish: "pr", HeadCommit: head,
		PullRequest: &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, HeadSHA: head},
		FollowUp:    &factory.PRFollowUp{Kind: "pr", State: "queued", HeadSHA: head, Attempt: 1, Limit: 3},
		Stages:      []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone},
			{Name: factory.FollowPRStep, Kind: factory.StageChat, State: factory.PhasePending, Round: 1},
		}},
	}
}

// A pull request already marked ready keeps that record on its follow-up, and
// every later round reports the follow-up whole. Saying "ready" again is not
// a move to ready: only the move needs the publication behind it.
func TestAReportMayRestateAPullRequestAlreadyReady(t *testing.T) {
	const head = "1111111111111111111111111111111111111111"
	ready := &factory.PRReadiness{Key: "snapshot", Head: head, State: "ready"}
	it := followRound(head)
	it.FollowUp.Readiness = ready
	began := *it.FollowUp
	began.State = "running"
	if err := ApplyReport(&it, factory.RunReport{FollowUp: &began}, time.Now()); err != nil {
		t.Fatalf("a round after the pull request went ready was refused: %v", err)
	}
	if it.FollowUp.State != "running" || it.FollowUp.Readiness.State != "ready" {
		t.Fatalf("follow-up after the report: %+v", it.FollowUp)
	}
	// The move itself still needs the recorded publication.
	it = followRound(head)
	it.FollowUp.Kind = "readiness"
	it.FollowUp.Readiness = &factory.PRReadiness{Key: "snapshot", Head: head, State: "reviewed"}
	moved := *it.FollowUp
	moved.Readiness = ready
	err := ApplyReport(&it, factory.RunReport{FollowUp: &moved}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "ready requires the recorded publication transition") {
		t.Fatalf("a move to ready with no publication was taken: %v", err)
	}
}

// The poller may write a pushed head on the job before a rerun's runner reads
// the forge. The runner then reports its follow-up moved to that head and no
// pull request, because its copy of the pull request did not change; the head
// the service already holds is the observation.
func TestARoundMayMoveToAHeadTheServiceAlreadyRead(t *testing.T) {
	const old, pushed = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	it := followRound(old)
	it.PullRequest.HeadSHA = pushed
	moved := *it.FollowUp
	moved.State, moved.HeadSHA, moved.ObservedFrom = "running", pushed, old
	if err := ApplyReport(&it, factory.RunReport{FollowUp: &moved}, time.Now()); err != nil {
		t.Fatalf("the moved follow-up was refused: %v", err)
	}
	if it.FollowUp.HeadSHA != pushed || it.FollowUp.ObservedFrom != old {
		t.Fatalf("follow-up after the report: %+v", it.FollowUp)
	}
	// A head the service has not read is still refused.
	it = followRound(old)
	elsewhere := *it.FollowUp
	elsewhere.State, elsewhere.HeadSHA, elsewhere.ObservedFrom = "running", pushed, old
	if err := ApplyReport(&it, factory.RunReport{FollowUp: &elsewhere}, time.Now()); err == nil {
		t.Fatal("a follow-up moved to a head nobody observed was taken")
	}
}

// A round whose first report the service refused did run, though its step
// still reads pending on the service. Its rerun is taken; a step that never
// ran is still refused.
func TestARerunIsTakenOverResultsTheServiceRefused(t *testing.T) {
	const head = "1111111111111111111111111111111111111111"
	it := followRound(head)
	it.State, it.QKind = factory.StateNeedsYou, "results_not_saved"
	it.Question = "The factory refused this step's results (HTTP 422): follow-up identity changed."
	if err := StepControl(&it, factory.FollowPRStep, true, "", time.Now()); err != nil {
		t.Fatalf("the rerun was refused: %v", err)
	}
	if it.State != factory.StateQueued || it.Question != "" || it.QKind != "" || it.FollowUp.State != "queued" {
		t.Fatalf("the rerun did not queue the step: state %s, question %q, follow %s", it.State, it.Question, it.FollowUp.State)
	}
	never := followRound(head)
	never.State = factory.StateNeedsYou
	if err := StepControl(&never, factory.FollowPRStep, true, "", time.Now()); err == nil || !strings.Contains(err.Error(), "has not run yet") {
		t.Fatalf("a rerun of a step that never ran was taken: %v", err)
	}
}
