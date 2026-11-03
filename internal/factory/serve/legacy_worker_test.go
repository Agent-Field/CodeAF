package serve_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	legacyrun "github.com/Agent-Field/codeaf/internal/factory/legacy/run"
	legacyworker "github.com/Agent-Field/codeaf/internal/factory/legacy/worker"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Run the merge-base worker implementation against the current host. The worker
// still accepts the pull response but enforces its old per-run budget rule.
func TestLegacyWorkerCannotSpendAtLifetimeCap(t *testing.T) {
	f := workerSetupAs(t, "owner")
	attached := f.attach(t, "owner")
	it := f.add(t, "owner")
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.Cap, cur.SpentBefore = 1, 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mirror, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paid := make(chan factory.Item, 1)
	runner := legacyrun.New(legacyrun.Options{Store: mirror, Exec: map[factory.StageKind]legacyrun.Executor{
		factory.StageChat: legacyrun.ExecutorFunc(func(context.Context, legacyrun.Job) (factory.StageResult, error) {
			saved, _ := f.st.Get(it.ID)
			paid <- saved
			return factory.StageResult{Done: true, Spent: .125}, nil
		}),
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		legacyworker.Run(ctx, legacyworker.Options{
			Dial: func(int) *client.WorkerClient {
				return client.Open(f.server.URL, config.AccessToken).Worker(attached.Credential)
			},
			Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: time.Millisecond, BeatEvery: time.Millisecond,
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case saved := <-paid:
		t.Fatalf("legacy worker began paid executor with authoritative lifetime=%g cap=%g", factory.JobLifetimeSpent(saved), saved.Cap)
	case <-time.After(2 * time.Second):
	}
}
