package worker_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// The next round uses the service's current permission.
func TestWorkerChecksCurrentWriteAccessBetweenRounds(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	observed := make(chan factory.Item, 1)
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
			if j.Stage.Name == "one" {
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
			} else {
				observed <- j.Item
			}
			return factory.StageResult{Done: true}, nil
		})}
	})
	saved, err := g.st.Create(factory.Item{Title: "member then stranger", Repo: "api", Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateQueued,
		Stages: []factory.Stage{{Name: "one", Kind: factory.StageCheck, On: true}, {Name: "two", Kind: factory.StageCheck, On: true}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "one", Kind: factory.StageCheck, State: factory.PhasePending}, {Name: "two", Kind: factory.StageCheck, State: factory.PhasePending}}}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("first stage did not start")
	}
	if err := g.st.Update(saved.ID, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-observed:
			t.Fatal("next stage ran without write access")
		default:
		}
		current, err := g.st.Get(saved.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Failure != nil && strings.Contains(current.Failure.Detail, "without write access") && factory.RunEnded(current) {
			if current.Failure.Step != "two" || current.Failure.Attempts != 0 || current.Stream.Phases[1].Round != 0 {
				t.Fatalf("untried round=%+v failure=%+v", current.Stream.Phases[1], current.Failure)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, _ := g.st.Get(saved.ID)
	t.Fatalf("permission refusal not complete: state=%s failure=%+v", current.State, current.Failure)
}
