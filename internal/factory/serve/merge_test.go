package serve

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

var mergeTestHead = strings.Repeat("1", 40)

// mergeFixture is a solo factory with one job whose pull request every gate
// rule lets through, and the person signed in as owner.
func mergeFixture(t *testing.T, policy *factory.MergePolicy) (*peopleV2Fixture, factory.Item) {
	t.Helper()
	f := peopleV2Setup(t, api.FactorySolo)
	f.seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	it := f.add(t, "owner")
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State, it.HeadCommit, it.Publish = factory.StateShipped, mergeTestHead, "pr"
		it.WorktreeMachine = "mbp"
		it.Proof = []factory.Claim{{Text: "works", OK: true, Evidence: "test"}}
		it.MergePolicy = policy
		it.PullRequest = &factory.PullRequest{
			Number: 42, URL: "https://github.com/acme/api/pull/42", State: factory.PROpen,
			HeadSHA: mergeTestHead, Mergeable: factory.MergeableYes,
			ChecksRead: true, Checks: factory.PRChecks{Passed: 2}, ReviewsRead: true,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f, got
}

func (f *peopleV2Fixture) kinds(t *testing.T, id int) []string {
	t.Helper()
	events, err := f.cfg.Record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range events {
		if e.Job == id {
			out = append(out, e.Kind)
		}
	}
	return out
}

// A pressed Merge waits five seconds with Undo, settles through the gate,
// rides the lease to the runner, and the runner's report finishes the job.
func TestMergePressWaitsForUndoThenRidesTheLeaseToTheRunner(t *testing.T) {
	f, it := mergeFixture(t, nil)
	path := fmt.Sprintf("/v1/jobs/%d", it.ID)
	var pressed struct {
		Merge     *api.JobMerge `json:"merge"`
		UndoUntil time.Time     `json:"undo_until"`
	}
	f.request(t, "POST", path+"/merge", "operator-secret", map[string]string{"commit": mergeTestHead}, 200, &pressed)
	if pressed.Merge == nil || pressed.Merge.Run == nil || pressed.Merge.Run.State != factory.MergeAsked || !pressed.UndoUntil.Equal(f.now.Add(factory.MergeUndo)) {
		t.Fatalf("pressed = %+v", pressed)
	}
	// Inside the window nothing is sent.
	f.request(t, "GET", path, "operator-secret", nil, 200, nil)
	if saved, _ := f.st.Get(it.ID); saved.Merging == nil || saved.Merging.State != factory.MergeAsked || saved.State != factory.StateShipped {
		t.Fatalf("inside the Undo window: %+v", saved.Merging)
	}
	f.now = f.now.Add(factory.MergeUndo)
	var job api.Job
	f.request(t, "GET", path, "operator-secret", nil, 200, &job)
	if job.Merge == nil || job.Merge.Run == nil || job.Merge.Run.State != factory.MergeQueued || job.Merge.Run.Commit != mergeTestHead || job.State != api.StateQueued {
		t.Fatalf("settled job = state %s merge %+v", job.State, job.Merge)
	}

	var worker api.RunnerAttached
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "owner"}, 201, &worker)
	var pull api.RunnerPull
	f.request(t, "GET", "/v1/runners/pull", worker.Credential, nil, 200, &pull)
	if len(pull.Signals) != 1 || pull.Signals[0].Verb != lease.SignalMerge || pull.Signals[0].Words != mergeTestHead {
		t.Fatalf("merge signal = %+v", pull.Signals)
	}
	if pull.Job.PR == nil || pull.Job.PR.HeadSHA != mergeTestHead || !pull.Job.PR.ChecksRead || pull.Job.Merge == nil || pull.Job.Merge.Run.By != "owner" {
		t.Fatalf("pulled job lacks the gate's facts: pr %+v merge %+v", pull.Job.PR, pull.Job.Merge)
	}
	merged := *api.FromItem(it).PR
	merged.State = factory.PRMerged
	f.request(t, "POST", "/v1/runners/leases/"+pull.Lease.ID+"/report", worker.Credential, api.RunnerReport{Merge: &api.WorkerMerge{State: factory.MergeMerged, SHA: "m3rg3", PR: &merged}}, 200, nil)
	f.request(t, "POST", "/v1/runners/leases/"+pull.Lease.ID+"/done", worker.Credential, api.RunnerDone{State: "done"}, 200, nil)

	job = api.Job{} // a fresh read: decoding into the old one keeps fields it omits
	f.request(t, "GET", path, "operator-secret", nil, 200, &job)
	if job.State != api.StateDone || job.DoneHow != "merged" || job.PR == nil || job.PR.State != factory.PRMerged || (job.Merge != nil && job.Merge.Run != nil) {
		t.Fatalf("merged job = state %s done %s pr %+v merge %+v", job.State, job.DoneHow, job.PR, job.Merge)
	}
	// The done of a merge is not a publish: nothing was posted again.
	got := strings.Join(f.kinds(t, it.ID), " ")
	if got != record.MergeAsked+" "+record.MergeQueued {
		t.Fatalf("record = %s", got)
	}
}

// The gate's refusal comes back in its own words and nothing is stored.
func TestMergeRefusesWhatTheGateRefuses(t *testing.T) {
	f, it := mergeFixture(t, nil)
	if err := f.st.Update(it.ID, func(it *factory.Item) error { it.PullRequest.Checks.Failed = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/jobs/%d/merge", it.ID)
	body := f.request(t, "POST", path, "operator-secret", nil, 409, nil)
	if !strings.Contains(body, "1 check is failing") {
		t.Fatalf("refusal = %s", body)
	}
	if saved, _ := f.st.Get(it.ID); saved.Merging != nil {
		t.Fatalf("a refused merge was stored: %+v", saved.Merging)
	}
	// A head the person was not shown is refused too.
	if err := f.st.Update(it.ID, func(it *factory.Item) error { it.PullRequest.Checks.Failed = 0; return nil }); err != nil {
		t.Fatal(err)
	}
	body = f.request(t, "POST", path, "operator-secret", map[string]string{"commit": strings.Repeat("2", 40)}, 409, nil)
	if !strings.Contains(body, "commits you did not approve") {
		t.Fatalf("stale head refusal = %s", body)
	}
}

func TestUndoWithdrawsAPressedMerge(t *testing.T) {
	f, it := mergeFixture(t, nil)
	path := fmt.Sprintf("/v1/jobs/%d", it.ID)
	f.request(t, "POST", path+"/merge", "operator-secret", nil, 200, nil)
	var pressed api.Job
	f.request(t, "GET", path, "operator-secret", nil, 200, &pressed)
	if !pressed.Can.Undo {
		t.Fatal("can.undo is false inside a pressed merge's window")
	}
	f.request(t, "POST", path+"/undo", "operator-secret", nil, 200, nil)
	f.now = f.now.Add(time.Minute)
	f.request(t, "GET", path, "operator-secret", nil, 200, nil)
	if saved, _ := f.st.Get(it.ID); saved.Merging != nil || saved.State != factory.StateShipped {
		t.Fatalf("undone merge = %+v state %s", saved.Merging, saved.State)
	}
}

// AUTO-MERGE NEVER FIRES WHEN IT IS OFF. The same clean job is swept with no
// policy, with a policy that says off, and with one that says on; only the
// last is sent, citing the pipeline's rule.
func TestAutoMergeSweepSendsOnlyWhatThePipelineTurnedOn(t *testing.T) {
	for name, policy := range map[string]*factory.MergePolicy{
		"no policy": nil,
		"off":       {Auto: false},
		"on":        {Auto: true, Method: factory.MergeMethodSquash},
	} {
		t.Run(name, func(t *testing.T) {
			f, it := mergeFixture(t, policy)
			path := fmt.Sprintf("/v1/jobs/%d", it.ID)
			f.request(t, "GET", path, "operator-secret", nil, 200, nil)
			saved, err := f.st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			on := policy != nil && policy.Auto
			if !on {
				if saved.Merging != nil || len(f.kinds(t, it.ID)) != 0 {
					t.Fatalf("auto-merge off sent a merge: %+v", saved.Merging)
				}
				return
			}
			if m := saved.Merging; m == nil || m.State != factory.MergeQueued || !m.Auto || m.By != "" || m.Method != factory.MergeMethodSquash || m.Rule != "auto-merge is on in this job's pipeline" {
				t.Fatalf("auto merge = %+v", saved.Merging)
			}
		})
	}
}

// An auto-merge whose facts are not there yet waits quietly: nothing is
// refused, and the sweep sends it once they are.
func TestAutoMergeWaitsForChecksQuietly(t *testing.T) {
	f, it := mergeFixture(t, &factory.MergePolicy{Auto: true})
	if err := f.st.Update(it.ID, func(it *factory.Item) error { it.PullRequest.Checks.Pending = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/jobs/%d", it.ID)
	f.request(t, "GET", path, "operator-secret", nil, 200, nil)
	if saved, _ := f.st.Get(it.ID); saved.Merging != nil || len(f.kinds(t, it.ID)) != 0 {
		t.Fatalf("waiting auto-merge = %+v", saved.Merging)
	}
	if err := f.st.Update(it.ID, func(it *factory.Item) error { it.PullRequest.Checks.Pending = 0; return nil }); err != nil {
		t.Fatal(err)
	}
	f.request(t, "GET", path, "operator-secret", nil, 200, nil)
	if saved, _ := f.st.Get(it.ID); saved.Merging == nil || saved.Merging.State != factory.MergeQueued {
		t.Fatalf("ready auto-merge = %+v", saved.Merging)
	}
}

// A merge the runner reports failed is the person's to look at, and the
// sweep does not try it again on its own.
func TestAFailedMergeNeedsYouAndIsNotRetried(t *testing.T) {
	f, it := mergeFixture(t, &factory.MergePolicy{Auto: true})
	path := fmt.Sprintf("/v1/jobs/%d", it.ID)
	f.request(t, "GET", path, "operator-secret", nil, 200, nil)
	var worker api.RunnerAttached
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "owner"}, 201, &worker)
	var pull api.RunnerPull
	f.request(t, "GET", "/v1/runners/pull", worker.Credential, nil, 200, &pull)
	f.request(t, "POST", "/v1/runners/leases/"+pull.Lease.ID+"/report", worker.Credential, api.RunnerReport{Merge: &api.WorkerMerge{State: factory.MergeFailed, Why: "the forge would not merge it: Base branch was modified"}}, 200, nil)
	f.request(t, "POST", "/v1/runners/leases/"+pull.Lease.ID+"/done", worker.Credential, api.RunnerDone{State: "done"}, 200, nil)
	var job api.Job
	f.request(t, "GET", path, "operator-secret", nil, 200, &job)
	if job.State != api.StateNeedsYou || job.Reason != "merge_failed" || job.Merge == nil || job.Merge.Run == nil || !strings.Contains(job.Merge.Run.Why, "Base branch was modified") {
		t.Fatalf("failed merge = state %s reason %s merge %+v", job.State, job.Reason, job.Merge)
	}
	f.now = f.now.Add(time.Hour)
	f.request(t, "GET", path, "operator-secret", nil, 200, nil)
	if saved, _ := f.st.Get(it.ID); saved.Merging == nil || saved.Merging.State != factory.MergeFailed {
		t.Fatalf("the sweep retried a failed merge: %+v", saved.Merging)
	}
	// A person may press Merge again.
	if job.Can.Merge != true {
		t.Fatalf("can.merge after a failure = %v", job.Can.Merge)
	}
}

// Approving the proof pins the pipeline's merge settings on the job.
func TestApprovingTheProofPinsTheMergePolicy(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	f.seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
	f.seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.Recipe{Merge: factory.MergePolicy{Auto: true, Method: factory.MergeMethodRebase}}, nil, "", nil
	}
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	it := f.add(t, "owner")
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State, it.HeadCommit = factory.StateLanded, mergeTestHead
		it.Proof = []factory.Claim{{Text: "works", OK: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/approve", it.ID), "operator-secret", map[string]string{"commit": mergeTestHead}, 200, nil)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MergePolicy == nil || !saved.MergePolicy.Auto || saved.MergePolicy.Method != factory.MergeMethodRebase {
		t.Fatalf("pinned = %+v", saved.MergePolicy)
	}
}
