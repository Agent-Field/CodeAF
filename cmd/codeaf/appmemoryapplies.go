package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// This read never opens a conversation. Only its already-held handle can prove
// its live configured scope; a closed conversation needs the next-open resolver.
func (b *appBackend) ApplicableMemory(ctx context.Context, id string) (session.MemoryScopeSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return session.MemoryScopeSnapshot{}, err
	}
	a, err := b.find(id)
	if err != nil {
		return session.MemoryScopeSnapshot{}, err
	}
	live := b.liveOf(a)
	if live == nil {
		return session.MemoryScopeSnapshot{}, appwire.Errorf(appwire.CodeEngine, "live memory scope is unknown until this conversation is open")
	}
	door, ok := live.conn.Conv.(interface {
		ApplicableMemory(context.Context) (session.MemoryScopeSnapshot, error)
	})
	if !ok {
		return session.MemoryScopeSnapshot{}, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation has no scoped memory reading")
	}
	return door.ApplicableMemory(ctx)
}
