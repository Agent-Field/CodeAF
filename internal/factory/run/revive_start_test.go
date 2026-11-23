package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// chatlessExec is a step run off this machine (an AgentField run it follows):
// it opens no chat, so a restart has no chat to carry on.
type chatlessExec struct {
	started chan struct{}
	release chan struct{}
}

func (e chatlessExec) Run(ctx context.Context, job Job) (factory.StageResult, error) {
	e.started <- struct{}{}
	select {
	case <-ctx.Done():
		return factory.StageResult{}, ctx.Err()
	case <-e.release:
		return factory.StageResult{Done: true, Output: "finished"}, nil
	}
}

// A ROUND A RESTART CUT SHORT IS THE SAME ROUND GOING ON (live e2e round 7):
// re-taking a chat-less step after a restart kept re-stamping its start and
// said "<step> started" a second time under "resumed". Its start is the one
// it had, and its conversation reads "write started" once.
func TestARevivedStepWithNoChatKeepsItsStartAndSaysStartedOnce(t *testing.T) {
	talk := newFakeTalk()
	first := chatlessExec{started: make(chan struct{}, 4), release: make(chan struct{})}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: first}, func(o *Options) { o.Talk = talk })
	id := g.add("one", chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error { it.Talk = "talk-1"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	<-first.started
	began := g.wait(id, "running", func(it factory.Item) bool {
		return it.Stream != nil && len(it.Stream.Phases) == 1 && !it.Stream.Phases[0].Started.IsZero()
	}).Stream.Phases[0].Started
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "paused", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused })
	if old := g.r.loop().ctl(id); old != nil {
		old.cancel()
	}
	time.Sleep(20 * time.Millisecond)

	second := chatlessExec{started: make(chan struct{}, 4), release: make(chan struct{})}
	r2 := New(Options{Store: g.st, Exec: map[factory.StageKind]Executor{factory.StageChat: second}, Benches: 1, Events: make(chan Event, 100), Talk: talk})
	if err := r2.Launch(id); err != nil {
		t.Fatal(err)
	}
	<-second.started
	it, _ := g.st.Get(id)
	if got := it.Stream.Phases[0].Started; !got.Equal(began) {
		t.Fatalf("the re-taken step's start moved from %s to %s", began, got)
	}
	close(second.release)
	g.waitState(id, factory.StateLanded)
	talk.mu.Lock()
	defer talk.mu.Unlock()
	starts := 0
	for _, line := range talk.said["talk-1"] {
		if strings.HasPrefix(line, "write started") {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("the conversation said write started %d times: %q", starts, talk.said["talk-1"])
	}
}
