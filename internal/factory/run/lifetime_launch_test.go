package run

import (
	"context"
	"math"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestRunnerLaunchKeepsEarlierRunMoney(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
		job.Spend(.03)
		return done("proved"), nil
	})}, nil)
	id := g.add("Restart", chat("proof"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Stream = &factory.Stream{Spent: .138, Phases: []factory.Phase{{Name: "proof", State: factory.PhaseFailed, Round: 1}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if got := factory.JobLifetimeSpent(it); math.Abs(got-.168) > 1e-9 {
		t.Fatalf("runner restart lifetime=%g, want .168", got)
	}
	if got := factory.JobSpent(it); got != .03 {
		t.Fatalf("run spend=%g, want .03", got)
	}
}
