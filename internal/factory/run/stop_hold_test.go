package run

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"testing"
	"time"
)

func TestRepeatedHoldRefusesPersonStop(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Kind: factory.KindIssue, State: factory.StateRunning, Stream: &factory.Stream{Paused: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { factory.MarkPersonStopped(cur); return nil }); err != nil {
		t.Fatal(err)
	}
	r := New(Options{Store: st})
	for n := 0; n < 3; n++ {
		if err := r.Hold(it.ID, true); !errors.Is(err, factory.ErrPersonStopped) {
			t.Fatalf("hold %d accepted Stop: %v", n, err)
		}
	}
}

func TestRepeatedHoldRefusesStopWhilePausedRoundDrains(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		close(entered)
		<-ctx.Done()
		<-release
		return factory.StageResult{}, ctx.Err()
	})}, nil)
	defer close(release)
	id := g.add("stopped paused round", chat("work"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("round never started")
	}
	if err := g.r.Hold(id, true); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Stop(id); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		if err := g.r.Hold(id, true); !errors.Is(err, factory.ErrPersonStopped) {
			t.Fatalf("draining hold %d accepted Stop: %v", n, err)
		}
	}
}
