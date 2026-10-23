package run

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFollowPRVerifiesThenWaitsForServiceRecordedUpdate(t *testing.T) {
	g := newPublishRig(t, factory.Item{Autonomy: factory.AutonomyAuto, Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Repair behavior", Cap: 3})
	mustGit(t, g.dir, "add", "ledger.go")
	mustGit(t, g.dir, "commit", "-qm", "initial issue fix")
	head := mustGit(t, g.dir, "rev-parse", "HEAD")
	pr := factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: head, ChecksRead: true, Checks: factory.PRChecks{Failed: 1}}
	g.r.opts.Source = func(string) factory.Source { return ciPublishSource{g.src, pr} }
	pushed := 0
	g.r.opts.Push = func(string, string) error { pushed++; return nil }
	g.r.opts.Exec = map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, j Job) (factory.StageResult, error) {
		// A dependency manifest was not in the original issue's changed files.
		if err := os.WriteFile(filepath.Join(g.dir, "go.mod"), []byte("module example\n// updated dependency\n"), 0600); err != nil {
			return factory.StageResult{}, err
		}
		return factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 2}, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "Dependency upgrade verified"}, Claims: []factory.Claim{{Text: "regression passes", OK: true, Medium: "test", Evidence: "focused dependency and regression checks"}}}, nil
	})}
	if err := g.st.Update(g.id, func(it *factory.Item) error {
		it.State = factory.StateShipped
		it.Publish = "pr"
		it.HeadCommit = head
		it.PullRequest = &pr
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}}
		factory.WatchPublishedPR(it)
		it.FollowUp.AutoPush = true
		it.FollowUp.PublishConsent = testTrailer + " " + head
		factory.QueueCIFollowUp(it, time.Now())
		it.State = factory.StateRunning
		it.Stream.Paused = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Hold(g.id, false); err != nil {
		t.Fatal(err)
	}
	it := g.settle(t)
	if it.State != factory.StateLanded || it.FollowUp.State != "waiting_approval" || pushed != 0 || len(g.src.got) != 0 || it.PullRequest.Number != 7 || it.HeadCommit == head {
		t.Fatalf("followup=%+v state=%s pushes=%d source writes=%d", it.FollowUp, it.State, pushed, len(g.src.got))
	}
	if !it.PullRequest.Draft {
		t.Fatal("marked PR ready without user action")
	}
}
