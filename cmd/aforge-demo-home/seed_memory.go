package main

// seed_memory.go is what the demo machine remembers.
//
// It goes through internal/store's own doors — AddMemory, ForgetMemory,
// RecordMemoryOutcome — and never through SQL, so the rows land with the real
// schema, the event journal behind them is whole, and a Rebuild would reproduce
// exactly what the page draws. The three shelves are a closed enum (`user`,
// `project`, `env`) and all three are filled, because a shelf with nothing on
// it is not drawn at all.

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// demoMemory is one thing worth remembering across conversations, with the two
// counters that decide where it ranks.
type demoMemory struct {
	memory store.Memory
	// helped is how many times it BORE ON THE MOMENT and unused how many times
	// it was put in front of a model and bore on nothing. They are written
	// through [store.Store.RecordMemoryOutcome], one call per count, because the
	// counter measures help rather than injection and there is no door that sets
	// it to a number.
	helped, unused int
	// letGo forgets it afterwards. A forgotten memory is a tombstone with its
	// row intact — every view stops agreeing with it and nothing is deleted —
	// and the memory page is the only reader in the product that can see one.
	letGo bool
}

var demoMemories = []demoMemory{
	{memory: store.Memory{Type: store.MemoryPreference, Scope: store.MemoryScopeUser,
		Title: "Answers short, evidence under them",
		Text:  "They want the answer first and the reasoning underneath, never the other way round."}, helped: 9, unused: 1},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser,
		Title: "Works from Bengaluru",
		Text:  "Their day is IST, so a morning watch means their morning."}, helped: 4},
	{memory: store.Memory{Type: store.MemoryPreference, Scope: store.MemoryScopeUser,
		Title: "No emoji anywhere they read",
		Text:  "Not in commits, not in a card, not in a status line."}, helped: 6, unused: 2},
	{memory: store.Memory{Type: store.MemoryCorrection, Scope: store.MemoryScopeUser,
		Title: "It is the strip, not the toolbar",
		Text:  "They corrected this twice; the row of verbs under a line is the strip."}, helped: 3},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser,
		Title: "Ships on Fridays",
		Text:  "This turned out to be wrong — they ship when a wave is done."}, helped: 1, unused: 5, letGo: true},

	{memory: store.Memory{Type: store.MemoryProjectState, Scope: store.MemoryScopeProject,
		Title: "Work lands on chat-v3-task",
		Text:  "Feature waves are built in worktrees off it and merged back the same day."}, helped: 11, unused: 1},
	{memory: store.Memory{Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
		Title: "Home is one list, never a dashboard",
		Text:  "Ordered by what wants them first; innovation goes into the interaction, not into panels."}, helped: 7},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeProject,
		Title: "make check is the end-of-change ritual",
		Text:  "Vet, the tests, the build, and the binary-size ratchet in SIZE-BUDGET."}, helped: 8, unused: 3},
	{memory: store.Memory{Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
		Title: "The annual toggle ships before the ladder",
		Text:  "The toggle is a day of work and the ladder is a quarter of pricing argument."}, helped: 2, unused: 4},

	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeEnv,
		Title: "Go is on the path here",
		Text:  "A build needs nothing installed first on this machine."}, helped: 5},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeEnv,
		Title: "tmux is how the terminal is driven",
		Text:  "Screens are captured with tmux capture-pane -p rather than photographed."}, helped: 3, unused: 1},
	{memory: store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeEnv,
		Title: "ripgrep answers to rg",
		Text:  "It is faster than find here and it respects the ignore files."}, helped: 6, unused: 2},
}

// writeMemories puts every memory into the store and answers how many it wrote.
func writeMemories(brain *store.Store) (int, error) {
	written := 0
	for _, held := range demoMemories {
		made, err := brain.AddMemory(held.memory)
		if err != nil {
			return written, fmt.Errorf("remember %q: %w", held.memory.Title, err)
		}
		// One call per count: the two counters are written together from one
		// confirmation after a turn, and there is no door that sets them to a
		// figure — which is the whole reason a memory cannot be credited for
		// being retrieved rather than for being worth retrieving.
		for range held.helped {
			if err := brain.RecordMemoryOutcome([]string{made.ID}, nil); err != nil {
				return written, fmt.Errorf("count a use of %q: %w", held.memory.Title, err)
			}
		}
		for range held.unused {
			if err := brain.RecordMemoryOutcome(nil, []string{made.ID}); err != nil {
				return written, fmt.Errorf("count a miss of %q: %w", held.memory.Title, err)
			}
		}
		if held.letGo {
			if err := brain.ForgetMemory(made.ID); err != nil {
				return written, fmt.Errorf("let go of %q: %w", held.memory.Title, err)
			}
		}
		written++
	}
	return written, nil
}
