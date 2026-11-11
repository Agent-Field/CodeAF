package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A WAIT BEFORE A STEP IS ASKED AGAIN IS SAID WITH ITS COUNTDOWN, once, from the
// engine's own phase: a provider's refusal, a rate limit and a lost network
// each count as one attempt, and a task's own wait or an errand's is not the
// reply's.
func TestAppRetryIsSaidWithItsCountdownOnce(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventToolBegin, CallID: "c1", Tool: "bash", Args: `{"command":"ls"}`}}, hold: hold}
	backend, rec, agent := workBackend(t, conv)
	if _, err := backend.Send(agent.ID, "list it", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool { _, ok := data.(appwire.TurnTool); return ok })

	now := time.Now()
	busy := session.PhaseNews{Phase: provider.PhaseRetrying, Role: lane.RoleTalk, Session: agent.ID, Since: now, At: now, Deadline: now.Add(4 * time.Second)}
	backend.phaseNews(busy)
	busy.At = now.Add(time.Second)
	backend.phaseNews(busy)
	backend.phaseNews(session.PhaseNews{Phase: provider.PhaseRetrying, Role: lane.RoleTalk, Session: agent.ID, Subject: agent.ID + "#3", Since: now, At: now, Deadline: now.Add(time.Second)})
	backend.phaseNews(session.PhaseNews{Phase: provider.PhaseRetrying, Role: lane.RoleMemory, Session: agent.ID, Since: now, At: now, Deadline: now.Add(time.Second)})
	later := now.Add(5 * time.Second)
	backend.phaseNews(session.PhaseNews{Phase: provider.PhasePaced, Role: lane.RoleTalk, Session: agent.ID, Since: later, At: later, Deadline: later.Add(8 * time.Second)})
	offline := now.Add(14 * time.Second)
	backend.phaseNews(session.PhaseNews{Phase: provider.PhaseConnectionLost, Role: lane.RoleTalk, Session: agent.ID, Since: offline, At: offline, Deadline: offline.Add(2 * time.Minute)})

	var said []appwire.TurnRetrying
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		if retry, ok := data.(appwire.TurnRetrying); ok {
			said = append(said, retry)
		}
		return len(said) == 3
	})
	want := []struct {
		in      int
		attempt int
		reason  string
	}{{4, 1, ""}, {8, 2, appRetryPaced}, {120, 3, appRetryOffline}}
	for i, w := range want {
		got := said[i]
		if got.InS == nil || *got.InS != w.in || got.Attempt != w.attempt || got.Reason != w.reason || got.Agent != agent.ID {
			t.Fatalf("retry %d = %+v, want %+v", i, got, w)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	count := 0
	for _, data := range rec.data {
		if _, ok := data.(appwire.TurnRetrying); ok {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("%d retries were said, want 3", count)
	}
}
