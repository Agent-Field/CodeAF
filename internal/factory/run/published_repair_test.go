package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func completedRepairReport(head string) factory.StageResult {
	return factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "Published repair passes its regression check", OK: true, Evidence: "Successful current-head regression run", Medium: "test"}}, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "The published head already contains the requested repair; all checks passed", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}
}

func TestPublishedRepairRecheckReturnsToMonitoringWithoutAnotherCommit(t *testing.T) {
	for _, ci := range []string{"pending", "passed"} {
		t.Run(ci, func(t *testing.T) {
			dir := gitRig(t)
			head := mustGit(t, dir, "rev-parse", "HEAD")
			var calls atomic.Int32
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, j Job) (factory.StageResult, error) {
				calls.Add(1)
				return completedRepairReport(head), nil
			})}, func(o *Options) {
				o.Workdir = func(factory.Item) (string, error) { return dir, nil }
				o.Push = func(string, string) error { t.Error("an unchanged PR was pushed"); return nil }
			})
			id := g.add("recheck published fix", chat("proof"))
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.Cap = 10
				it.State, it.Publish, it.HeadCommit = factory.StateRunning, "pr", head
				it.PullRequest = &factory.PullRequest{Number: 11, State: factory.PROpen, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, Checks: factory.PRChecks{Pending: 1}, CI: "ci …"}
				if ci == "passed" {
					it.PullRequest.Checks = factory.PRChecks{Passed: 1}
					it.PullRequest.CI = "ci ✓"
				}
				it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "queued", HeadSHA: head, Attempt: 2, Limit: 3, Request: "Check the published repair"}
				it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}, Paused: true}
				factory.EnsureFollowPR(it)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Hold(id, false); err != nil {
				t.Fatal(err)
			}
			it := g.waitState(id, factory.StateShipped)
			if it.Question != "" || it.FollowUp.State != "watching" || it.FollowUp.Attempt != 2 || it.Approval != nil || calls.Load() != 1 {
				t.Fatalf("unexpected recheck ending: question=%q follow=%+v calls=%d", it.Question, it.FollowUp, calls.Load())
			}
			if it.Stream.Phases[len(it.Stream.Phases)-1].Result.Repair.Decision != "no_change" {
				t.Fatal("incorrect fixed label was not reconciled")
			}
			if got := mustGit(t, dir, "rev-parse", "HEAD"); got != head {
				t.Fatal("monitoring manufactured a commit")
			}
		})
	}
}

func TestPublishedRepairNormalizationRequiresCurrentCleanHeadAndCompleteReport(t *testing.T) {
	for _, mode := range []string{"clean", "dirty", "untracked", "local head moved", "remote head moved", "new feedback", "pending decision", "missing review", "stale report", "failed check"} {
		t.Run(mode, func(t *testing.T) {
			dir := gitRig(t)
			head := mustGit(t, dir, "rev-parse", "HEAD")
			g := newRig(t, nil, func(o *Options) { o.Workdir = func(factory.Item) (string, error) { return dir, nil } })
			id := g.add("published repair", chat("followpr"))
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.PullRequest = &factory.PullRequest{Number: 11, State: factory.PROpen, HeadSHA: head}
				it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "running", HeadSHA: head}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			captured, _ := g.st.Get(id)
			r := completedRepairReport(head)
			switch mode {
			case "dirty":
				if err := os.WriteFile(filepath.Join(dir, "ledger.go"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "untracked":
				if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
			case "local head moved":
				mustGit(t, dir, "-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "new local commit")
			case "remote head moved", "new feedback", "pending decision":
				if err := g.st.Update(id, func(it *factory.Item) error {
					if mode == "remote head moved" {
						it.PullRequest.HeadSHA = strings.Repeat("f", 40)
					}
					if mode == "new feedback" {
						it.PullRequest.Feedback = "A new finding arrived during the check"
					}
					if mode == "pending decision" {
						it.Question = "Choose API behavior"
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case "missing review":
				r.Repair.Readiness = nil
			case "stale report":
				r.Repair.Readiness.Head = strings.Repeat("f", 40)
			case "failed check":
				r.Done = false
				r.Exit = 1
				r.Notes = []string{"Unresolved verification command failed: cargo test failed"}
			}
			got := (&floorLoop{r: g.r}).completedPublishedRepair(captured, r)
			if (got.Repair.Decision == "no_change") != (mode == "clean") {
				t.Fatalf("mode %s became %s", mode, got.Repair.Decision)
			}
		})
	}
}
