package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type fakeGuidance struct {
	mu     sync.Mutex
	loaded EffectiveGuidance
	err    error
	calls  int
	lastID string
}

func (f *fakeGuidance) Effective(_ context.Context, conversationID string) (EffectiveGuidance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastID = conversationID
	return f.loaded, f.err
}

func (f *fakeGuidance) set(loaded EffectiveGuidance) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded = loaded
}

func (f *fakeGuidance) id() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastID
}

func TestEffectiveGuidanceLoadsOnTheMainTurn(t *testing.T) {
	src := &fakeGuidance{loaded: EffectiveGuidance{
		Items: []GuidanceItem{{
			ScopeID: "rootrootrootroot",
			Name:    "Root",
			Text:    "never file billing chats in security",
		}},
		Snapshot: GuidanceSnapshot{
			RootRevision: 2,
			Guidance:     []GuidanceRev{{ScopeID: "rootrootrootroot", Revision: 2}},
		},
	}}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("noted"), nil
		},
	}}
	place := filepath.Join(t.TempDir(), "aaaaaaaaaaaaaaaa")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = Place{Dir: place}
		config.Guidance = src
	})
	collect(t, mustSubmit(t, agent, "what are we working on?"))
	agent.mu.Lock()
	prompt := ""
	if len(agent.messages) > 0 && len(agent.messages[0].Content) > 0 {
		prompt = agent.messages[0].Content[0].Text
	}
	agent.mu.Unlock()
	if !strings.Contains(prompt, "<guidance>") || !strings.Contains(prompt, "never file billing chats in security") {
		t.Fatalf("main turn missing folder instructions:\n%s", prompt)
	}
	if src.id() != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("guidance loaded for %q, want this chat's id", src.id())
	}
}

func TestGuidanceConflictOpensOneDiscussionInsteadOfAskingAfterTwoTurns(t *testing.T) {
	src := &fakeGuidance{loaded: EffectiveGuidance{
		Conflict: true,
		Items: []GuidanceItem{
			{ScopeID: "billingbilling00", Name: "Billing", Text: "mail receipt links"},
			{ScopeID: "securitysecurity", Name: "Security", Text: "never mail raw URLs"},
		},
		Snapshot: GuidanceSnapshot{Conflict: true},
	}}
	collab := &fakeCollab{}
	place := filepath.Join(t.TempDir(), "aaaaaaaaaaaaaaaa")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: place}
		config.Guidance = src
		config.Collab = collab
	})
	agent.refreshGuidanceLocked(context.Background())
	agent.refreshGuidanceLocked(context.Background())
	collab.mu.Lock()
	opened := append([]string{}, collab.conflicts...)
	collab.mu.Unlock()
	if len(opened) != 2 || opened[0] != "aaaaaaaaaaaaaaaa" || opened[1] != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("software must keep opening/reusing the conflict room rather than ask after two turns: %v", opened)
	}
	agent.mu.Lock()
	prompt := agent.guidanceText
	agent.mu.Unlock()
	if !strings.Contains(prompt, "one conflict discussion") {
		t.Fatalf("guidance must name the software-opened room:\n%s", prompt)
	}
	if !strings.Contains(prompt, "always ask after two turns") {
		t.Fatalf("must keep the hierarchical-escalation wording, not drop the needle:\n%s", prompt)
	}
}

func TestGuidanceCheckpointPausesOnlyAffectedMutations(t *testing.T) {
	src := &fakeGuidance{loaded: EffectiveGuidance{
		Snapshot: GuidanceSnapshot{
			RootRevision: 1,
			Guidance:     []GuidanceRev{{ScopeID: "r", Revision: 1}},
		},
	}}
	fake := &fakeFolders{listed: []FolderRef{{ID: "billingbilling00", Name: "Billing"}}}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: filepath.Join(t.TempDir(), "aaaaaaaaaaaaaaaa")}
		config.Folders = fake
		config.Guidance = src
	})
	agent.refreshGuidanceLocked(context.Background())

	listed, failed := callFolders(t, agent, `{"action":"list"}`)
	if failed {
		t.Fatalf("list refused on a stable snapshot: %s", listed)
	}

	out, failed := callFolders(t, agent, `{"action":"file","id":"billingbilling00"}`)
	if failed {
		t.Fatalf("stable guidance paused file: %s", out)
	}

	src.set(EffectiveGuidance{
		Conflict: true,
		Snapshot: GuidanceSnapshot{
			RootRevision: 2,
			Conflict:     true,
			Guidance:     []GuidanceRev{{ScopeID: "r", Revision: 2}},
		},
	})
	listed, failed = callFolders(t, agent, `{"action":"list"}`)
	if failed {
		t.Fatalf("list must keep working after guidance moves: %s", listed)
	}
	out, failed = callFolders(t, agent, `{"action":"file","id":"billingbilling00"}`)
	if !failed || !strings.Contains(out, ErrGuidanceChanged.Error()) {
		t.Fatalf("file was not paused: failed=%v %s", failed, out)
	}
	_, _, _, err := agent.StartTask(context.Background(), "do the filing", false)
	if !errors.Is(err, ErrGuidanceChanged) {
		t.Fatalf("StartTask: %v", err)
	}
}
