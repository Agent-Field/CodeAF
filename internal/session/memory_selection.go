package session

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/store"
)

// MemorySelection reports the working set actually admitted to the current
// prompt. Inspection never emits rehearsal events or changes prompt admission.
func (a *Agent) MemorySelection() (store.MemoryBlock, error) {
	if !a.remembers() {
		return store.MemoryBlock{}, errors.New("this conversation keeps no memory")
	}
	a.memory.mu.Lock()
	if current := a.memory.selection; current != nil {
		block := *current
		block.Memories = append([]store.Memory(nil), current.Memories...)
		block.BudgetedRunesBySemanticType = make(map[string]int, len(current.BudgetedRunesBySemanticType))
		for kind, runes := range current.BudgetedRunesBySemanticType {
			block.BudgetedRunesBySemanticType[kind] = runes
		}
		block.RunesBySemanticType = make(map[string]int, len(current.RunesBySemanticType))
		for kind, runes := range current.RunesBySemanticType {
			block.RunesBySemanticType[kind] = runes
		}
		block.Decisions = make(map[string]store.MemoryAdmission, len(current.Decisions))
		for id, decision := range current.Decisions {
			block.Decisions[id] = decision
		}
		a.memory.mu.Unlock()
		return block, nil
	}
	a.memory.mu.Unlock()
	a.mu.Lock()
	ctx := store.MemoryContext{Owners: MemoryOwners(a.config.MemoryProjectKey), Project: a.config.MemoryProjectKey, Session: a.sessionID(), Brief: a.cardText}
	a.mu.Unlock()
	return a.memory.store.AssembleMemoryBlock(ctx)
}
