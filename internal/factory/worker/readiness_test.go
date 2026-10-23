package worker_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

type readinessSource struct {
	*pullSource
	ready    atomic.Int32
	terminal string
}

func (s *readinessSource) Follow(_ context.Context, it factory.Item) (factory.PullRequest, error) {
	pr := *it.PullRequest
	if s.terminal != "" {
		pr.State = s.terminal
	}
	return pr, nil
}

func (s *readinessSource) Ready(_ context.Context, it factory.Item) (factory.PullRequest, error) {
	if !factory.ReadinessPending(it) {
		return factory.PullRequest{}, fmt.Errorf("no reviewed snapshot")
	}
	s.ready.Add(1)
	pr := *it.PullRequest
	pr.Draft = false
	return pr, nil
}
func TestReadinessRealServiceWorkerUsesRecordedAutoOrManualApproval(t *testing.T) {
	for _, mode := range []string{factory.AutonomyAuto, factory.AutonomyManual, "external ready", "external merge"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			git(t, "", "init", "-q", "-b", "main", dir)
			os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0600)
			git(t, dir, "add", "a.go")
			git(t, dir, "commit", "-qm", "initial")
			head := git(t, dir, "rev-parse", "HEAD")
			src := &readinessSource{pullSource: &pullSource{}}
			if mode == "external merge" {
				src.terminal = factory.PRMerged
			}
			g := newRigWith(t, nil, func(o *run.Options) {
				o.Workdir = func(factory.Item) (string, error) { return dir, nil }
				o.Source = func(string) factory.Source { return src }
				o.Push = func(string, string) error { t.Error("mark ready pushed code"); return nil }
				o.Exec[factory.StageChat] = run.ExecutorFunc(func(_ context.Context, j run.Job) (factory.StageResult, error) {
					if j.Item.FollowUp.Kind != "readiness" {
						return factory.StageResult{}, fmt.Errorf("unexpected repair")
					}
					return factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Final diff and actual PR body match; review feedback addressed, no unfinished work", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}, nil
				})
			}, "me")
			id := g.add(t, check("true"))
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.Autonomy = mode
				if mode == "external ready" || mode == "external merge" {
					it.Autonomy = factory.AutonomyAuto
				}
				it.Starter = "me"
				it.Cap = 10
				it.State = factory.StateShipped
				it.HeadCommit = head
				it.Publish = "pr"
				it.PullRequest = &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, ReadinessKey: "snapshot", ReadinessContext: "complete review material"}
				if mode == "external ready" || mode == "external merge" {
					it.PullRequest.Draft = false
					it.HeadCommit = strings.Repeat("b", 40)
				}
				it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}}
				it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}
				if !factory.QueueCIFollowUp(it, time.Now()) {
					return fmt.Errorf("readiness not queued")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if mode == "external merge" {
				ended := g.waitFor(t, id, factory.StateShipped)
				if ended.PullRequest.State != factory.PRMerged || ended.FollowUp.State != factory.PRMerged || src.ready.Load() != 0 {
					t.Fatal("observed terminal PR did not release maintenance without publication")
				}
				return
			}
			if mode == "external ready" {
				checked := g.waitFor(t, id, factory.StateShipped)
				if checked.HeadCommit != head || checked.FollowUp.Readiness.State != "checked" || src.ready.Load() != 0 {
					t.Fatal("external ready head did not complete a fresh review without another ready transition")
				}
				return
			}
			if mode == factory.AutonomyManual {
				landed := g.waitFor(t, id, factory.StateLanded)
				if src.ready.Load() != 0 || !strings.Contains(landed.FollowUp.Reason, "mark the existing draft PR ready") {
					t.Fatal("Manual did not ask before changing draft status")
				}
				if _, err := g.floor.Load(); err != nil {
					t.Fatal(err)
				}
				if _, err := g.floor.SignOff(id, false); err != nil {
					t.Fatal(err)
				}
			}
			it := g.waitFor(t, id, factory.StateShipped)
			if it.PullRequest.Draft || src.ready.Load() != 1 || len(src.writes()) != 0 || it.FollowUp.Readiness.State != "ready" || it.MergePolicy != nil {
				t.Fatalf("readiness lifecycle lost: draft=%t writes=%d state=%s", it.PullRequest.Draft, src.ready.Load(), it.FollowUp.Readiness.State)
			}
			events, _ := g.record.Since(context.Background(), time.Time{})
			approvals := 0
			for _, e := range events {
				if e.Kind == record.Approved {
					approvals++
					if e.Commit != head {
						t.Error("approval used wrong head")
					}
				}
			}
			seenReview, seenReady := false, false
			for _, a := range it.Activity {
				seenReview = seenReview || strings.HasPrefix(a.What, "Final readiness review passed")
				seenReady = seenReady || strings.HasPrefix(a.What, "PR marked ready for review")
			}
			if !seenReview || !seenReady {
				t.Fatal("readiness evidence missing from job Timeline")
			}
			if approvals != 1 {
				t.Fatalf("approval count=%d", approvals)
			}
			if !strings.Contains(strings.Join(func() []string {
				var v []string
				for _, l := range it.Stream.Log {
					v = append(v, l.Text)
				}
				return v
			}(), "\n"), "ready for review") {
				t.Fatal("readiness milestone missing from history")
			}
		})
	}
}
