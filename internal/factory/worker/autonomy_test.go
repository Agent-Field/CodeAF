package worker_test

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Real service, lease, worker, shell checks, Git commit and local bare push;
// only the GitHub source is fake. No model, live job or external write exists.
func TestAutoPipelinePassesPlanGateAndPublishesWithoutUserApproval(t *testing.T) {
	testAutoPipeline(t, false)
}

func TestRecoveryAutoPipelineRepairsTransientFailureAndUpdatesTheSamePR(t *testing.T) {
	testAutoPipeline(t, true)
}

func testAutoPipeline(t *testing.T, disconnectFirst bool) {
	var repairCalls atomic.Int32
	checkout := filepath.Join(t.TempDir(), "api")
	git(t, "", "init", "-q", "-b", "main", checkout)
	if err := os.WriteFile(filepath.Join(checkout, "ledger.go"), []byte("package ledger\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "add", "ledger.go")
	git(t, checkout, "commit", "-qm", "initial")
	bare := filepath.Join(t.TempDir(), "remote.git")
	git(t, "", "init", "-q", "--bare", bare)
	git(t, checkout, "remote", "add", "origin", bare)
	dirs := run.NewWorkdirs(filepath.Join(t.TempDir(), "work"), run.ExecGit{})
	src := &autonomySource{pullSource: &pullSource{}}
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Source = func(string) factory.Source { return src }
		o.Workdir = func(it factory.Item) (string, error) { return dirs.For(it, checkout) }
		o.Push = run.GitPush(run.ExecGit{})
		o.Exec[factory.StageChat] = run.ExecutorFunc(func(_ context.Context, j run.Job) (factory.StageResult, error) {
			call := repairCalls.Add(1)
			if disconnectFirst && call == 1 {
				return factory.StageResult{}, fmt.Errorf("connection reset by peer")
			}
			if err := os.WriteFile(filepath.Join(j.Dir, "dependency.go"), []byte("package ledger\n// checked repair\n"), 0600); err != nil {
				return factory.StageResult{}, err
			}
			return factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "checked local repair"}, Tests: &factory.TestCounts{Passed: 1}, Claims: []factory.Claim{{Text: "repair passes", OK: true, Evidence: "isolated scripted check", Medium: "test"}}}, nil
		})
	})
	plan := check("true")
	plan.Name = "plan"
	write := check("printf 'package ledger\nconst fixed = true\n' > ledger.go")
	write.Name = "write"
	id := g.add(t, plan, factory.Stage{Name: "approve", Kind: factory.StageGate, On: true}, write)
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Autonomy = factory.AutonomyAuto
		it.Starter = "me"
		it.Cap = 10
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateShipped)
	if it.PullRequest == nil || it.PullRequest.Number != 7 || len(src.writes()) != 1 || it.Stream.Phases[1].State != factory.PhaseDone {
		t.Fatalf("Auto pipeline did not publish once: pr=%+v writes=%d", it.PullRequest, len(src.writes()))
	}
	if got := git(t, bare, "rev-parse", dirs.Branch(it)); got != it.HeadCommit {
		t.Fatal("pushed a different head")
	}
	events, err := g.record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	planApprovals, proofApprovals := 0, 0
	for _, e := range events {
		if e.Kind == record.Approved {
			if !strings.Contains(e.Detail, "Auto mode") {
				t.Fatal("approval did not identify Auto")
			}
			if strings.HasPrefix(e.Detail, "plan") {
				planApprovals++
			}
			if strings.HasPrefix(e.Detail, "proof") {
				proofApprovals++
				if e.Commit != it.HeadCommit {
					t.Fatal("proof approval did not bind the pushed head")
				}
			}
		}
	}
	if planApprovals != 1 || proofApprovals != 1 {
		t.Fatalf("approval counts: plan=%d proof=%d", planApprovals, proofApprovals)
	}

	initialHead := it.HeadCommit
	src.mu.Lock()
	src.current = *it.PullRequest
	src.current.HeadSHA = initialHead
	src.mu.Unlock()
	if err := g.st.Update(id, func(cur *factory.Item) error {
		cur.PullRequest.HeadSHA = initialHead
		cur.PullRequest.ChecksRead = true
		cur.PullRequest.Checks = factory.PRChecks{Failed: 1}
		if !factory.QueueCIFollowUp(cur, time.Now()) {
			return fmt.Errorf("repair not queued")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	repaired := g.waitFor(t, id, factory.StateShipped)
	if repaired.HeadCommit == initialHead || repaired.PullRequest.Number != 7 || len(src.writes()) != 1 {
		t.Fatalf("repair replaced the PR or did not publish: head=%s writes=%d", repaired.HeadCommit, len(src.writes()))
	}
	if got := git(t, bare, "rev-parse", dirs.Branch(repaired)); got != repaired.HeadCommit {
		t.Fatal("repair head was not pushed")
	}
	events, _ = g.record.Since(context.Background(), time.Time{})
	newestProof := 0
	for _, e := range events {
		if e.Kind == record.Approved && e.Commit == repaired.HeadCommit && strings.Contains(e.Detail, "Auto mode") {
			newestProof++
		}
	}
	if newestProof != 1 {
		t.Fatal("repair did not receive exactly one fresh exact-head Auto approval")
	}
	if disconnectFirst {
		retries := 0
		for _, e := range events {
			if e.Kind == record.StepRerun && strings.Contains(e.Detail, "Automatic retry") {
				retries++
			}
		}
		if repairCalls.Load() != 2 || retries != 1 {
			t.Fatalf("repair calls=%d recovery events=%d", repairCalls.Load(), retries)
		}
		if phase(repaired, "plan").Round != phase(it, "plan").Round {
			t.Fatal("recovery reran the completed plan")
		}
	}
}

func TestManualSwitchCannotBeBypassedByWorkersCachedAutoMode(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	g := newRigWith(t, nil, func(o *run.Options) {
		inner := o.Exec[factory.StageCheck]
		o.Exec[factory.StageCheck] = run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
			if j.Stage.Name == "plan" {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
			}
			return inner.Run(ctx, j)
		})
	})
	plan := check("true")
	plan.Name = "plan"
	write := check("true")
	write.Name = "write"
	id := g.add(t, plan, factory.Stage{Name: "approve", Kind: factory.StageGate, On: true}, write)
	if err := g.st.Update(id, func(it *factory.Item) error { it.Autonomy = "auto"; it.Starter = "me"; it.Cap = 10; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("plan never ran")
	}
	// This is the interval between the factory's confirmed write and delivery
	// of its mode signal. The worker still has Auto in its mirror.
	if err := g.st.Update(id, func(it *factory.Item) error { it.Autonomy = "manual"; return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	it := g.waitFor(t, id, factory.StateNeedsYou)
	if it.QKind != factory.QKindApprove || it.Stream.Phases[2].State != factory.PhasePending {
		t.Fatalf("Manual approval bypassed: q=%s write=%s", it.QKind, it.Stream.Phases[2].State)
	}
}

type autonomySource struct {
	*pullSource
	mu      sync.Mutex
	current factory.PullRequest
}

func (s *autonomySource) Follow(context.Context, factory.Item) (factory.PullRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, nil
}
