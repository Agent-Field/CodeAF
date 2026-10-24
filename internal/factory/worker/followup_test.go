package worker_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestQueuedCIRepairCrossesTheWorkerWireAndRetainsItsPR(t *testing.T) {
	for _, baseline := range []bool{false, true} {
		t.Run(map[bool]string{false: "repair", true: "baseline"}[baseline], func(t *testing.T) {
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				c := exec.Command("git", args...)
				c.Dir = dir
				b, e := c.CombinedOutput()
				if e != nil {
					t.Fatalf("git: %v %s", e, b)
				}
				return strings.TrimSpace(string(b))
			}
			git("init", "-q", "-b", "main")
			git("config", "user.name", "test")
			git("config", "user.email", "test@example.invalid")
			git("config", "commit.gpgsign", "false")
			os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n"), 0600)
			git("add", ".")
			git("commit", "-qm", "base")
			head := git("rev-parse", "HEAD")
			g := newRigWith(t, nil, func(o *run.Options) {
				o.Workdir = func(factory.Item) (string, error) { return dir, nil }
				o.Exec = map[factory.StageKind]run.Executor{factory.StageChat: run.ExecutorFunc(func(_ context.Context, j run.Job) (factory.StageResult, error) {
					if factory.IsCIRepairStage(j.Stage) {
						if j.Item.FollowUp == nil || j.Item.FollowUp.HeadSHA != head {
							t.Error("worker lost the repair identity")
						}
						if baseline {
							return factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "needs_you", Reason: "Pre-existing dependency audit needs a scope decision."}}, nil
						}
						if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n// repaired\n"), 0600); err != nil {
							return factory.StageResult{}, err
						}
						return factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 2}, Claims: []factory.Claim{{Text: "repair passes", OK: true, Evidence: "focused tests", Medium: "test"}}, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "Regression repaired."}}, nil
					}
					return factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 2}, Claims: []factory.Claim{{Text: "regression test passes", OK: true, Evidence: "focused test", Medium: "test"}}}, nil
				})}
			})
			it := factory.Item{Repo: "api", Num: 12, Kind: factory.KindIssue, Title: "Repair CI", State: factory.StateShipped, Publish: "pr", HeadCommit: head, Change: &factory.Change{Files: []factory.ChangedFile{{Path: "app.go"}}}, PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen, URL: "https://github.com/acme/api/pull/7", HeadSHA: head, ChecksRead: true, Checks: factory.PRChecks{Failed: 1}}, Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}}}
			factory.WatchPublishedPR(&it)
			factory.QueueCIFollowUp(&it, time.Now())
			saved, err := g.st.Create(it)
			if err != nil {
				t.Fatal(err)
			}
			want := factory.StateLanded
			if baseline {
				want = factory.StateNeedsYou
			}
			got := g.waitFor(t, saved.ID, want)
			if got.PullRequest == nil || got.PullRequest.Number != 7 || got.FollowUp == nil || got.FollowUp.Attempt != 1 || got.FollowUp.HeadSHA != head {
				t.Fatalf("repair identity lost: %+v", got.FollowUp)
			}
			if baseline {
				if got.FollowUp.State != "needs_you" || !strings.Contains(got.Question, "dependency audit") {
					t.Fatalf("escalation lost: %+v %s", got.FollowUp, got.Question)
				}
			} else {
				if got.FollowUp.State != "waiting_approval" || got.HeadCommit == head || !got.Stream.Phases[len(got.Stream.Phases)-1].Result.Done {
					t.Fatalf("repair not awaiting approval: %+v", got)
				}
			}
		})
	}
}
