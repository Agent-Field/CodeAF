package serve_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// Structurally matches the legacy retained no-change report: Follow PR round 37
// reset its attempt from 1 to 0 while the service still held the running phase.
// All identities, paths and evidence are local fixture data.
func retainedNoChangeSetup(t *testing.T) (*workerFixture, factory.Item, api.WorkerAttached, api.WorkerPull, api.WorkerRecovery) {
	t.Helper()
	f := workerSetupAs(t, "sam")
	it := f.add(t, "sam")
	head := strings.Repeat("3", 40)
	started := now.Add(-time.Minute)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.Kind, it.Num, it.Autonomy = factory.KindIssue, 9, factory.AutonomyAuto
		it.Title, it.Publish, it.HeadCommit, it.Cap = "Retain a completed review", "pr", head, 10
		it.Stream = &factory.Stream{Started: now.Add(-time.Hour), Cur: 6, Spent: .5}
		it.Stages = nil
		for _, name := range []string{"plan", "approve", "write", "test", "review", "proof", factory.FollowPRStep} {
			kind, round := factory.StageChat, 1
			if name == "approve" {
				kind, round = factory.StageGate, 0
			}
			it.Stages = append(it.Stages, factory.Stage{Name: name, Kind: kind, On: true, Until: factory.UntilDone, Max: 2})
			it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: name, Kind: kind, State: factory.PhaseDone, Round: round})
		}
		it.Stream.Phases[6] = factory.Phase{Name: factory.FollowPRStep, Kind: factory.StageChat, State: factory.PhaseRunning, Round: 37, Started: started, Chat: "fixture-follow-chat"}
		it.PullRequest = &factory.PullRequest{Number: 11, URL: "https://github.com/owner/repo/pull/11", State: factory.PROpen, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, Checks: factory.PRChecks{Passed: 1}, CI: "ci ✓", FeedbackTracked: true, FeedbackItems: []factory.PRFeedback{{ID: "inline:1", Revision: "v1"}}}
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "running", HeadSHA: head, Attempt: 1, Limit: 3, EventKey: "captured-event", ReviewKey: "captured-reviews", Inspecting: map[string]string{"inline:1": "v1"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := f.attach(t, "sam")
	pulled := f.pull(t, w)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State, it.QKind, it.Question = factory.StateNeedsYou, "results_not_saved", "The factory refused this step's results (HTTP 422): follow-up identity changed"
		it.Asking = &factory.Asked{Question: it.Question, With: factory.AskedYou}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report := api.WorkerReport{Cost: .25,
		FollowUp: &factory.PRFollowUp{Kind: "pr", State: "watching", HeadSHA: head, Attempt: 0, Limit: 3, EventKey: "captured-event", ReviewKey: "captured-reviews", Reason: "No code change needed; watching review feedback.", Reviewed: map[string]string{"inline:1": "v1"}},
		Steps:    []api.StepReport{{Name: factory.FollowPRStep, State: api.StepWaiting, Round: 37, RoundLimit: 2, StartedAt: &started, Chat: "fixture-follow-chat", Model: "fixture/main", Result: &api.StepResult{Done: true, Output: "No code change needed: the current head already addresses this comment.", Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Inspected the current implementation and regression evidence", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}}},
	}
	prepared, err := client.PrepareWorkerReport(report, "retained-completed-round")
	if err != nil {
		t.Fatal(err)
	}
	return f, it, w, pulled, api.WorkerRecovery{OriginalLease: pulled.Lease.ID, Body: prepared.Body, Headers: prepared.Headers, AdditionalCost: .125}
}

func TestRetainedNoChangeReportRecoversExistingJobAndAcknowledgesCostOnce(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "original lease live", true: "original lease expired"}[expired], func(t *testing.T) {
			f, it, worker, _, body := retainedNoChangeSetup(t)
			if expired {
				f.clock.Add(int64(2 * time.Minute))
			}
			path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
			var first api.WorkerRecovered
			f.request(t, "POST", path, worker.Credential, body, 200, &first)
			if first.AlreadyApplied {
				t.Fatal("the first retained report was mistaken for a receipt replay")
			}
			saved, err := f.st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			ph := saved.Stream.Phases[6]
			if saved.State != factory.StateShipped || saved.Question != "" || saved.QKind != "" || saved.Asking != nil || saved.FollowUp.State != "watching" || saved.FollowUp.Attempt != 0 || ph.State != factory.PhaseWaiting || ph.Round != 37 || ph.Result == nil || ph.Result.Repair.Decision != "no_change" || saved.Stream.Spent != .875 || saved.FollowUp.Reviewed["inline:1"] != "v1" || len(saved.ReportReceipts) != 1 {
				t.Fatalf("completed work did not become a saved monitoring baseline: state=%s question=%q follow=%+v phase=%+v spent=%g receipts=%d", saved.State, saved.Question, saved.FollowUp, ph, saved.Stream.Spent, len(saved.ReportReceipts))
			}
			desk, err := lease.Open(f.st, lease.Options{Now: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }})
			if err != nil {
				t.Fatal(err)
			}
			live, err := desk.Live()
			if err != nil || len(live) != 0 {
				t.Fatalf("completed result left its old worker lease alive: %+v %v", live, err)
			}
			revision, _ := f.st.Revision(it.ID)
			var replay api.WorkerRecovered
			f.request(t, "POST", path, worker.Credential, body, 200, &replay)
			afterRevision, _ := f.st.Revision(it.ID)
			if !replay.AlreadyApplied || afterRevision != revision || replay.Job.Cost != .875 {
				t.Fatalf("receipt replay mutated or recharged the job: replay=%v rev=%d/%d cost=%g", replay.AlreadyApplied, revision, afterRevision, replay.Job.Cost)
			}
			// Usage recorded during runner cancellation is cumulative and carries
			// no execution-state authority. Its increase also charges exactly once.
			body.AdditionalCost = .25
			f.request(t, "POST", path, worker.Credential, body, 200, &replay)
			f.request(t, "POST", path, worker.Credential, body, 200, &replay)
			if replay.Job.Cost != 1 || !replay.AlreadyApplied {
				t.Fatalf("late cumulative usage was lost or charged twice: %+v", replay)
			}
		})
	}
}

func TestRetainedResultRecoveryCannotBypassNewerJobFacts(t *testing.T) {
	for _, mode := range []string{"new question", "manual takeover", "paused", "stopped", "closed PR", "new head", "new round", "new start", "new chat", "other machine", "other owner", "replacement lease"} {
		t.Run(mode, func(t *testing.T) {
			f, it, worker, _, body := retainedNoChangeSetup(t)
			want := 422
			if mode == "replacement lease" {
				desk, err := lease.Open(f.st, lease.Options{Now: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }})
				if err != nil {
					t.Fatal(err)
				}
				if err := desk.Requeue(it.ID, worker.WorkerID); err != nil {
					t.Fatal(err)
				}
				if err := f.st.Update(it.ID, func(it *factory.Item) error { it.State = factory.StateQueued; return nil }); err != nil {
					t.Fatal(err)
				}
				f.pull(t, worker)
				want = 409
			}
			if err := f.st.Update(it.ID, func(it *factory.Item) error {
				it.State = factory.StateNeedsYou
				switch mode {
				case "new question":
					it.QKind, it.Question = "scope", "Choose the desired behavior"
				case "manual takeover":
					it.Manual = true
				case "paused":
					it.Stream.Paused = true
				case "stopped":
					it.State = factory.StateNew
				case "closed PR":
					it.PullRequest.State = factory.PRClosed
				case "new head":
					it.PullRequest.HeadSHA, it.FollowUp.HeadSHA = strings.Repeat("4", 40), strings.Repeat("4", 40)
				case "new round":
					it.Stream.Phases[6].Round++
				case "new start":
					it.Stream.Phases[6].Started = it.Stream.Phases[6].Started.Add(time.Second)
				case "new chat":
					it.Stream.Phases[6].Chat = "a-new-conversation"
				case "other machine":
					it.WorktreeMachine = "another-machine"
				case "other owner":
					it.Assignee = "another-owner"
					want = 409
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			revision, _ := f.st.Revision(it.ID)
			f.request(t, "POST", fmt.Sprintf("/v1/workers/recover/%d", it.ID), worker.Credential, body, want, nil)
			after, err := f.st.Get(it.ID)
			afterRevision, _ := f.st.Revision(it.ID)
			if err != nil || len(after.ReportReceipts) != 0 || after.Stream.Spent != .5 || afterRevision != revision {
				t.Fatalf("refused reconciliation mutated the job: receipts=%d cost=%g revision=%d/%d err=%v", len(after.ReportReceipts), after.Stream.Spent, revision, afterRevision, err)
			}
		})
	}
}

func TestAcknowledgedRetainedReportCannotReplaceOrReleaseALaterAttempt(t *testing.T) {
	f, it, worker, _, body := retainedNoChangeSetup(t)
	path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
	var out api.WorkerRecovered
	f.request(t, "POST", path, worker.Credential, body, 200, &out)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateQueued
		it.FollowUp.State, it.FollowUp.Attempt = "queued", 1
		it.Stream.Phases[6].State = factory.PhasePending
		it.Stream.Phases[6].Round = 38
		it.Stream.Phases[6].Started = now.Add(time.Second)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	current := f.pull(t, worker)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.Question, it.QKind = "Choose the API behavior", "scope"
		it.State = factory.StateNeedsYou
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.request(t, "POST", path, worker.Credential, body, 200, &out)
	body.AdditionalCost = .25
	f.request(t, "POST", path, worker.Credential, body, 200, &out)
	saved, _ := f.st.Get(it.ID)
	if !out.AlreadyApplied || saved.Question != "Choose the API behavior" || saved.QKind != "scope" || saved.State != factory.StateNeedsYou || saved.Stream.Phases[6].Round != 38 || saved.Stream.Spent != 1 {
		t.Fatalf("acknowledged old work overwrote a new attempt: state=%s question=%q round=%d cost=%g", saved.State, saved.Question, saved.Stream.Phases[6].Round, saved.Stream.Spent)
	}
	desk, err := lease.Open(f.st, lease.Options{Now: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	live, err := desk.Live()
	if err != nil || len(live) != 1 || live[0].ID != current.Lease.ID {
		t.Fatalf("receipt replay released the new worker: %+v %v", live, err)
	}
}

func TestWorkerRecoveryRetryStatusIsPrivateAndReadOnly(t *testing.T) {
	f, it, worker, _, _ := retainedNoChangeSetup(t)
	retryAt := now.Add(time.Minute)
	if err := f.st.Update(it.ID, func(it *factory.Item) error { it.ReportRetryAt = retryAt; return nil }); err != nil {
		t.Fatal(err)
	}
	revision, _ := f.st.Revision(it.ID)
	path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
	var status api.WorkerRecovered
	f.request(t, "GET", path, worker.Credential, nil, 200, &status)
	if !status.RetryRequestedAt.Equal(retryAt) || status.Job.ID != 0 {
		t.Fatalf("retry status lost its grant or leaked the job: %+v", status)
	}
	f.request(t, "GET", path, "", nil, 401, nil)
	var other api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Name: "other laptop", Handle: "sam", Machine: "another-machine"}, 201, &other)
	f.request(t, "GET", path, other.Credential, nil, 403, nil)
	after, _ := f.st.Revision(it.ID)
	if after != revision {
		t.Fatalf("reading retry metadata mutated the job: %d/%d", revision, after)
	}
}
