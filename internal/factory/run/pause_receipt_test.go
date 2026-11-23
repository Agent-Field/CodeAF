package run

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestPauseResumeWhileSpendReceiptSettles(t *testing.T) {
	p := newPauseExec()
	receipt := make(chan func(), 1)
	var once sync.Once
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		once.Do(func() { receipt <- job.PendingSpend() })
		return p.Run(ctx, job)
	})}, nil)
	id := g.add("one", chat("write"))
	control := make(chan context.Context, 1)
	g.r.SetStepBegin(id, func(ctx context.Context, _ factory.StageReport) error {
		select {
		case control <- ctx:
		default:
		}
		return nil
	})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	finish := <-receipt
	var settled sync.Once
	settle := func() { settled.Do(finish) }
	defer settle()
	ctx := <-control

	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	// Keep the canceled control present while its spend receipt settles.
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("paused control did not drain")
	}
	resumed := make(chan error, 1)
	go func() { resumed <- g.r.Launch(id) }()
	select {
	case err := <-resumed:
		t.Fatalf("Resume returned before spending settled: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	settle()
	select {
	case err := <-resumed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resume did not continue after spending settled")
	}
	waitStarted(t, p)
	close(p.release)
	g.waitState(id, factory.StateLanded)
}
