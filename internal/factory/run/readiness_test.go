package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestReadinessRoundLandsAtExistingApprovalWithoutChangingCode(t *testing.T) {
	for _, mode := range []string{"pass", "external head", "unfinished", "no report", "verification retry", "report retry"} {
		t.Run(mode, func(t *testing.T) {
			dir := gitRig(t)
			head := mustGit(t, dir, "rev-parse", "HEAD")
			var calls atomic.Int32
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, j Job) (factory.StageResult, error) {
				call := calls.Add(1)
				if !strings.Contains(ciFollowUpBrief(j.Item), "read only") {
					t.Error("repair instructions used for audit")
				}
				r := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Inspected full diff and description; review feedback is addressed", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: mode != "unfinished"}}}
				if mode == "no report" || (mode == "report retry" && call == 1) {
					r.Repair.Readiness = nil
				}
				if mode == "verification retry" && call == 1 {
					r.Done, r.Exit = false, 1
					r.Notes = []string{"Unresolved verification command failed: go test ./... · failed"}
					r.Output = "Verification blocked: go test ./... · failed"
				}
				return r, nil
			})}, func(o *Options) {
				o.Workdir = func(factory.Item) (string, error) { return dir, nil }
				o.Push = func(string, string) error { t.Error("audit pushed code"); return nil }
			})
			id := g.add("review readiness", factory.Stage{Name: "proof", Kind: factory.StageChat, Until: factory.UntilProven})
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateShipped
				it.Publish = "pr"
				it.Cap = 10
				it.Autonomy = factory.AutonomyManual
				it.HeadCommit = head
				if mode == "external head" {
					it.HeadCommit = strings.Repeat("b", 40)
				}
				it.PullRequest = &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, ReadinessKey: "snapshot", ReadinessContext: "actual diff and body"}
				it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}
				it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}}
				factory.QueueCIFollowUp(it, time.Now())
				it.State = factory.StateRunning
				it.Stream.Paused = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Hold(id, false); err != nil {
				t.Fatal(err)
			}
			want := factory.StateLanded
			if mode == "unfinished" || mode == "no report" {
				want = factory.StateNeedsYou
			}
			it := g.waitState(id, want)
			if got := mustGit(t, dir, "rev-parse", "HEAD"); got != head {
				t.Fatal("audit committed work")
			}
			if mode != "unfinished" && mode != "no report" {
				if !factory.ReadinessPending(it) || it.HeadCommit != head || !strings.Contains(it.FollowUp.Reason, "mark the existing draft PR ready") || len(it.Proof) != 1 {
					t.Fatal("readiness consent or evidence lost")
				}
			} else if factory.ReadinessPending(it) {
				t.Fatal("incomplete review passed")
			}
			if (mode == "verification retry" || mode == "report retry") && calls.Load() != 2 {
				t.Fatal("final review did not recover automatically in two rounds")
			}
		})
	}
}

func TestReadinessOrdinaryFindingContinuesToBoundedRepair(t *testing.T) {
	dir := gitRig(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, j Job) (factory.StageResult, error) {
		if calls.Add(1) == 1 {
			return factory.StageResult{Done: false, Findings: 1, Repair: &factory.CIRepairResult{Decision: "needs_repair", Reason: "One changed branch omits the error check"}}, nil
		}
		if j.Item.FollowUp.Kind != "pr" {
			t.Error("did not leave read-only audit")
		}
		if err := os.WriteFile(filepath.Join(dir, "ledger.go"), []byte("package ledger\n// final review finding repaired\n"), 0600); err != nil {
			return factory.StageResult{}, err
		}
		return factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 1}, Claims: []factory.Claim{{Text: "error check verified", OK: true, Medium: "test", Evidence: "focused regression"}}, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "error propagation repaired and checked"}}, nil
	})}, func(o *Options) {
		o.Workdir = func(factory.Item) (string, error) { return dir, nil }
		o.Push = func(string, string) error { t.Error("pushed without approval"); return nil }
	})
	id := g.add("readiness finding", factory.Stage{Name: "proof", Kind: factory.StageChat, Until: factory.UntilProven})
	if e := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateShipped
		it.Publish = "pr"
		it.Cap = 10
		it.HeadCommit = head
		it.PullRequest = &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, ReadinessKey: "snapshot"}
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}
		it.Stream = &factory.Stream{}
		factory.QueueCIFollowUp(it, time.Now())
		it.State = factory.StateRunning
		it.Stream.Paused = true
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := g.r.Hold(id, false); e != nil {
		t.Fatal(e)
	}
	it := g.waitState(id, factory.StateLanded)
	if calls.Load() != 2 || it.FollowUp.Kind != "pr" || it.FollowUp.State != "waiting_approval" || it.HeadCommit == head || it.FollowUp.Readiness != nil {
		t.Fatal("ordinary finding did not repair through the existing pipeline")
	}
}

type readinessPublishSource struct {
	*prSource
	calls atomic.Int32
}

func (s *readinessPublishSource) Ready(_ context.Context, it factory.Item) (factory.PullRequest, error) {
	s.calls.Add(1)
	pr := *it.PullRequest
	pr.Draft = false
	return pr, nil
}

func TestReadinessPublicationReachesReadyWithoutPushingOrMerging(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
	head := mustGit(t, g.dir, "rev-parse", "HEAD")
	source := &readinessPublishSource{prSource: g.src}
	g.r.opts.Source = func(string) factory.Source { return source }
	g.r.opts.Push = func(string, string) error { t.Error("ready transition pushed code"); return nil }
	if err := g.st.Update(g.id, func(it *factory.Item) error {
		it.HeadCommit = head
		it.Autonomy = factory.AutonomyAuto
		it.PullRequest = &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, ReadinessKey: "snapshot", Required: 1, ReviewDecision: "review_required"}
		it.FollowUp = &factory.PRFollowUp{Kind: "readiness", State: "ready_approval", Readiness: &factory.PRReadiness{Key: "snapshot", Head: head, State: "reviewed", Summary: "Checked diff, description and feedback"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Publish(g.id, testTrailer+" "+head); err != nil {
		t.Fatal(err)
	}
	it := g.settle(t)
	if it.State != factory.StateShipped || it.PullRequest.Draft || source.calls.Load() != 1 || it.FollowUp.Readiness.State != "ready" || it.FollowUp.State != "watching" {
		t.Fatal("ready transition did not settle into monitoring")
	}
	if it.Merging != nil || len(g.src.got) != 0 {
		t.Fatal("ready transition created another PR, comment or merge")
	}
}

func TestReadinessExternalReadyHeadIsCheckedWithoutPublishing(t *testing.T) {
	dir := gitRig(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, j Job) (factory.StageResult, error) {
		calls.Add(1)
		if j.Item.HeadCommit == head {
			t.Error("head promoted before review passed")
		}
		return factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Reviewed the current external commit, complete diff, description and feedback", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}, nil
	})}, func(o *Options) {
		o.Workdir = func(factory.Item) (string, error) { return dir, nil }
		o.Push = func(string, string) error { t.Error("external checked head was pushed"); return nil }
	})
	id := g.add("external ready commit", chat("proof"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State, it.Publish, it.HeadCommit, it.Cap = factory.StateShipped, "pr", strings.Repeat("b", 40), 10
		it.Autonomy = factory.AutonomyAuto
		it.PullRequest = &factory.PullRequest{Number: 7, State: factory.PROpen, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, ReadinessKey: "external-snapshot", ReadinessContext: "full diff and description"}
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", State: factory.PhaseDone}}}
		factory.QueueCIFollowUp(it, time.Now())
		it.State = factory.StateRunning
		it.Stream.Paused = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	got := g.waitState(id, factory.StateShipped)
	if calls.Load() != 1 || got.HeadCommit != head || got.PullRequest.Draft || got.FollowUp.State != "healthy" || got.FollowUp.Readiness.State != "checked" || factory.ReadinessPending(got) || got.Approval != nil {
		t.Fatalf("external review did not return directly to monitoring: %+v", got.FollowUp)
	}
}
