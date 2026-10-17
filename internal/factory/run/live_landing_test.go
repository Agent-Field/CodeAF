package run

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestLiveLandedSnapshotAcceptsSendBackBeforeControlUnwinds(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, _ Job) (factory.StageResult, error) {
		<-ctx.Done()
		return factory.StageResult{}, ctx.Err()
	})}, nil)
	id := g.add("ready", chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lp := g.r.loop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, benchNo: 1}
	lp.mu.Lock()
	lp.ctls[id], lp.bench[1] = c, id
	lp.mu.Unlock()
	// Run the final transition synchronously, before drive's deferred cleanup.
	lp.land(c)
	it, err := g.st.Get(id)
	if err != nil || it.State != factory.StateLanded {
		t.Fatalf("landed: %s %v", it.State, err)
	}
	if err := g.r.SendBack(id, "prove the live change"); err != nil {
		t.Fatal(err)
	}
	if lp.ctl(id) == c {
		t.Fatal("SendBack retained the finished control")
	}
	lp.mu.Lock()
	released := c.released
	lp.mu.Unlock()
	if !released {
		t.Fatal("finished control still occupies its bench")
	}
}
