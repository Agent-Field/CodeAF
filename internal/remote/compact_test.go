package remote

import (
	"context"
	"testing"
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
