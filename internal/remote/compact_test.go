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

type observingAgent struct{ *fakeAgent }

func (a observingAgent) CompactObserved(_ context.Context, observe func(session.Event)) error {
	observe(session.Event{Kind: session.EventSummaryStarted, Compaction: &session.CompactionFact{Seq: 4, TokensBefore: 30000}})
	a.mu.Lock()
	a.tokens = 2000
	a.mu.Unlock()
	observe(session.Event{Kind: session.EventCompacting})
	observe(session.Event{Kind: session.EventCompacted, Compaction: &session.CompactionFact{Seq: 4, TokensBefore: 30000, TokensAfter: 2000, Summarized: 12, Summary: "kept"}})
	return nil
}

// A PASS ASKED FOR OUTSIDE A TURN IS SAID WHILE IT RUNS. Its started and
// settled events reach the surface as pass frames, each behind fresh facts,
// all ahead of the call's answer; the bare compacting event is not sent.
func TestCompactSaysItsPassAheadOfTheReply(t *testing.T) {
	agent := &fakeAgent{model: "test/model", tokens: 30000}
	engine := engineOn(agent)
	engine.Agent = observingAgent{agent}
	link := dialAgent(t, engine)
	link.hello(Hello{Version: Version})
	link.write(Frame{Kind: "call", ID: 1, Method: MethodCompact})
	var kinds []string
	var passes []session.Event
	for {
		frame := link.recvAny()
		if frame.Kind == "result" && frame.ID == 1 {
			if frame.Error != "" {
				t.Fatalf("compact: %s", frame.Error)
			}
			break
		}
		kinds = append(kinds, frame.Kind)
		if frame.Kind == passFrameKind {
			passes = append(passes, decode[EventWire](t, frame.Payload).Unwire())
		}
	}
	if len(passes) != 2 || passes[0].Kind != session.EventSummaryStarted || passes[1].Kind != session.EventCompacted {
		t.Fatalf("pass frames = %+v, want started then compacted", passes)
	}
	if passes[1].Compaction == nil || passes[1].Compaction.Seq != 4 || passes[1].Compaction.Summary != "kept" {
		t.Fatalf("settled fact = %+v", passes[1].Compaction)
	}
	for i, kind := range kinds {
		if kind == passFrameKind && (i == 0 || kinds[i-1] != "facts") {
			t.Fatalf("frames %v: a pass frame went out without fresh facts ahead of it", kinds)
		}
	}
}
