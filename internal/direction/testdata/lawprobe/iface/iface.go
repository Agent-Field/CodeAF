// Package iface hides a raw transaction behind an interface of its own, so
// the call site names no workspace object at all. The door is a function, so
// the interface needs an adaptor, and the adaptor names the door.
package iface

import (
	"context"
	"database/sql"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

type raw interface {
	WriteImmediate(context.Context, func(*sql.Tx) error) error
}

type adaptor struct{ s *workspace.Store }

func (a adaptor) WriteImmediate(ctx context.Context, fn func(*sql.Tx) error) error {
	return workspace.WriteImmediate(ctx, a.s, fn)
}

// Wrap hides the store behind the interface.
func Wrap(s *workspace.Store) raw { return adaptor{s} }
