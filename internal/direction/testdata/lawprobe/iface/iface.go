// Package iface renames the raw transaction behind an interface of its own,
// so the call site names no workspace method at all.
package iface

import (
	"context"
	"database/sql"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

type raw interface {
	WriteImmediate(context.Context, func(*sql.Tx) error) error
}

// Wrap hides the store behind the interface.
func Wrap(s *workspace.Store) raw { return s }
