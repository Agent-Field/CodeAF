package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Additive optional pure reading; old engines answer no such method.
const MethodMemoryApplies = "Memory.Applies"

var memoryAppliesReads = make(chan struct{}, 4)

type applicableMemory interface {
	ApplicableMemory(context.Context) (session.MemoryScopeSnapshot, error)
}

func (a *Agent) ApplicableMemory(ctx context.Context) (session.MemoryScopeSnapshot, error) {
	if ctx == nil {
		return session.MemoryScopeSnapshot{}, fmt.Errorf("memory scope needs a context")
	}
	if err := ctx.Err(); err != nil {
		return session.MemoryScopeSnapshot{}, err
	}
	raw, err := a.c.call(ctx, MethodMemoryApplies, nil)
	if err != nil {
		return session.MemoryScopeSnapshot{}, err
	}
	var out session.MemoryScopeSnapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return session.MemoryScopeSnapshot{}, err
	}
	return out, nil
}
