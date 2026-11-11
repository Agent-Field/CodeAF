package session

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/store"
)

// MemoryScopeSnapshot describes configured live scopes, not what rode a turn.
// Absence of a memory store is explicit, never three invented zero counts.
type MemoryScopeSnapshot struct {
	Available   bool                     `json:"available"`
	Basis       string                   `json:"basis"`
	Counts      *store.MemoryScopeCounts `json:"counts,omitempty"`
	RidingKnown bool                     `json:"riding_known"`
}

func (a *Agent) ApplicableMemory(ctx context.Context) (MemoryScopeSnapshot, error) {
	if ctx == nil {
		return MemoryScopeSnapshot{}, fmt.Errorf("memory scope needs a context")
	}
	if err := ctx.Err(); err != nil {
		return MemoryScopeSnapshot{}, err
	}
	a.mu.Lock()
	brain := a.config.bindingBrain()
	owners := MemoryOwners(a.config.MemoryProjectKey)
	a.mu.Unlock()
	out := MemoryScopeSnapshot{Basis: "configured_live", Available: brain != nil}
	if brain == nil {
		return out, nil
	}
	counts, err := brain.MemoryCountsFor(ctx, owners)
	if err != nil {
		return MemoryScopeSnapshot{}, err
	}
	out.Counts = &counts
	return out, nil
}
