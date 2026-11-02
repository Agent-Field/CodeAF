package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A rig built without an opener (the factory wire, most app tests) still
// reaches a conversation open from background work such as the brief's
// writer holding a job's chat. That open answers a refusal, never a panic
// that takes the whole test binary down.
func TestAFakeBackendWithoutAnOpenerRefusesAnOpen(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	a := &appAgent{view: appwire.Agent{ID: "job-chat"}, transcript: appFakeTranscript}
	b.locked(func() { b.agents[a.view.ID] = a })
	held, err := b.holdJobChat(a.view.ID)
	if err == nil || held {
		t.Fatalf("held=%v err=%v, want a refused open", held, err)
	}
}
