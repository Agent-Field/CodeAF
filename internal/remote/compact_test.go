package remote

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type compactingAgent struct{ *fakeAgent }

func (a compactingAgent) Compact(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokens = 2000
	return nil
}

func TestCompactPublishesReducedSizeBeforeReply(t *testing.T) {
	agent := &fakeAgent{model: "test/model", tokens: 30000}
	engine := engineOn(agent)
	engine.Agent = compactingAgent{agent}
	link := dialAgent(t, engine)
	link.hello(Hello{Version: Version})
	link.ok(1, MethodCompact, nil)
	if len(link.stated) == 0 {
		t.Fatal("compact replied without publishing fresh facts")
	}
	facts := decode[FactsPush](t, link.stated[len(link.stated)-1].Payload)
	if facts.Facts.ContextTokens != 2000 {
		t.Fatalf("compact replied with stale size %d", facts.Facts.ContextTokens)
	}
}

type compactingWithoutSummary struct{ *fakeAgent }

func (a compactingWithoutSummary) Compact(context.Context) error {
	a.mu.Lock()
	a.tokens = 2000
	a.mu.Unlock()
	return &session.SummarySkipped{Why: "provider unavailable"}
}

func TestCompactServerCarriesSkippedSummaryInSuccessfulResult(t *testing.T) {
	agent := &fakeAgent{model: "test/model", tokens: 30000}
	engine := engineOn(agent)
	engine.Agent = compactingWithoutSummary{agent}
	link := dialAgent(t, engine)
	link.hello(Hello{Version: Version})
	result := link.ok(1, MethodCompact, nil)
	if why := decode[string](t, result.Payload); why != "provider unavailable" {
		t.Fatalf("optional success result = %q", why)
	}
}

func TestCompactClientReadsSkippedSummaryResult(t *testing.T) {
	client, engine := newEngine(t)
	engine.answers[MethodCompact] = "provider unavailable"
	why, ok := session.SummarySkippedWhy(client.Agent().Compact(context.Background()))
	if !ok || why != "provider unavailable" {
		t.Fatalf("remote compact result = %q, skipped=%v", why, ok)
	}
}
