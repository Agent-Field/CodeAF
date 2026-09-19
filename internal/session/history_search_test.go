package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

type fakeHybrid struct {
	hits []SearchCandidate
	err  error
}

func (f *fakeHybrid) HybridMessages(context.Context, string, string, string, int) ([]SearchCandidate, error) {
	if f == nil {
		return nil, nil
	}
	return f.hits, f.err
}

// A18 / J15: search_conversations follows ConversationHistory, not Memory.
func TestConversationHistoryWorksWhenMemoryIsOff(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("open history: %v", err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ConversationHistory = brain
	})
	if agent.config.Memory != nil {
		t.Fatal("Memory must stay nil when only ConversationHistory is set")
	}
	if beltHas(agent, "remember") {
		t.Fatal("remember is on the belt with memory off")
	}
	if !beltHas(agent, "search_conversations") {
		t.Fatal("search_conversations is absent with history on and memory off")
	}
	post(t, brain, "elsewhere", store.RoleUser, "the flag is called quiet-mode")
	out := searchConversations(t, agent, `{"query":"quiet-mode"}`)
	if !strings.Contains(out, "the flag is called quiet-mode") {
		t.Fatalf("history search missed the indexed words:\n%s", out)
	}
}

func TestHybridSearchAddsEmbeddingCandidatesBesideBM25(t *testing.T) {
	hybrid := &fakeHybrid{}
	agent, brain := brainAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.HybridSearch = hybrid
	})
	for i := 0; i < 5; i++ {
		post(t, brain, "lexical-room", store.RoleUser, "the retry limit stays at three")
	}
	extra, err := brain.PostMessage(store.Message{
		SessionID: "embed-room",
		Role:      store.RoleUser,
		Body:      "the backoff window is four seconds",
	})
	if err != nil {
		t.Fatalf("post embedding neighbour: %v", err)
	}
	hybrid.hits = []SearchCandidate{{
		Hit: store.MessageHit{
			SessionID: extra.SessionID, Seq: extra.Seq, Role: extra.Role,
			Body: extra.Body, Complete: true,
		},
		ScoreKind: ScoreEmbed,
	}}
	out := searchConversations(t, agent, `{"query":"retry","limit":3}`)
	if !strings.Contains(out, "retry limit") {
		t.Fatalf("lost BM25 hits:\n%s", out)
	}
	if !strings.Contains(out, "backoff window") {
		t.Fatalf("missing embedding candidate:\n%s", out)
	}
	if !strings.Contains(out, " · embed") {
		t.Fatalf("embedding hit unmarked:\n%s", out)
	}

	hybrid.hits = []SearchCandidate{{
		Hit: store.MessageHit{
			SessionID: extra.SessionID, Seq: extra.Seq, Role: extra.Role,
			Body: extra.Body, Complete: true,
		},
		ScoreKind: ScoreExpansion,
		Degraded:  true,
	}}
	expanded := searchConversations(t, agent, `{"query":"retry","limit":3}`)
	if !strings.Contains(expanded, " · expansion · degraded") {
		t.Fatalf("degraded expansion unmarked:\n%s", expanded)
	}
}

func TestSearchConversationsSentenceIsNoLongerLexicalOnly(t *testing.T) {
	if strings.Contains(searchConversationsDescription, "Search is lexical") {
		t.Fatal("the tool still says search is lexical-only")
	}
	if !strings.Contains(searchConversationsDescription, "hybrid") {
		t.Fatal("the tool never says search is hybrid")
	}
}
