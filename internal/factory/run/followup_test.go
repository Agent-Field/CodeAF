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

func TestCIRepairRetestsProvesAndWaitsForApprovalOnTheSamePR(t *testing.T) {
	for _, mode := range []string{"repair", "baseline", "outside scope"} {
		t.Run(mode, func(t *testing.T) {
			dir := gitRig(t)
			mustGit(t, dir, "config", "user.name", "t")
			mustGit(t, dir, "config", "user.email", "t@t")
			mustGit(t, dir, "config", "commit.gpgsign", "false")
			head := mustGit(t, dir, "rev-parse", "HEAD")
			var calls atomic.Int32
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, j Job) (factory.StageResult, error) {
				calls.Add(1)
				if factory.IsCIRepairStage(j.Stage) {
					if mode == "baseline" {
						return factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "needs_you", Reason: "Existing dependency audit failure; no dependency manifest changed."}}, nil
					}
					name := "ledger.go"
					if mode == "outside scope" {
						name = "unrelated.go"
					}
					if err := os.WriteFile(filepath.Join(dir, name), []byte("package ledger\n// regression repaired\n"), 0o600); err != nil {
						return factory.StageResult{}, err
					}
					return factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 2}, Claims: []factory.Claim{{Text: "repair passes", OK: true, Evidence: "focused tests passed", Medium: "test"}}, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "Reproduced and repaired the changed code."}}, nil
				}
				return factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 2}, Claims: []factory.Claim{{Text: "repair tests pass", OK: true, Evidence: "focused tests passed", Medium: "test"}}}, nil
			})}, func(o *Options) {
				o.Workdir = func(factory.Item) (string, error) { return dir, nil }
				o.Push = func(string, string) error { t.Error("pushed without approval"); return nil }
			})
			id := g.add("fix regression", factory.Stage{Name: "proof", Kind: factory.StageChat, Until: factory.UntilProven})
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.Kind = factory.KindIssue
				it.State = factory.StateShipped
				it.Publish = "pr"
				it.HeadCommit = head
				it.Change = &factory.Change{Files: []factory.ChangedFile{{Path: "ledger.go"}}}
				it.PullRequest = &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, HeadSHA: head, ChecksRead: true, Checks: factory.PRChecks{Failed: 1}}
				it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}}
				factory.WatchPublishedPR(it)
				factory.QueueCIFollowUp(it, time.Now())
				// The worker stands a persisted follow-up on the existing runner's resume door.
				it.State = factory.StateRunning
				it.Stream.Paused = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Hold(id, false); err != nil {
				t.Fatal(err)
			}
			if mode != "baseline" {
				it := g.waitState(id, factory.StateLanded)
				if it.PullRequest.Number != 7 || it.FollowUp.State != "waiting_approval" || calls.Load() != 1 || it.HeadCommit == head {
					t.Fatalf("bad repair ending: %+v calls %d", it.FollowUp, calls.Load())
				}
			} else {
				it := g.waitState(id, factory.StateNeedsYou)
				if it.FollowUp.State != "needs_you" || it.HeadCommit != head {
					t.Fatalf("bad escalation: %+v", it.FollowUp)
				}
				if mode == "baseline" && calls.Load() != 1 {
					t.Fatal("tested or proved an escalated baseline issue")
				}
			}
		})
	}
}

func TestMeasuredEvidenceDoesNotRepeatModelTestTotals(t *testing.T) {
	r := measuredTestEvidence(factory.StageResult{Output: "Ran full retrieval test suite: 95 tests pass. All 96 tests pass.", Tests: &factory.TestCounts{Passed: 93}, Claims: []factory.Claim{{Text: "95 tests pass", Evidence: "96 tests passed"}}})
	if strings.Contains(r.Output, "95") || strings.Contains(r.Output, "96") || !strings.Contains(r.Output, "93 passed") {
		t.Fatal(r.Output)
	}
	if strings.Contains(r.Claims[0].Text, "95") || strings.Contains(r.Claims[0].Evidence, "96") {
		t.Fatal(r.Claims)
	}
}

type ciPublishSource struct {
	*prSource
	pr factory.PullRequest
}

func (s ciPublishSource) Follow(context.Context, factory.Item) (factory.PullRequest, error) {
	return s.pr, nil
}
func TestCIRepairPublishRechecksTheExistingPRBeforePush(t *testing.T) {
	for _, state := range []string{"open", "closed", "moved"} {
		t.Run(state, func(t *testing.T) {
			g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
			old := mustGit(t, g.dir, "rev-parse", "HEAD")
			mustGit(t, g.dir, "add", "ledger.go")
			mustGit(t, g.dir, "commit", "-q", "-m", "CI repair")
			head := mustGit(t, g.dir, "rev-parse", "HEAD")
			it := g.item(t)
			it.HeadCommit = head
			it.PullRequest = &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: old}
			it.FollowUp = &factory.PRFollowUp{Kind: "ci", State: "waiting_approval", HeadSHA: old, Attempt: 1, Limit: 2}
			fresh := *it.PullRequest
			if state == "closed" {
				fresh.State = factory.PRClosed
			}
			if state == "moved" {
				fresh.HeadSHA = strings.Repeat("f", 40)
			}
			g.r.opts.Source = func(string) factory.Source { return ciPublishSource{g.src, fresh} }
			pushes := 0
			g.r.opts.Push = func(string, string) error { pushes++; return nil }
			result, err := g.r.publish(context.Background(), it, testTrailer+" "+head)
			if state == "open" {
				if err != nil || result.pr == nil || result.pr.Number != 7 || !result.pr.Draft || pushes != 1 {
					t.Fatalf("result=%+v pushes=%d err=%v", result, pushes, err)
				}
			} else if err == nil || pushes != 0 || len(g.src.got) != 0 {
				t.Fatalf("published to changed PR: pushes=%d err=%v", pushes, err)
			}
		})
	}
}

func TestPublishBodyRetainsReviewAndLatestRepairAfterLongWrite(t *testing.T) {
	it := factory.Item{Title: "Fix it", Stream: &factory.Stream{Phases: []factory.Phase{
		{Name: "write", Result: &factory.StageResult{Output: "Implemented dispatch.\n\nMore implementation detail.\n\nEven more detail."}},
		{Name: "review", Result: &factory.StageResult{Output: "Fixed the race discovered during review."}},
		{Name: "ci1", Result: &factory.StageResult{Output: "First CI repair."}},
		{Name: "ci2", Result: &factory.StageResult{Output: "Latest CI repair."}},
	}}}
	body := PublishBody(it, testTrailer)
	for _, want := range []string{"Implemented dispatch.", "Fixed the race", "Latest CI repair."} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "More implementation detail") {
		t.Fatal("write detail crowded out later fixes")
	}
}
