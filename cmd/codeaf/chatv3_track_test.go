package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// v3HeldAgents is what the process is holding right now, read under its own
// lock because every other reader of that list takes it.
func v3HeldAgents(proc *v3Process) []*session.Agent {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	return append([]*session.Agent(nil), proc.agents...)
}

// v3TrackedAgent opens one throwaway conversation for the tracking tests. The
// conversation is real because [v3Process.track] holds what the doors build and
// nothing else; a stand-in would prove the list works and not that the thing
// put in it is the thing a broadcast has to reach.
func v3TrackedAgent(t *testing.T, workspace string) *session.Agent {
	t.Helper()
	place, err := v3MintSession(t.TempDir(), workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := openV3Agent(session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	}, workspace, v3OpenSession)
	if err != nil {
		t.Fatalf("a conversation did not open: %v", err)
	}
	return agent
}

// A CONVERSATION THIS PROCESS HOLDS IS REACHED BY ITS BROADCASTS, AND A CLOSED
// ONE IS LET GO OF. The two halves are one test because neither is worth
// anything alone: tracking without forgetting is the leak below, and forgetting
// without tracking is the defect that sent a qualified model id to the default
// service.
func TestATrackedConversationIsReachedAndAForgottenOneIsNot(t *testing.T) {
	proc := v3TestProcess(t)
	workspace := t.TempDir()
	first := v3TrackedAgent(t, workspace)
	second := v3TrackedAgent(t, workspace)

	proc.track(first)
	proc.track(second)
	if got := v3HeldAgents(proc); len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("the process holds %d conversations, want the two it was told about in the order they opened", len(got))
	}

	proc.forget(first)
	held := v3HeldAgents(proc)
	if len(held) != 1 || held[0] != second {
		t.Fatalf("after forgetting the first conversation the process holds %d, want only the second", len(held))
	}

	// Forgetting is answerable twice and answerable about a conversation this
	// process never held: both are a door saying a conversation is over, and a
	// door may say it more than once.
	proc.forget(first)
	proc.forget(v3TrackedAgent(t, workspace))
	if held := v3HeldAgents(proc); len(held) != 1 || held[0] != second {
		t.Fatalf("a repeated or unknown goodbye changed the list: %d held", len(held))
	}
}

// AND THE LIST DOES NOT GROW WITH THE CONVERSATIONS A PROCESS HAS SERVED. This
// is the whole reason the forget half exists: a surface goes away with the
// handful of conversations one terminal opened, and an engine process outlives
// every conversation in it, so a door that only ever registered would walk
// agents that ended hours ago on every broadcast.
func TestOpeningAndClosingConversationsLeavesTheProcessHoldingNothing(t *testing.T) {
	proc := v3TestProcess(t)
	workspace := t.TempDir()
	for range 8 {
		agent := v3TrackedAgent(t, workspace)
		proc.track(agent)
		if err := agent.Close(); err != nil {
			t.Fatalf("a conversation did not close: %v", err)
		}
		proc.forget(agent)
	}
	if held := v3HeldAgents(proc); len(held) != 0 {
		t.Fatalf("after eight conversations opened and closed the process still holds %d", len(held))
	}
}

// THE ENGINE DOOR REGISTERS EVERY CONVERSATION IT BUILDS AND PAIRS IT WITH A
// GOODBYE, read off the door's own source.
//
// IT IS STRUCTURAL BECAUSE THE DEFECT WAS STRUCTURAL. Two doors tracked what
// they built (chatv3.go's boot conversation, chatv3_process.go's /new) and the
// engine's did not, so every conversation served over a session host kept the
// account set it was born with and a service connected mid-conversation reached
// nothing. No behaviour test of one road would have named the road that was
// missing; counting them does.
func TestTheEngineDoorTracksEveryConversationItBuildsAndForgetsIt(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "engine.go", nil, 0)
	if err != nil {
		t.Fatalf("read the engine door: %v", err)
	}
	var boot *ast.FuncDecl
	for _, decl := range file.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok && function.Name.Name == "bootEngine" {
			boot = function
		}
	}
	if boot == nil {
		t.Fatal("bootEngine is gone from engine.go — this law names the door that opens an engine conversation")
	}

	built, tracked, closed := 0, 0, false
	ast.Inspect(boot, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			switch called := value.Fun.(type) {
			case *ast.Ident:
				// openV3Agent is the shared assembly; `open` is the builder it
				// and both replacement closures are handed. Each call is one
				// more conversation this process now owns.
				if called.Name == "openV3Agent" || called.Name == "open" {
					built++
				}
			case *ast.SelectorExpr:
				if receiver, ok := called.X.(*ast.Ident); ok && receiver.Name == "proc" && called.Sel.Name == "track" {
					tracked++
				}
			}
		case *ast.KeyValueExpr:
			if key, ok := value.Key.(*ast.Ident); ok && key.Name == "Closed" {
				closed = true
			}
		}
		return true
	})

	if built == 0 {
		t.Fatal("bootEngine builds no conversation — this law can no longer read the door it was written for")
	}
	if tracked != built {
		t.Fatalf("bootEngine builds %d conversations and tracks %d: a conversation this process does not hold is one no broadcast reaches, so a service connected while it is open never arrives", built, tracked)
	}
	if !closed {
		t.Fatal("the engine hands the wire no Closed door: tracking without it grows the held list for the life of a daemon that outlives every conversation in it")
	}
}
