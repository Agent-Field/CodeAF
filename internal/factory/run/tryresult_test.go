package run

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// EACH ROUND KEEPS HOW ITS TRY ENDED: the word its rule gives the report and
// the counted reason, so the job page reads "Try 1 · not clean · 2 findings"
// without parsing what the step wrote. The prose summary says otherwise on
// purpose: the word and reason come only from the structured report.
func TestEachRoundKeepsItsTrysResultWordAndReason(t *testing.T) {
	var round atomic.Int64
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			n := round.Add(1)
			findings := 0
			if n == 1 {
				findings = 2
			}
			return factory.StageResult{Done: true, Findings: findings, Output: "all good", Chat: "review-" + strconv.FormatInt(n, 10)}, nil
		}),
	}, nil)
	review := factory.Stage{Name: "review", Kind: factory.StageChat, Ask: "review it", Until: factory.UntilClean, Max: 2}
	id := g.add("review it", review)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	rounds := it.Stream.Phases[0].Rounds
	if len(rounds) != 2 {
		t.Fatalf("the step kept rounds %+v", rounds)
	}
	for i, want := range [][2]string{{"not clean", "2 findings"}, {"clean", "no findings"}} {
		if rounds[i].Result != want[0] || rounds[i].Reason != want[1] {
			t.Fatalf("try %d reads %q · %q, want %q · %q", i+1, rounds[i].Result, rounds[i].Reason, want[0], want[1])
		}
	}
}
