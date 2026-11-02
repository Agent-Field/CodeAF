package tui3

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"sync"
	"sync/atomic"
	"testing"
)

type l4QuitAgent struct {
	*hostedAgent
	idle  bool
	saved atomic.Bool
}

func (a *l4QuitAgent) LocalDurable() bool { return a.saved.Load() }
func (a *l4QuitAgent) Detach() error {
	if a.idle {
		a.saved.Store(true)
		return syncwords.UploadFailure(context.DeadlineExceeded)
	}
	return a.hostedAgent.Detach()
}

// L4-R5: A running chat's successful detach cannot poison an idle chat's pending upload on the same quit.
func TestL4FixTwoChatQuit(t *testing.T) {
	running := &l4QuitAgent{hostedAgent: &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}}
	idle := &l4QuitAgent{hostedAgent: &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}, idle: true}
	a := newTestApp(running)
	a.file = t.TempDir() + "/running/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	stowAgent(t, a, idle, t.TempDir()+"/idle/transcript.jsonl")
	var checks []func() bool
	a.savingDurability = func(check func() bool) { checks = append(checks, check) }
	var mu sync.Mutex
	var failed error
	a.saveFailure = func(err error) { mu.Lock(); defer mu.Unlock(); failed = err }
	a.leaveEverything()
	if len(checks) != 2 || !syncwords.UploadPending(failed) {
		t.Fatalf("checks=%d err=%v", len(checks), failed)
	}
	for i, check := range checks {
		if !check() {
			t.Errorf("chat %d is treated as an unacknowledged local save", i)
		}
	}
	if running.detaches != 1 || running.closes != 0 {
		t.Fatalf("running chat was ended: %+v", running)
	}
}
