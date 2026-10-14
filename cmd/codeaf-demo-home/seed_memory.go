package main

// seed_memory.go is what the demo machine remembers.
//
// It goes through internal/store's own doors — AddMemory and ForgetMemory —
// and never through SQL, so the rows land with the real schema, the event
// journal behind them is whole, and a Rebuild would reproduce exactly what the
// page draws. Every shelf the memory place draws for a conversation here is
// filled — the person's, this machine's and the first project's — and one line
// is let go, so the let-go shelf has something to put back. Each line says who
// kept it, because the place draws that beside every line.

import (
	"fmt"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// demoMemory is one thing worth remembering across conversations.
type demoMemory struct {
	memory store.Memory
	// letGo forgets it afterwards: a tombstone with its row intact, which the
	// memory place's let-go shelf can put back.
	letGo bool
}

var demoMemories = []demoMemory{
	{memory: store.Memory{Type: store.MemoryPreference, Scope: store.MemoryScopeUser, Origin: store.OriginPerson,
		Text: "Wants the answer first and the reasoning underneath, never the other way round."}},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser, Origin: store.OriginNoticed,
		Text: "Works from Bengaluru, so a morning means IST."}},
	{memory: store.Memory{Type: store.MemoryPreference, Scope: store.MemoryScopeUser, Origin: store.OriginPerson,
		Text: "No emoji anywhere they read: not in commits, cards or status lines."}},
	{memory: store.Memory{Type: store.MemoryCorrection, Scope: store.MemoryScopeUser, Origin: store.OriginNoticed,
		Text: "The row of verbs under a line is the strip, not the toolbar."}},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser, Origin: store.OriginModel,
		Text: "Ships on Fridays."}, letGo: true},

	{memory: store.Memory{Type: store.MemoryDecision, Scope: store.MemoryScopeProject, Origin: store.OriginPerson,
		Text: "Branch off dev and open every pull request against dev."}},
	{memory: store.Memory{Type: store.MemoryDecision, Scope: store.MemoryScopeProject, Origin: store.OriginNoticed,
		Text: "Home is one list ordered by what wants them first, never a dashboard."}},
	{memory: store.Memory{Type: store.MemoryPreference, Scope: store.MemoryScopeProject, Origin: store.OriginPerson,
		Text: "Always run make pr-ready before opening a pull request."}},

	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeEnv, Origin: store.OriginNoticed,
		Text: "Go is on the path here, so a build needs nothing installed first."}},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeEnv, Origin: store.OriginModel,
		Text: "ripgrep answers to rg on this machine."}},
}

// writeMemories puts every memory into the store and answers how many it wrote.
//
// THE PROJECT ROWS ARE OWNED BY THE FIRST PROJECT'S REAL KEY and carry its name,
// minted the same way the engine mints it ([gitidentity.ProjectKey] and
// [gitidentity.ProjectName]), so a conversation opened in that folder carries
// them and the memory place heads their shelf "this project".
func writeMemories(brain *store.Store, projectDir string) (int, error) {
	key, err := gitidentity.ProjectKey(projectDir)
	if err != nil || key == "" {
		return 0, fmt.Errorf("mint the demo project key: %w", err)
	}
	projectOwner := store.OwnerProject(key)
	projectName := gitidentity.ProjectName(projectDir)
	if projectName == "" {
		projectName = filepath.Base(projectDir)
	}
	written := 0
	for _, held := range demoMemories {
		memory := held.memory
		if memory.Scope == store.MemoryScopeProject {
			memory.Owner = projectOwner
			memory.ProjectName = projectName
		}
		made, err := brain.AddMemory(memory)
		if err != nil {
			return written, fmt.Errorf("remember %q: %w", memory.Text, err)
		}
		if held.letGo {
			if err := brain.ForgetMemory(made.ID); err != nil {
				return written, fmt.Errorf("let go of %q: %w", memory.Text, err)
			}
		}
		written++
	}
	return written, nil
}
