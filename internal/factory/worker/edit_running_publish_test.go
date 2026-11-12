package worker_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// A STEP ADDED TO A RUNNING AUTO JOB STILL ENDS IN ITS PULL REQUEST. The live
// managers lane (job 61) added a security step during Write through the
// manager's door. This is that road on the real service, lease and worker:
// the edit withdraws the worker, the job is pulled again at Write, runs the
// added step and publishes once, and it never falls back to Backlog on the
// way. The runner-level race behind the live stall, a withdrawn Write still
// ending when the next lease asks to go on, is pinned by
// run.TestGoOnAfterAWithdrawWaitsForTheOldDriveAndRunsOn.
func TestAStepAddedDuringWriteStillPublishesInAuto(t *testing.T) {
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
	inWrite, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Source = func(string) factory.Source { return src }
		o.Workdir = func(it factory.Item) (string, error) { return dirs.For(it, checkout) }
		o.Push = run.GitPush(run.ExecGit{})
		o.Exec[factory.StageChat] = run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "no security finding in the diff", OK: true, Evidence: "read the diff", Medium: "inspection"}}}, nil
		})
		inner := o.Exec[factory.StageCheck]
		o.Exec[factory.StageCheck] = run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
			if j.Stage.Name == "write" {
				first := false
				once.Do(func() { first = true; close(inWrite) })
				if first {
					// The first Write waits, as a model's does, for the edit
					// to land while it is running.
					select {
					case <-release:
					case <-ctx.Done():
						return factory.StageResult{}, ctx.Err()
					}
				}
			}
			return inner.Run(ctx, j)
		})
	})
	plan := check("true")
	plan.Name = "plan"
	write := check("printf 'package ledger\nconst fixed = true\n' > ledger.go")
	write.Name = "write"
	review := check("true")
	review.Name = "review"
	id := g.add(t, plan, factory.Stage{Name: "approve", Kind: factory.StageGate, On: true}, write, review)
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
	<-inWrite
	desk, err := lease.Open(g.st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	security := factory.Stage{Name: "security", Kind: factory.StageChat, Ask: "audit the diff", Until: "done", On: true}
	if _, lines, err := lease.NewDoors(desk).EditSteps(id, factory.RunEdit{By: factory.ByManager, Add: []factory.Added{{Stage: security, After: "review"}}}, factory.DefaultRecipe()); err != nil || len(lines) == 0 {
		t.Fatalf("the edit = %v (%v)", lines, err)
	}
	close(release)
	// Every state the job passes through is kept, so a fall back to Backlog
	// on the way is caught even when Auto recovers it afterwards.
	var states []factory.State
	var it factory.Item
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		cur, err := g.st.Get(id)
		if err != nil {
			continue
		}
		if len(states) == 0 || states[len(states)-1] != cur.State {
			states = append(states, cur.State)
		}
		if cur.State == factory.StateShipped {
			it = cur
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the edited job never shipped; it went %v", states)
		}
	}
	for _, st := range states {
		if st == factory.StateNew {
			t.Fatalf("the edited job fell back to Backlog on its way: %v", states)
		}
	}
	if it.PullRequest == nil || it.PullRequest.Number == 0 || len(src.writes()) != 1 {
		t.Fatalf("the edited Auto job did not open its pull request once: pr=%+v writes=%d", it.PullRequest, len(src.writes()))
	}
	if st := phaseState(it, "security"); st != factory.PhaseDone {
		t.Fatalf("the added step is %q", st)
	}
	if factory.StoppedByPerson(it) {
		t.Fatal("the step edit recorded a person's stop")
	}
}
