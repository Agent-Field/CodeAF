package remote

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// RULES CROSS THE WIRE BOTH WAYS THE SURFACE REACHES THEM: /always through the
// conversation's own engine, which decides where the rule holds and says so in
// its receipt, and the memory place's `a` through the engine's store, by id.
// The far end is a real session over a real store, because what is under test
// is that the default road (a *remote.Agent) reaches the same rule a local
// session would keep.
func TestRulesCrossTheWireToTheRealSession(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	// No model call is made: the store holds nothing near the rule, so the
	// settle never asks a decider, and an unreachable address is the honest
	// fixture for a provider nobody needs.
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/rules-wire",
		APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only.", PromptProfile: "full",
		Memory: brain, MemoryProjectKey: "wire"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Memory: engineMemoryRows{brain}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	commands, ok := any(loop.Client.Agent()).(MemoryCommands)
	if !ok || !commands.Remembers() {
		t.Fatal("the ordinary engine handle has no memory commands")
	}
	receipt, err := commands.RememberAlways("use tabs in this repository", false)
	if err != nil || receipt != "always · use tabs in this repository · in this project" {
		t.Fatalf("/always over the wire = (%q, %v)", receipt, err)
	}
	lines, err := commands.AlwaysMemories(false)
	if err != nil || len(lines) != 1 || !lines[0].Always || lines[0].Text != "use tabs in this repository" {
		t.Fatalf("the rules over the wire = (%+v, %v), want the one rule", lines, err)
	}
	if everywhere, err := commands.AlwaysMemories(true); err != nil || len(everywhere) != 0 {
		t.Fatalf("the person's rules everywhere = (%+v, %v), want none: the rule is this project's", everywhere, err)
	}
	// The place's `a`, by the id the page shows, through the engine's store.
	if err := loop.Client.SetMemoryAlways(lines[0].ID, false); err != nil {
		t.Fatalf("`a` over the wire: %v", err)
	}
	if lines, err := commands.AlwaysMemories(false); err != nil || len(lines) != 0 {
		t.Fatalf("after `a` took the rule back the rules = (%+v, %v), want none", lines, err)
	}
	row, ok, err := brain.MemoryRecord(lines[0].ID)
	if err != nil || !ok || row.Always || row.Status != store.MemoryActive {
		t.Fatalf("the line after `a` = (%+v, %v, %v), want it held and ordinary", row, ok, err)
	}
}

// WITH MEMORY OFF ON THE ENGINE, THE RULE DOORS ARE REFUSED IN THE ONE SENTENCE
// every memory door is, rather than answering an empty list that would read as
// "no rules" when the truth is "no memory".
func TestTheRuleDoorsWithMemoryOffAreRefused(t *testing.T) {
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/rules-off",
		APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only.", PromptProfile: "full"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	for _, method := range []string{MethodMemoryRememberAlways, MethodMemoryAlways} {
		if _, err := loop.Client.call(nil, method, MemoryAlwaysArgs{Text: "use tabs"}); err == nil || !strings.Contains(err.Error(), memoryOffWord) {
			t.Fatalf("%s with memory off = %v", method, err)
		}
	}
	if err := loop.Client.SetMemoryAlways("mem_x", true); err == nil || !strings.Contains(err.Error(), memoryOffWord) {
		t.Fatalf("the place's `a` with memory off = %v", err)
	}
}
