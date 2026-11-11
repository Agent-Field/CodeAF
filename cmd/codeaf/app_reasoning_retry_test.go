package main

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppReasoningRefusalNotRetryableS2(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventError, ErrKind: session.TurnErrorProvider, ErrStatus: 400, ErrWords: `"llama3.2:latest" does not support thinking`}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	b := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer b.Close()
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Send(agent.ID, "hello", "s2"); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	appSyncWait(t, "S2 failed turn to settle", func() bool {
		idle := false
		b.locked(func() { idle = !b.agents[agent.ID].live.running })
		return idle
	})
	b.locked(func() {
		a := b.agents[agent.ID]
		if a.view.Retryable || a.failed != "" {
			t.Errorf("S2 permanent refusal retained retryable work: %+v", a.view)
		}
	})
}
