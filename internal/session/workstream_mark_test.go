package session

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// markedDir is a saved conversation's folder, marked as a workstream's when ws is set.
func markedDir(t *testing.T, ws string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	if err := SaveMeta(dir, Meta{ID: "0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	if ws != "" {
		if _, err := SetConversationWorkstream(dir, ws); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The mark is one word in the conversation's meta: kept, read back, cleared, and
// never rewritten when it already says the same thing.
func TestAConversationsWorkstreamMarkIsKeptAndCleared(t *testing.T) {
	dir := markedDir(t, "")
	if WorkstreamAt(dir) != "" {
		t.Fatal("an ordinary conversation reads as a workstream's")
	}
	if changed, err := SetConversationWorkstream(dir, " ws-pantry "); err != nil || !changed {
		t.Fatalf("mark = %v, %v", changed, err)
	}
	if got := WorkstreamAt(dir); got != "ws-pantry" {
		t.Fatalf("WorkstreamAt = %q", got)
	}
	if changed, _ := SetConversationWorkstream(dir, "ws-pantry"); changed {
		t.Fatal("the same word was written again")
	}
	if changed, err := SetConversationWorkstream(dir, ""); err != nil || !changed || WorkstreamAt(dir) != "" {
		t.Fatalf("clear = %v, %v, %q", changed, err, WorkstreamAt(dir))
	}
	for _, bad := range []string{strings.Repeat("w", workstreamMarkMost+1), "ws\x00x"} {
		if _, err := SetConversationWorkstream(dir, bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
	if _, err := SetConversationWorkstream(filepath.Join(t.TempDir(), "none"), "ws"); err == nil {
		t.Fatal("a folder with no conversation was marked")
	}
}

// A workstream's standing work is set up in the workstream, so a conversation
// that belongs to one has no app-wide `automation` verb; an ordinary one keeps it.
// A held conversation loses it at its next turn's start, and its page says why.
func TestAWorkstreamsConversationHasNoAutomationVerbAndIsToldWhereItsWorkGoes(t *testing.T) {
	store := automationStoreFor(t)
	plain := Config{Workspace: t.TempDir(), Automations: &Automations{Store: store}, Place: Place{Dir: markedDir(t, "")}}
	marked := Config{Workspace: t.TempDir(), Automations: &Automations{Store: store}, Place: Place{Dir: markedDir(t, "ws-pantry")}}
	if !automationBeltHas(automationBelt(&Agent{config: plain}), "automation") {
		t.Fatal("an ordinary conversation lost the automation verb")
	}
	if automationBeltHas(automationBelt(&Agent{config: marked}), "automation") {
		t.Fatal("a workstream's conversation carries the automation verb")
	}

	dir := markedDir(t, "")
	agent := automationAgent(t, &scriptedCompleter{}, store, func(c *Config) { c.System = ""; c.Place = Place{Dir: dir, Workspace: c.Workspace} })
	held := func() bool {
		agent.armMu.Lock()
		defer agent.armMu.Unlock()
		return slices.ContainsFunc(agent.tools, func(tool bare.Tool) bool { return tool.Name == "automation" })
	}
	if !held() {
		t.Fatal("the automation verb is not on the held conversation's belt")
	}
	if _, err := SetConversationWorkstream(dir, "ws-pantry"); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.followWorkScopeLocked(time.Now())
	page := agent.system
	agent.mu.Unlock()
	if held() {
		t.Fatal("a conversation that joined a workstream kept the automation verb")
	}
	for _, words := range []string{"THIS CONVERSATION BELONGS TO A WORKSTREAM", "only when a person adds it there", "only when ws_file_read lists one under earlier_copies", "People and rules › Keeps running"} {
		if !strings.Contains(page, words) {
			t.Fatalf("the page does not say %q", words)
		}
	}
	if strings.Contains(renderSystem(plain), "BELONGS TO A WORKSTREAM") {
		t.Fatal("an ordinary conversation's page names a workstream")
	}
}

// What a workstream's conversation says is never captured into the Library: its
// sources are read past, so the cursor moves and the vault stays empty, while an
// ordinary conversation beside it is captured as before.
func TestAWorkstreamsConversationIsNeverCapturedIntoTheLibrary(t *testing.T) {
	run := func(ws string) (pending int, moved bool) {
		profile := t.TempDir()
		if err := config.WriteMemoryEnabled(profile, false); err != nil {
			t.Fatal(err)
		}
		dir := markedDir(t, ws)
		agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("The architecture uses a durable queue."), nil
			},
		}}, func(c *Config) {
			c.usageLedger = filepath.Join(t.TempDir(), "usage.jsonl")
			c.ProfileDir = profile
			c.Place = Place{Dir: dir, Workspace: c.Workspace}
			c.SessionFile = c.Place.Transcript()
		})
		vault := agent.Knowledge()
		if vault == nil {
			t.Fatal("no knowledge vault")
		}
		if err := vault.StartBuild(); err != nil {
			t.Fatal(err)
		}
		collect(t, mustSubmit(t, agent, "Explain our durable queue architecture"))
		if ws == "" {
			deadline := time.Now().Add(4 * time.Second)
			for time.Now().Before(deadline) {
				if snapshot, err := vault.Snapshot(); err == nil && snapshot.Pending == 2 {
					return 2, true
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		agent.knowledgeMu.Lock()
		j := agent.knowledgeJournal
		agent.knowledgeMu.Unlock()
		if j == nil {
			t.Fatal("no knowledge journal")
		}
		if err := agent.scanKnowledge(context.Background(), j, j.path, agent.id, true); err != nil {
			t.Fatal(err)
		}
		snapshot, err := vault.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		j.mu.Lock()
		moved = j.offset > 0
		j.mu.Unlock()
		return snapshot.Pending, moved
	}
	if pending, _ := run(""); pending != 2 {
		t.Fatalf("an ordinary conversation captured %d sources, want 2", pending)
	}
	if pending, moved := run("ws-pantry"); pending != 0 || !moved {
		t.Fatalf("a workstream's conversation: %d captured, cursor moved %v", pending, moved)
	}
}
